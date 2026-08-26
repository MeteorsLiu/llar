package pkgconfig

import (
	"fmt"
	"io"
	"maps"
	"slices"
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

type fragments struct {
	public  []string
	private []string
	shared  []string
}

// File is pkg-config metadata ready to be written.
type File struct {
	variables   map[string]string
	name        string
	description string
	version     string
	url         string
	libs        fragments
	cflags      fragments
}

// New validates spec and creates a relocatable pkg-config file.
func New(spec *Spec) (*File, error) {
	if spec == nil {
		return nil, fmt.Errorf("pkgconfig: spec is required")
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
			return nil, fmt.Errorf("pkgconfig: %s is required", field.name)
		}
		if strings.ContainsAny(field.value, "\r\n") {
			return nil, fmt.Errorf("pkgconfig: %s must be a single line", field.name)
		}
	}

	for name, value := range spec.Variables {
		if name == "" {
			return nil, fmt.Errorf("pkgconfig: variable name is required")
		}
		for _, r := range name {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				return nil, fmt.Errorf("pkgconfig: invalid variable name %q", name)
			}
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("pkgconfig: variable %q must be a single line", name)
		}
	}

	for _, field := range []struct {
		name   string
		values []string
	}{
		{name: "Libs", values: spec.Libs},
		{name: "Cflags", values: spec.Cflags},
	} {
		if err := validateFragments(field.name, field.values); err != nil {
			return nil, err
		}
	}

	return &File{
		variables:   maps.Clone(spec.Variables),
		name:        spec.Name,
		description: spec.Description,
		version:     spec.Version,
		url:         spec.URL,
		libs:        fragments{public: slices.Clone(spec.Libs)},
		cflags:      fragments{public: slices.Clone(spec.Cflags)},
	}, nil
}

// Libs returns the linking fragments.
func (f *File) Libs() *fragments {
	return &f.libs
}

// Cflags returns the compiler fragments.
func (f *File) Cflags() *fragments {
	return &f.cflags
}

// Private sets fragments used for static compilation or linking only.
func (f *fragments) Private(values []string) {
	f.private = slices.Clone(values)
}

// Shared sets fragments used for shared compilation or linking only.
func (f *fragments) Shared(values []string) {
	f.shared = slices.Clone(values)
}

func validateFragments(name string, values []string) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("pkgconfig: %s contains an empty value", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("pkgconfig: %s value %q must be a single line", name, value)
		}
	}
	return nil
}

// WriteTo encodes the pkg-config file and writes it to w.
func (f *File) WriteTo(w io.Writer) (n int64, err error) {
	for _, field := range []struct {
		name   string
		values []string
	}{
		{name: "Libs.private", values: f.libs.private},
		{name: "Libs.shared", values: f.libs.shared},
		{name: "Cflags.private", values: f.cflags.private},
		{name: "Cflags.shared", values: f.cflags.shared},
	} {
		if err := validateFragments(field.name, field.values); err != nil {
			return 0, err
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

	var out strings.Builder
	for _, variable := range defaultVariables {
		value := variable.value
		if override, ok := f.variables[variable.name]; ok {
			value = override
		}
		fmt.Fprintf(&out, "%s=%s\n", variable.name, value)
	}
	customVariables := make([]string, 0, len(f.variables))
	for name := range f.variables {
		if _, ok := defaultNames[name]; !ok {
			customVariables = append(customVariables, name)
		}
	}
	sort.Strings(customVariables)
	for _, name := range customVariables {
		fmt.Fprintf(&out, "%s=%s\n", name, f.variables[name])
	}
	out.WriteByte('\n')
	escapeLiteral := func(value string) string {
		return strings.ReplaceAll(value, "${", "$${")
	}
	fmt.Fprintf(&out, "Name: %s\n", escapeLiteral(f.name))
	fmt.Fprintf(&out, "Description: %s\n", escapeLiteral(f.description))
	fmt.Fprintf(&out, "Version: %s\n", escapeLiteral(f.version))
	if f.url != "" {
		fmt.Fprintf(&out, "URL: %s\n", escapeLiteral(f.url))
	}
	writeFragments := func(name string, values []string, always bool) {
		if len(values) == 0 && !always {
			return
		}
		out.WriteString(name)
		out.WriteByte(':')
		if len(values) > 0 {
			out.WriteByte(' ')
			out.WriteString(strings.Join(values, " "))
		}
		out.WriteByte('\n')
	}
	writeFragments("Libs", f.libs.public, true)
	writeFragments("Libs.private", f.libs.private, false)
	writeFragments("Libs.shared", f.libs.shared, false)
	writeFragments("Cflags", f.cflags.public, true)
	writeFragments("Cflags.private", f.cflags.private, false)
	writeFragments("Cflags.shared", f.cflags.shared, false)

	data := []byte(out.String())
	written, err := w.Write(data)
	if written > len(data) {
		panic("pkgconfig.File.WriteTo: invalid Write count")
	}
	if written != len(data) && err == nil {
		err = io.ErrShortWrite
	}
	return int64(written), err
}
