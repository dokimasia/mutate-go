// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build go1.22

package enumerate

import "go/types"

// unalias returns the type that t denotes when t is an alias.
func unalias(t types.Type) types.Type { return types.Unalias(t) }
