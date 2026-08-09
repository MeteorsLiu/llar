package c

import "testing"

func TestToolchain(t *testing.T) {
	toolchain := NewToolchain("cc", "c++", "ar", "ranlib", "nm", "strip")
	for _, test := range []struct {
		name string
		got  string
		want string
	}{
		{name: "CC", got: toolchain.CC(), want: "cc"},
		{name: "CXX", got: toolchain.CXX(), want: "c++"},
		{name: "Archiver", got: toolchain.Archiver(), want: "ar"},
		{name: "Ranlib", got: toolchain.Ranlib(), want: "ranlib"},
		{name: "NM", got: toolchain.NM(), want: "nm"},
		{name: "Strip", got: toolchain.Strip(), want: "strip"},
	} {
		if test.got != test.want {
			t.Fatalf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
}
