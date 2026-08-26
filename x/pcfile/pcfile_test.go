package pcfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/execbroker"
	"github.com/goplus/llar/x/pkgconfig"
	"github.com/kballard/go-shellquote"
)

func TestNewEncodesFormulaShapes(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want string
	}{
		{
			name: "header only",
			spec: &Spec{
				Name:        "glm",
				Description: "OpenGL Mathematics (GLM)",
				Version:     "1.0.1",
				Cflags:      []string{"-I${includedir}"},
			},
			want: "prefix=${pcfiledir}/../..\n" +
				"exec_prefix=${prefix}\n" +
				"libdir=${prefix}/lib\n" +
				"includedir=${prefix}/include\n\n" +
				"Name: glm\n" +
				"Description: OpenGL Mathematics (GLM)\n" +
				"Version: 1.0.1\n" +
				"Libs:\n" +
				"Cflags: -I${includedir}\n",
		},
		{
			name: "custom library directory",
			spec: &Spec{
				Variables:   map[string]string{"libdir": "${prefix}/lib/datetime"},
				Name:        "datetime",
				Description: "A simple Date and time library built in C++",
				Version:     "1.0.2",
				Libs:        []string{"-L${libdir}", "-ldatetime", "-lm", "-lpthread", "-ldl"},
				Cflags:      []string{"-I${includedir}", "-DDATETIME_STATIC"},
			},
			want: "prefix=${pcfiledir}/../..\n" +
				"exec_prefix=${prefix}\n" +
				"libdir=${prefix}/lib/datetime\n" +
				"includedir=${prefix}/include\n\n" +
				"Name: datetime\n" +
				"Description: A simple Date and time library built in C++\n" +
				"Version: 1.0.2\n" +
				"Libs: -L${libdir} -ldatetime -lm -lpthread -ldl\n" +
				"Cflags: -I${includedir} -DDATETIME_STATIC\n",
		},
		{
			name: "multiple flags and frameworks",
			spec: &Spec{
				Name:        "ntv2",
				Description: "AJA NTV2 SDK",
				Version:     "16.2",
				Libs: []string{
					"-L${libdir}", "-lajantv2", "-lpthread", "-lrt",
					"-framework", "CoreFoundation", "-framework", "Foundation", "-framework", "IOKit",
				},
				Cflags: []string{
					"-I${includedir}/ajalibraries",
					"-I${includedir}/ajalibraries/ajabase",
					"-DAJALinux", "-DAJA_LINUX", "-DNTV2_USE_STDINT",
				},
			},
			want: "prefix=${pcfiledir}/../..\n" +
				"exec_prefix=${prefix}\n" +
				"libdir=${prefix}/lib\n" +
				"includedir=${prefix}/include\n\n" +
				"Name: ntv2\n" +
				"Description: AJA NTV2 SDK\n" +
				"Version: 16.2\n" +
				"Libs: -L${libdir} -lajantv2 -lpthread -lrt -framework CoreFoundation -framework Foundation -framework IOKit\n" +
				"Cflags: -I${includedir}/ajalibraries -I${includedir}/ajalibraries/ajabase -DAJALinux -DAJA_LINUX -DNTV2_USE_STDINT\n",
		},
		{
			name: "URL",
			spec: &Spec{
				Name:        "libdmtx",
				Description: "Library for reading and writing Data Matrix barcodes",
				Version:     "0.7.8",
				URL:         "http://www.libdmtx.org/",
				Libs:        []string{"-L${libdir}", "-ldmtx", "-lm"},
				Cflags:      []string{"-I${includedir}"},
			},
			want: "prefix=${pcfiledir}/../..\n" +
				"exec_prefix=${prefix}\n" +
				"libdir=${prefix}/lib\n" +
				"includedir=${prefix}/include\n\n" +
				"Name: libdmtx\n" +
				"Description: Library for reading and writing Data Matrix barcodes\n" +
				"Version: 0.7.8\n" +
				"URL: http://www.libdmtx.org/\n" +
				"Libs: -L${libdir} -ldmtx -lm\n" +
				"Cflags: -I${includedir}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := New(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			n, err := file.WriteTo(&out)
			if err != nil {
				t.Fatal(err)
			}
			if n != int64(len(tt.want)) {
				t.Fatalf("WriteTo bytes = %d, want %d", n, len(tt.want))
			}
			if got := out.String(); got != tt.want {
				t.Fatalf("encoded file:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestNewDefaultsAndOrdersVariables(t *testing.T) {
	file, err := New(&Spec{
		Variables: map[string]string{
			"prefix": "/opt/demo",
			"zdir":   "${prefix}/z",
			"adir":   "${prefix}/a",
		},
		Name:        "demo",
		Description: "demo library",
		Version:     "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := file.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	wantPrefix := "prefix=/opt/demo\n" +
		"exec_prefix=${prefix}\n" +
		"libdir=${prefix}/lib\n" +
		"includedir=${prefix}/include\n" +
		"adir=${prefix}/a\n" +
		"zdir=${prefix}/z\n\n"
	if !strings.HasPrefix(out.String(), wantPrefix) {
		t.Fatalf("encoded variables:\n%s\nwant prefix:\n%s", out.String(), wantPrefix)
	}
}

func TestNewValidatesRequiredFields(t *testing.T) {
	valid := Spec{Name: "demo", Description: "demo library", Version: "1.0.0"}
	tests := []struct {
		name string
		spec *Spec
		want string
	}{
		{name: "nil spec", want: "spec is required"},
		{name: "name", spec: &Spec{Description: valid.Description, Version: valid.Version}, want: "name is required"},
		{name: "description", spec: &Spec{Name: valid.Name, Version: valid.Version}, want: "description is required"},
		{name: "version", spec: &Spec{Name: valid.Name, Description: valid.Description}, want: "version is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewValidatesVariablesAndProperties(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{name: "empty variable name", mutate: func(spec *Spec) { spec.Variables = map[string]string{"": "value"} }, want: "variable name is required"},
		{name: "invalid variable name", mutate: func(spec *Spec) { spec.Variables = map[string]string{"bad-name": "value"} }, want: "invalid variable name"},
		{name: "multiline variable", mutate: func(spec *Spec) { spec.Variables = map[string]string{"prefix": "one\ntwo"} }, want: "single line"},
		{name: "empty Libs entry", mutate: func(spec *Spec) { spec.Libs = []string{""} }, want: "empty value"},
		{name: "multiline Cflags entry", mutate: func(spec *Spec) { spec.Cflags = []string{"-DONE\n-DTWO"} }, want: "single line"},
		{name: "multiline URL", mutate: func(spec *Spec) { spec.URL = "https://example.com/\nnext" }, want: "URL must be a single line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &Spec{Name: "demo", Description: "demo library", Version: "1.0.0"}
			tt.mutate(spec)
			_, err := New(spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewWritesFragmentsVerbatim(t *testing.T) {
	file, err := New(&Spec{
		Name:        "demo",
		Description: "demo library",
		Version:     "1.0.0",
		Libs:        []string{"-L${libdir}", "-framework CoreFoundation"},
		Cflags:      []string{"-I${includedir}", `-DVALUE=\"quoted\"`},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := file.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Libs: -L${libdir} -framework CoreFoundation",
		`Cflags: -I${includedir} -DVALUE=\"quoted\"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("encoded file missing %q:\n%s", want, out.String())
		}
	}
}

type limitedWriter struct {
	n   int
	err error
}

func (w limitedWriter) Write(p []byte) (int, error) {
	if w.n > len(p) {
		return len(p), w.err
	}
	return w.n, w.err
}

func TestWriteToReportsWriterResults(t *testing.T) {
	file, err := New(&Spec{Name: "demo", Description: "demo library", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	n, err := file.WriteTo(limitedWriter{n: 3})
	if n != 3 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteTo = (%d, %v), want (3, %v)", n, err, io.ErrShortWrite)
	}

	wantErr := errors.New("write failed")
	n, err = file.WriteTo(limitedWriter{n: 4, err: wantErr})
	if n != 4 || !errors.Is(err, wantErr) {
		t.Fatalf("WriteTo = (%d, %v), want (4, %v)", n, err, wantErr)
	}
}

func TestPkgconfigLookupReadsEncodedFile(t *testing.T) {
	if _, err := exec.LookPath("pkg-config"); err != nil {
		t.Skip("pkg-config is not installed")
	}

	root := t.TempDir()
	pcDir := filepath.Join(root, "lib", "pkgconfig")
	if err := os.MkdirAll(pcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := New(&Spec{
		Name:        "llar-pcfile-test",
		Description: "pcfile integration test",
		Version:     "1.0.0",
		Libs:        []string{"-L${prefix}/lib/custom", "-lllar_pcfile_test", "-lm"},
		Cflags:      []string{"-I${prefix}/include/first", "-I${prefix}/include/second", "-DLLAR_PCFILE_TEST"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(pcDir, "llar-pcfile-test.pc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteTo(out); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	var metadata string
	env := append(os.Environ(), "PKG_CONFIG_PATH=")
	err = execbroker.Do(execbroker.Scope{Env: env}, func() error {
		pkgconfig.Use(root)
		var err error
		metadata, err = pkgconfig.Lookup("llar-pcfile-test")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := shellquote.Split(metadata)
	if err != nil {
		t.Fatal(err)
	}
	for i, flag := range got {
		if strings.HasPrefix(flag, "-I") || strings.HasPrefix(flag, "-L") {
			got[i] = flag[:2] + filepath.Clean(flag[2:])
		}
	}
	want := []string{
		"-I" + filepath.Join(root, "include", "first"),
		"-I" + filepath.Join(root, "include", "second"),
		"-DLLAR_PCFILE_TEST",
		"-L" + filepath.Join(root, "lib", "custom"),
		"-lllar_pcfile_test",
		"-lm",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pkgconfig.Lookup = %#v, want %#v", got, want)
	}
}
