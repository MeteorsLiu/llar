package buildtarget

import (
	"fmt"
	"strings"
)

type Platform struct {
	Arch string
	OS   string
}

func Parse(matrix string) (Platform, error) {
	if matrix == "" {
		return Platform{}, fmt.Errorf("invalid matrix %q: empty", matrix)
	}
	parts := strings.Split(matrix, "-")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return Platform{}, fmt.Errorf("invalid matrix %q: empty arch", matrix)
		}
		return Platform{Arch: parts[0]}, nil
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return Platform{}, fmt.Errorf("invalid matrix %q: want <arch>-<os>", matrix)
		}
		return Platform{Arch: parts[0], OS: parts[1]}, nil
	default:
		return Platform{}, fmt.Errorf("invalid matrix %q: want <arch> or <arch>-<os>", matrix)
	}
}

func (p Platform) IsNative(host Platform) bool {
	return p.Arch == host.Arch && p.OS != "" && p.OS == host.OS
}

func (p Platform) NeedsDefaultGlibc(host Platform) bool {
	return p.OS == "linux" && !p.IsNative(host)
}
