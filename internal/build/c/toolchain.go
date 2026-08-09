package c

// Toolchain contains prepared C-family command paths.
type Toolchain struct {
	cc       string
	cxx      string
	archiver string
	ranlib   string
	nm       string
	strip    string
}

// NewToolchain creates a Toolchain from prepared command paths.
func NewToolchain(cc, cxx, archiver, ranlib, nm, strip string) Toolchain {
	return Toolchain{
		cc:       cc,
		cxx:      cxx,
		archiver: archiver,
		ranlib:   ranlib,
		nm:       nm,
		strip:    strip,
	}
}

func (t Toolchain) CC() string       { return t.cc }
func (t Toolchain) CXX() string      { return t.cxx }
func (t Toolchain) Archiver() string { return t.archiver }
func (t Toolchain) Ranlib() string   { return t.ranlib }
func (t Toolchain) NM() string       { return t.nm }
func (t Toolchain) Strip() string    { return t.strip }
