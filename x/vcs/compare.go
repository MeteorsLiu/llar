// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package vcs provides VCS-backed version comparison to comparator formulas.
package vcs

import "github.com/goplus/llar/mod/module"

// CompareFunc compares versions using the repository bound by LLAR and
// compareTag to order tags.
func CompareFunc(a, b module.Version, compareTag func(a, b string) int) int {
	panic("vcs.CompareFunc called outside a module comparator")
}
