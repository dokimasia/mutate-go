// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package memory

import (
	"io/fs"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// selfStatus is the status file of the process that reads it, as a path
// under the root of the file system. highWater is the label of its line of
// the high-water mark of the process's resident memory, in the unit kiB.
const (
	selfStatus = "proc/self/status"
	highWater  = "VmHWM:"
)

// rssCounters is the number of counters of Linux whose sum is the resident
// memory of a process: its anonymous, file and shared memory pages.
// countBatch is the least number of pages that a CPU counts before Linux
// adds them to the total of a counter.
const (
	rssCounters = 3
	countBatch  = 32
)

// Peak returns the peak resident memory of the process that state
// describes, in bytes, from the ru_maxrss that wait4 reports in kiB. At
// exec, Linux raises the ru_maxrss of a process to the high-water mark of
// the process that started it, so a ru_maxrss near the high-water mark of
// this process can be that mark. Peak reads the mark, VmHWM in
// proc/self/status under root. It returns 0 for a ru_maxrss that is not
// above the mark by more than the error of the counters of resident pages,
// and when the status does not read or has no mark in kiB that parses.
//
// Linux keeps each counter per CPU, and adds a CPU's count to the total once
// the count is max(32, 2 × CPUs) pages or more. Linux reads the totals at
// exec, and adds the counts of every CPU for the status. The mark of exec
// can then exceed the mark of the status by that many pages of each counter
// on each CPU that this process may use.
func Peak(root fs.FS, state *os.ProcessState) *int64 {
	usage := state.SysUsage().(*syscall.Rusage) //nolint:forcetypeassert // SysUsage returns a *syscall.Rusage on Unix
	peak := usage.Maxrss * kibibyte
	// When the status does not read or has no mark, mark keeps the value of
	// the peak, so the peak counts as the mark.
	mark := peak
	data, _ := fs.ReadFile(root, selfStatus)
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == highWater && fields[2] == kiB {
			if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				mark = kb * kibibyte
			}
		}
	}
	cpus := runtime.NumCPU()
	slack := int64(rssCounters*cpus*max(countBatch, 2*cpus)) * int64(os.Getpagesize())
	if peak <= mark+slack {
		peak = 0
	}
	return &peak
}
