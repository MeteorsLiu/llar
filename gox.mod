xgo 1.6

project *_llar.gox ModuleF github.com/goplus/llar/formula

import github.com/goplus/llar/x/autotools
import github.com/goplus/llar/x/cmake
import github.com/goplus/llar/x/pkgconfig

project *_cmp.gox CmpApp github.com/goplus/llar/cmp

import golang.org/x/mod/semver
import github.com/goplus/llar/x/gnu
