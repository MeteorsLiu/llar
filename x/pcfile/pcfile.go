// Package pcfile encodes pkg-config metadata for installed libraries.
package pcfile

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/kballard/go-shellquote"
)

// Spec describes the compiler and linker interface published by a package.
type Spec struct {
	// Name: The displayed name of the package.
	Name string
	// Description: A description of the package.
	Description string
	// Version: The version of the package.
	Version string
	// Homepage is encoded as URL: A URL to a webpage for the package. This is
	// used to recommend where newer versions of the package can be acquired.
	Homepage string

	// IncludeDirs emits each package-relative path as -I${prefix}/<path> in Cflags.
	IncludeDirs []string
	// LibraryDirs emits each package-relative path as -L${prefix}/<path> in Libs.
	LibraryDirs []string
	// Libraries emits each name as -l<name> in Libs.
	Libraries []string
	// Defines emits each definition as -D<definition> in Cflags.
	Defines []string
	// Frameworks emits each name as -framework <name> in Libs.
	Frameworks []string
}

// File is validated pkg-config metadata ready to be written.
type File struct {
	data []byte
}

// New validates spec and encodes a relocatable pkg-config file.
func New(spec *Spec) (*File, error) {
	if spec == nil {
		return nil, fmt.Errorf("pcfile: spec is required")
	}

	fields := []struct {
		name     string
		value    string
		required bool
	}{
		{name: "name", value: spec.Name, required: true},
		{name: "description", value: spec.Description, required: true},
		{name: "version", value: spec.Version, required: true},
		{name: "homepage", value: spec.Homepage},
	}
	for _, field := range fields {
		if field.required && field.value == "" {
			return nil, fmt.Errorf("pcfile: %s is required", field.name)
		}
		if strings.ContainsAny(field.value, "\r\n") {
			return nil, fmt.Errorf("pcfile: %s must be a single line", field.name)
		}
	}

	directories := []struct {
		name   string
		values []string
	}{
		{name: "includeDirs", values: spec.IncludeDirs},
		{name: "libraryDirs", values: spec.LibraryDirs},
	}
	for _, field := range directories {
		for _, dir := range field.values {
			if dir == "" {
				return nil, fmt.Errorf("pcfile: %s contains an empty path", field.name)
			}
			clean := path.Clean(dir)
			if path.IsAbs(dir) || clean == ".." || strings.HasPrefix(clean, "../") {
				return nil, fmt.Errorf("pcfile: %s path %q must stay relative to the package root", field.name, dir)
			}
			if strings.ContainsAny(dir, "\r\n") {
				return nil, fmt.Errorf("pcfile: %s path %q must be a single line", field.name, dir)
			}
		}
	}

	fragments := []struct {
		name   string
		values []string
	}{
		{name: "libraries", values: spec.Libraries},
		{name: "defines", values: spec.Defines},
		{name: "frameworks", values: spec.Frameworks},
	}
	for _, field := range fragments {
		for _, value := range field.values {
			if value == "" {
				return nil, fmt.Errorf("pcfile: %s contains an empty value", field.name)
			}
			if strings.ContainsAny(value, "\r\n") {
				return nil, fmt.Errorf("pcfile: %s value %q must be a single line", field.name, value)
			}
		}
	}

	libs := make([]string, 0, len(spec.LibraryDirs)+len(spec.Libraries)+2*len(spec.Frameworks))
	for _, dir := range spec.LibraryDirs {
		libs = append(libs, "-L${prefix}/"+shellquote.Join(dir))
	}
	for _, library := range spec.Libraries {
		libs = append(libs, "-l"+shellquote.Join(library))
	}
	for _, framework := range spec.Frameworks {
		libs = append(libs, "-framework", shellquote.Join(framework))
	}

	cflags := make([]string, 0, len(spec.IncludeDirs)+len(spec.Defines))
	for _, dir := range spec.IncludeDirs {
		cflags = append(cflags, "-I${prefix}/"+shellquote.Join(dir))
	}
	for _, define := range spec.Defines {
		cflags = append(cflags, "-D"+shellquote.Join(define))
	}

	escapeLiteral := func(value string) string {
		return strings.ReplaceAll(value, "${", "$${")
	}

	var out strings.Builder
	out.WriteString("prefix=${pcfiledir}/../..\n\n")
	fmt.Fprintf(&out, "Name: %s\n", escapeLiteral(spec.Name))
	fmt.Fprintf(&out, "Description: %s\n", escapeLiteral(spec.Description))
	fmt.Fprintf(&out, "Version: %s\n", escapeLiteral(spec.Version))
	if spec.Homepage != "" {
		fmt.Fprintf(&out, "URL: %s\n", escapeLiteral(spec.Homepage))
	}
	writeFragments := func(name string, fragments []string) {
		out.WriteString(name)
		out.WriteByte(':')
		if len(fragments) > 0 {
			out.WriteByte(' ')
			out.WriteString(strings.Join(fragments, " "))
		}
		out.WriteByte('\n')
	}
	writeFragments("Libs", libs)
	writeFragments("Cflags", cflags)

	return &File{data: []byte(out.String())}, nil
}

// WriteTo writes the encoded pkg-config file to w.
func (f *File) WriteTo(w io.Writer) (n int64, err error) {
	written, err := w.Write(f.data)
	if written > len(f.data) {
		panic("pcfile.File.WriteTo: invalid Write count")
	}
	if written != len(f.data) && err == nil {
		err = io.ErrShortWrite
	}
	return int64(written), err
}
