// Package pcfile encodes pkg-config metadata for installed libraries.
package pcfile

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Spec describes the variables and properties written to a pkg-config file.
type Spec struct {
	// Variables defines pkg-config variables. Values override the default
	// prefix, exec_prefix, libdir, and includedir variables with the same name.
	Variables map[string]string

	// Name: The displayed name of the package.
	Name string
	// Description: A description of the package.
	Description string
	// Version: The version of the package.
	Version string
	// URL: A URL to a webpage for the package. This is used to recommend where
	// newer versions of the package can be acquired.
	URL string
	// Libs: Required linking flags for this package.
	Libs []string
	// Cflags: Required compiler flags. These flags are always used, regardless
	// of whether static compilation is requested.
	Cflags []string
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
		{name: "URL", value: spec.URL},
	}
	for _, field := range fields {
		if field.required && field.value == "" {
			return nil, fmt.Errorf("pcfile: %s is required", field.name)
		}
		if strings.ContainsAny(field.value, "\r\n") {
			return nil, fmt.Errorf("pcfile: %s must be a single line", field.name)
		}
	}

	defaultVariables := []struct {
		name  string
		value string
	}{
		{name: "prefix", value: "${pcfiledir}/../.."},
		{name: "exec_prefix", value: "${prefix}"},
		{name: "libdir", value: "${prefix}/lib"},
		{name: "includedir", value: "${prefix}/include"},
	}
	defaultNames := make(map[string]struct{}, len(defaultVariables))
	for _, variable := range defaultVariables {
		defaultNames[variable.name] = struct{}{}
	}
	for name, value := range spec.Variables {
		if name == "" {
			return nil, fmt.Errorf("pcfile: variable name is required")
		}
		for _, r := range name {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				return nil, fmt.Errorf("pcfile: invalid variable name %q", name)
			}
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("pcfile: variable %q must be a single line", name)
		}
	}

	fragments := []struct {
		name   string
		values []string
	}{
		{name: "Libs", values: spec.Libs},
		{name: "Cflags", values: spec.Cflags},
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

	escapeLiteral := func(value string) string {
		return strings.ReplaceAll(value, "${", "$${")
	}

	var out strings.Builder
	for _, variable := range defaultVariables {
		value := variable.value
		if override, ok := spec.Variables[variable.name]; ok {
			value = override
		}
		fmt.Fprintf(&out, "%s=%s\n", variable.name, value)
	}
	customVariables := make([]string, 0, len(spec.Variables))
	for name := range spec.Variables {
		if _, ok := defaultNames[name]; !ok {
			customVariables = append(customVariables, name)
		}
	}
	sort.Strings(customVariables)
	for _, name := range customVariables {
		fmt.Fprintf(&out, "%s=%s\n", name, spec.Variables[name])
	}
	out.WriteByte('\n')
	fmt.Fprintf(&out, "Name: %s\n", escapeLiteral(spec.Name))
	fmt.Fprintf(&out, "Description: %s\n", escapeLiteral(spec.Description))
	fmt.Fprintf(&out, "Version: %s\n", escapeLiteral(spec.Version))
	if spec.URL != "" {
		fmt.Fprintf(&out, "URL: %s\n", escapeLiteral(spec.URL))
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
	writeFragments("Libs", spec.Libs)
	writeFragments("Cflags", spec.Cflags)

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
