// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// peakBytes returns the peak resident memory of the process that state
// describes, which wait4 reports in KiB on Linux. The memory ceiling of a
// test binary needs it.
func peakBytes(state *os.ProcessState) *int64 {
	peak := state.SysUsage().(*syscall.Rusage).Maxrss * 1024
	return &peak
}

// resident returns the resident memory of the process pid, from the
// resident pages that /proc/<pid>/statm states. It returns 0 for a process
// that has exited, whose file no longer reads.
func resident(pid int) int64 {
	data, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	var size, pages int64
	_, _ = fmt.Sscan(string(data), &size, &pages)
	return pages * int64(os.Getpagesize())
}
