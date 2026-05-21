package crosscompile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/goplus/llar/internal/build/buildtarget"
)

const defaultManifestURL = "https://raw.githubusercontent.com/MeteorsLiu/llar-toolchains/main/manifest.json"

type Command struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

type Patch struct {
	Name       string
	PrependArg []string
	AppendArg  []string
	SetEnv     map[string]string
	PrependEnv map[string][]string
}

type CrossCompile struct {
	enabled       bool
	matrix        string
	target        buildtarget.Platform
	host          buildtarget.Platform
	triple        string
	buildTriple   string
	toolchainDir  string
	toolchainID   string
	toolchainVers string
	toolchainSHA  string
	toolchainFile string
}

type manifest struct {
	Version   string                     `json:"version"`
	Toolchain map[string]toolchainRecord `json:"toolchains"`
}

type toolchainRecord struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	StripPrefix string `json:"strip_prefix"`
}

func New(matrix string) (*CrossCompile, error) {
	return newWithHost(matrix, buildtarget.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
}

func newWithHost(matrix string, host buildtarget.Platform) (*CrossCompile, error) {
	cc := &CrossCompile{matrix: matrix, host: host, buildTriple: tripleFor(host)}
	if matrix == "" {
		return cc, nil
	}
	target, err := buildtarget.Parse(matrix)
	if err != nil {
		return nil, err
	}
	cc.target = target
	if target.OS == "" || target.IsNative(host) {
		return cc, nil
	}
	if target.OS != "linux" {
		return nil, fmt.Errorf("unsupported cross compile target matrix %q: target %s/%s from build platform %s/%s is not supported by MVP", matrix, target.OS, target.Arch, host.OS, host.Arch)
	}
	triple, err := linuxTriple(target.Arch)
	if err != nil {
		return nil, fmt.Errorf("unsupported cross compile target matrix %q: %w", matrix, err)
	}
	cc.enabled = true
	cc.triple = triple
	if err := cc.prepareToolchain(); err != nil {
		return nil, err
	}
	return cc, nil
}

func (c *CrossCompile) Identity() string {
	if !c.enabled {
		return ""
	}
	return c.toolchainID + "@" + c.toolchainVers + ":" + c.toolchainSHA
}

func (c *CrossCompile) Use(cmd Command) Patch {
	if c == nil || !c.enabled {
		return Patch{}
	}
	base := filepath.Base(cmd.Name)
	switch base {
	case "cmake":
		if isCMakeConfigure(cmd.Args) && !hasCMakeToolchain(cmd.Args) {
			return Patch{AppendArg: []string{"-DCMAKE_TOOLCHAIN_FILE:STRING=" + c.toolchainFile}}
		}
	case "configure":
		return c.autotoolsPatch(cmd)
	case "cc", "gcc":
		return Patch{Name: c.tool("clang"), PrependArg: []string{"--target=" + c.triple}}
	case "c++", "g++":
		return Patch{Name: c.tool("clang++"), PrependArg: []string{"--target=" + c.triple}}
	case "ar":
		return Patch{Name: c.tool("llvm-ar")}
	case "ranlib":
		return Patch{Name: c.tool("llvm-ranlib")}
	case "strip":
		return Patch{Name: c.tool("llvm-strip")}
	}
	if strings.HasSuffix(base, "configure") {
		return c.autotoolsPatch(cmd)
	}
	return Patch{}
}

func (c *CrossCompile) autotoolsPatch(cmd Command) Patch {
	set := map[string]string{
		"CC":       c.tool("clang"),
		"CXX":      c.tool("clang++"),
		"AR":       c.tool("llvm-ar"),
		"RANLIB":   c.tool("llvm-ranlib"),
		"STRIP":    c.tool("llvm-strip"),
		"CFLAGS":   appendFlag(envValue(cmd.Env, "CFLAGS"), "--target="+c.triple),
		"CXXFLAGS": appendFlag(envValue(cmd.Env, "CXXFLAGS"), "--target="+c.triple),
	}
	appendArgs := make([]string, 0, 2)
	if !hasArgPrefix(cmd.Args, "--host=") {
		appendArgs = append(appendArgs, "--host="+c.triple)
	}
	if c.buildTriple != "" && !hasArgPrefix(cmd.Args, "--build=") {
		appendArgs = append(appendArgs, "--build="+c.buildTriple)
	}
	return Patch{SetEnv: set, AppendArg: appendArgs}
}

func (c *CrossCompile) tool(name string) string {
	return filepath.Join(c.toolchainDir, "bin", name)
}

func (c *CrossCompile) prepareToolchain() error {
	m, err := loadManifest(manifestURL())
	if err != nil {
		return fmt.Errorf("prepare managed LLVM for matrix %q on build platform %s/%s: %w", c.matrix, c.host.OS, c.host.Arch, err)
	}
	key := c.host.OS + "/" + c.host.Arch
	rec, ok := m.Toolchain[key]
	if !ok {
		return fmt.Errorf("prepare managed LLVM for matrix %q: manifest has no toolchain for build platform %s", c.matrix, key)
	}
	dir, err := ensureToolchain(rec)
	if err != nil {
		return fmt.Errorf("prepare managed LLVM %s@%s for matrix %q: %w", rec.ID, rec.Version, c.matrix, err)
	}
	c.toolchainDir = dir
	c.toolchainID = rec.ID
	c.toolchainVers = rec.Version
	c.toolchainSHA = rec.SHA256
	return c.writeCMakeToolchain()
}

func manifestURL() string {
	if v := os.Getenv("LLAR_TOOLCHAIN_MANIFEST_URL"); v != "" {
		return v
	}
	return defaultManifestURL
}

func loadManifest(url string) (manifest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return manifest{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return manifest{}, fmt.Errorf("download manifest %s: HTTP %s", url, resp.Status)
	}
	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return manifest{}, err
	}
	return m, nil
}

func ensureToolchain(rec toolchainRecord) (string, error) {
	root, err := toolchainCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, rec.ID+"@"+rec.Version+"-"+shortSHA(rec.SHA256))
	if _, err := os.Stat(filepath.Join(dir, "bin", "clang")); err == nil {
		return dir, nil
	}
	tmp, err := os.MkdirTemp(root, "download-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, "toolchain.tar.gz")
	if err := downloadFile(rec.URL, archive); err != nil {
		return "", err
	}
	if err := verifySHA256(archive, rec.SHA256); err != nil {
		return "", err
	}
	if err := extractTarGz(archive, tmp, rec.StripPrefix); err != nil {
		return "", err
	}
	extracted := filepath.Join(tmp, rec.StripPrefix)
	if rec.StripPrefix == "" {
		extracted = tmp
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.Rename(extracted, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func toolchainCacheDir() (string, error) {
	if dir := os.Getenv("LLAR_TOOLCHAIN_CACHE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, ".llar", "toolchains")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func downloadFile(url, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, want)
	}
	return nil
}

func extractTarGz(path, dest, stripPrefix string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func (c *CrossCompile) writeCMakeToolchain() error {
	path := filepath.Join(c.toolchainDir, "llar-"+c.triple+".cmake")
	systemProcessor := "x86_64"
	if c.target.Arch == "arm64" {
		systemProcessor = "aarch64"
	}
	content := fmt.Sprintf(`set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR %s)
set(CMAKE_C_COMPILER %s)
set(CMAKE_CXX_COMPILER %s)
set(CMAKE_C_COMPILER_TARGET %s)
set(CMAKE_CXX_COMPILER_TARGET %s)
set(CMAKE_AR %s)
set(CMAKE_RANLIB %s)
set(CMAKE_STRIP %s)
`, systemProcessor, c.tool("clang"), c.tool("clang++"), c.triple, c.triple, c.tool("llvm-ar"), c.tool("llvm-ranlib"), c.tool("llvm-strip"))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	c.toolchainFile = path
	return nil
}

func linuxTriple(arch string) (string, error) {
	switch arch {
	case "amd64", "x86_64":
		return "x86_64-linux-gnu", nil
	case "arm64", "aarch64":
		return "aarch64-linux-gnu", nil
	default:
		return "", fmt.Errorf("unsupported linux target arch %q", arch)
	}
}

func tripleFor(p buildtarget.Platform) string {
	if p.OS == "linux" {
		if triple, err := linuxTriple(p.Arch); err == nil {
			return triple
		}
	}
	return p.Arch + "-" + p.OS
}

func isCMakeConfigure(args []string) bool {
	if len(args) == 0 {
		return true
	}
	return !strings.HasPrefix(args[0], "--")
}

func hasCMakeToolchain(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DCMAKE_TOOLCHAIN_FILE") {
			return true
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return os.Getenv(key)
}

func appendFlag(current, flag string) string {
	if current == "" {
		return flag
	}
	return current + " " + flag
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
