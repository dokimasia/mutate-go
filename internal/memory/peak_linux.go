// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package memory

import (
	"os"
	"syscall"
)

// Peak returns the peak resident memory of the process that state
// describes, in bytes, from the ru_maxrss that wait4 reports in kiB.
func Peak(state *os.ProcessState) *int64 {
	peak := state.SysUsage().(*syscall.Rusage).Maxrss * kibibyte
	return &peak
}
