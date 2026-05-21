package buildtarget

import "testing"

func TestParseLinuxTarget(t *testing.T) {
	target, err := Parse("arm64-linux")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if target.Arch != "arm64" || target.OS != "linux" {
		t.Fatalf("target = %+v, want arch=arm64 os=linux", target)
	}
}

func TestParseNoOSTarget(t *testing.T) {
	target, err := Parse("arm64")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if target.Arch != "arm64" || target.OS != "" {
		t.Fatalf("target = %+v, want arch=arm64 no os", target)
	}
}

func TestParseRejectsMalformedMatrix(t *testing.T) {
	for _, matrix := range []string{"", "-linux", "arm64-", "arm64-linux-debug"} {
		if _, err := Parse(matrix); err == nil {
			t.Fatalf("Parse(%q) error = nil, want error", matrix)
		}
	}
}

func TestTargetClassification(t *testing.T) {
	host := Platform{Arch: "arm64", OS: "darwin"}

	tests := []struct {
		name       string
		matrix     string
		wantNative bool
		wantGlibc  bool
	}{
		{name: "native", matrix: "arm64-darwin", wantNative: true, wantGlibc: false},
		{name: "cross linux", matrix: "amd64-linux", wantNative: false, wantGlibc: true},
		{name: "cross darwin", matrix: "amd64-darwin", wantNative: false, wantGlibc: false},
		{name: "no os", matrix: "arm64", wantNative: false, wantGlibc: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := Parse(tt.matrix)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := target.IsNative(host); got != tt.wantNative {
				t.Fatalf("IsNative = %v, want %v", got, tt.wantNative)
			}
			if got := target.NeedsDefaultGlibc(host); got != tt.wantGlibc {
				t.Fatalf("NeedsDefaultGlibc = %v, want %v", got, tt.wantGlibc)
			}
		})
	}
}
