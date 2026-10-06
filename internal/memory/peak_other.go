// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build !linux

package memory

import "os"

// Peak returns nil. The package measures no process's peak on this
// platform, so a test binary has no memory ceiling.
func Peak(*os.ProcessState) *int64 { return nil }
