// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build !linux

package run

import "os"

// peakBytes returns nil. The engine measures no process's memory on this
// platform, so a test binary has no memory ceiling.
func peakBytes(*os.ProcessState) *int64 { return nil }

// resident returns 0. The engine measures no process's memory on this
// platform, and applies no ceiling.
func resident(int) int64 { return 0 }
