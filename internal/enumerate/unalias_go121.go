// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build !go1.22

package enumerate

import "go/types"

// unalias returns t. The type checker of Go 1.21 represents no alias as a
// type of its own.
func unalias(t types.Type) types.Type { return t }
