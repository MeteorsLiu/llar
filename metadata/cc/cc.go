// Package cc parses LLAR C/C++ build metadata.
package cc

import (
	"fmt"
	"strings"

	"github.com/kballard/go-shellquote"
)

// Metadata is the parsed form of LLAR C/C++ raw metadata flags.
type Metadata struct {
	CCFLAGS []string
	CFLAGS  []string
	LDFLAGS []string

	sysroot string
}

// Parse parses raw C/C++ metadata flags.
func Parse(raw string) (Metadata, error) {
	flags, err := shellquote.Split(raw)
	if err != nil {
		return Metadata{}, err
	}

	var meta Metadata
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		switch {
		case strings.HasPrefix(flag, "--sysroot="):
			meta.sysroot = strings.TrimPrefix(flag, "--sysroot=")
		case strings.HasPrefix(flag, "-sysroot="):
			meta.sysroot = strings.TrimPrefix(flag, "-sysroot=")
		case flag == "--sysroot" || flag == "-sysroot" || flag == "-isysroot":
			if i+1 >= len(flags) {
				return Metadata{}, fmt.Errorf("%s requires an argument", flag)
			}
			i++
			meta.sysroot = flags[i]
		case isLinkFlag(flag):
			meta.LDFLAGS = append(meta.LDFLAGS, flag)
		default:
			meta.CCFLAGS = append(meta.CCFLAGS, flag)
		}
	}

	return meta, nil
}

// Sysroot returns the parsed sysroot directory.
func (m Metadata) Sysroot() string {
	return m.sysroot
}

func isLinkFlag(flag string) bool {
	return strings.HasPrefix(flag, "-L") ||
		strings.HasPrefix(flag, "-l") ||
		strings.HasPrefix(flag, "-Wl,")
}
