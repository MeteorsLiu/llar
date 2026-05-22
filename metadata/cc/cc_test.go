package cc

import (
	"reflect"
	"testing"
)

func TestParseClassifiesMetadataFlags(t *testing.T) {
	meta, err := Parse("-I/include -DDEBUG -L/lib -lz -Wl,--as-needed")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !reflect.DeepEqual(meta.CCFLAGS, []string{"-I/include", "-DDEBUG"}) {
		t.Fatalf("CCFLAGS = %#v", meta.CCFLAGS)
	}
	if len(meta.CFLAGS) != 0 {
		t.Fatalf("CFLAGS = %#v, want empty", meta.CFLAGS)
	}
	if !reflect.DeepEqual(meta.LDFLAGS, []string{"-L/lib", "-lz", "-Wl,--as-needed"}) {
		t.Fatalf("LDFLAGS = %#v", meta.LDFLAGS)
	}
	if got := meta.Sysroot(); got != "" {
		t.Fatalf("Sysroot = %q, want empty", got)
	}
}

func TestParseExtractsSysrootDir(t *testing.T) {
	meta, err := Parse("--sysroot=/sdk -I/include -L/lib")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := meta.Sysroot(); got != "/sdk" {
		t.Fatalf("Sysroot = %q, want /sdk", got)
	}
	if !reflect.DeepEqual(meta.CCFLAGS, []string{"-I/include"}) {
		t.Fatalf("CCFLAGS = %#v", meta.CCFLAGS)
	}
	if !reflect.DeepEqual(meta.LDFLAGS, []string{"-L/lib"}) {
		t.Fatalf("LDFLAGS = %#v", meta.LDFLAGS)
	}
}

func TestParseSysrootFormsAndLastWins(t *testing.T) {
	meta, err := Parse("--sysroot /old -isysroot /middle -sysroot=/new")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := meta.Sysroot(); got != "/new" {
		t.Fatalf("Sysroot = %q, want /new", got)
	}
	if len(meta.CCFLAGS) != 0 {
		t.Fatalf("CCFLAGS = %#v, want empty", meta.CCFLAGS)
	}
	if len(meta.LDFLAGS) != 0 {
		t.Fatalf("LDFLAGS = %#v, want empty", meta.LDFLAGS)
	}
}

func TestParseSingleDashSysrootWithSeparateArg(t *testing.T) {
	meta, err := Parse("-sysroot /sdk -I/include")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := meta.Sysroot(); got != "/sdk" {
		t.Fatalf("Sysroot = %q, want /sdk", got)
	}
	if !reflect.DeepEqual(meta.CCFLAGS, []string{"-I/include"}) {
		t.Fatalf("CCFLAGS = %#v", meta.CCFLAGS)
	}
}

func TestParseReturnsErrorForMissingSysrootArg(t *testing.T) {
	if _, err := Parse("-I/include --sysroot"); err == nil {
		t.Fatal("Parse error = nil, want error")
	}
	if _, err := Parse("-isysroot"); err == nil {
		t.Fatal("Parse error = nil, want error")
	}
	if _, err := Parse("-sysroot"); err == nil {
		t.Fatal("Parse error = nil, want error")
	}
}

func TestParseSplitsQuotedFlags(t *testing.T) {
	meta, err := Parse(`-DNAME="hello world" -Wl,-rpath,/path\ with\ space -lfoo`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !reflect.DeepEqual(meta.CCFLAGS, []string{`-DNAME=hello world`}) {
		t.Fatalf("CCFLAGS = %#v", meta.CCFLAGS)
	}
	if !reflect.DeepEqual(meta.LDFLAGS, []string{`-Wl,-rpath,/path with space`, "-lfoo"}) {
		t.Fatalf("LDFLAGS = %#v", meta.LDFLAGS)
	}
}
