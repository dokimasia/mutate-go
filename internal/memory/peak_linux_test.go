// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package memory_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/memory"
)

// TestMain exits at once in a copy of the test binary that the tests start,
// so the copy's own peak is the peak of a small process.
func TestMain(m *testing.M) {
	if os.Getenv(childVar) != "" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// childVar makes the test binary exit at once.
const childVar = "MEMORY_TEST_CHILD"

// statusFile is the status file of the process that reads it, as a path
// under the root of the file system.
const statusFile = "proc/self/status"

// ballastMiB is the memory in MiB that the test touches beyond twice the
// error of the counters before it starts a copy of the test binary. The
// copy's ru_maxrss is then above the error.
const ballastMiB = 32

func TestPeak(t *testing.T) {
	t.Parallel()

	t.Run("Peak", func(t *testing.T) {
		t.Parallel()
		// slack is the error of the counters of resident pages in kiB. Each of
		// the 3 counters can differ by max(32, 2 × CPUs) pages on each CPU.
		cpus := runtime.NumCPU()
		slack := int64(3*cpus*max(32, 2*cpus)*os.Getpagesize()) >> 10
		ballast := make([]byte, 2*slack<<10+ballastMiB<<20)
		for i := range ballast {
			ballast[i] = 1
		}
		state := exited(t)
		runtime.KeepAlive(ballast)
		maxrss := state.SysUsage().(*syscall.Rusage).Maxrss
		// above is the highest mark in kiB below which Peak returns the
		// copy's ru_maxrss.
		above := maxrss - slack - 1

		tests := []struct {
			name string
			give fs.FS
			want int64
		}{
			{
				name: "returns the peak of a process whose peak is above the high-water mark of this process",
				give: files(map[string]string{statusFile: fmt.Sprintf("VmHWM:\t%8d kB\n", above)}),
				want: maxrss << 10,
			},
			{
				name: "returns 0 for a process whose peak is within the error of the high-water mark",
				give: files(map[string]string{statusFile: fmt.Sprintf("VmHWM:\t%8d kB\n", above+1)}),
			},
			{
				name: "returns 0 for a process whose peak is the high-water mark of this process",
				give: files(map[string]string{statusFile: fmt.Sprintf("VmHWM:\t%8d kB\n", maxrss)}),
			},
			{
				name: "returns 0 when the high-water mark is not in kB",
				give: files(map[string]string{statusFile: fmt.Sprintf("VmHWM:\t%8d MB\n", above)}),
			},
			{
				name: "returns 0 when the high-water mark has no unit",
				give: files(map[string]string{statusFile: fmt.Sprintf("VmHWM:\t%8d\n", above)}),
			},
			{
				name: "returns 0 when the high-water mark does not parse",
				give: files(map[string]string{statusFile: "VmHWM:\t    many kB\n"}),
			},
			{
				name: "returns 0 when the status of this process states no high-water mark",
				give: files(map[string]string{statusFile: "VmRSS:\t       1 kB\n"}),
			},
			{
				name: "returns 0 when the status of this process does not read",
				give: files(nil),
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				peak := memory.Peak(tt.give, state)
				assert.NotNil(t, peak, "Linux reports the peak")
				assert.Equal(t, *peak, tt.want, "Peak returns the peak that the status of this process tells apart")
			})
		}

		t.Run("returns 0 for a process that used less memory than this process", func(t *testing.T) {
			t.Parallel()
			peak := memory.Peak(os.DirFS("/"), exited(t))
			assert.NotNil(t, peak, "Linux reports the peak")
			assert.Equal(t, *peak, 0, "the copy's ru_maxrss is the high-water mark of the test process")
		})
	})
}

// exited runs a copy of the test binary that exits at once, and returns the
// state of the copy.
func exited(t *testing.T) *os.ProcessState {
	t.Helper()
	self, err := os.Executable()
	assert.NoError(t, err, "the test binary's path reads")
	cmd := exec.CommandContext(t.Context(), self)
	cmd.Env = append(os.Environ(), childVar+"=1")
	assert.NoError(t, cmd.Run(), "the copy of the test binary exits")
	return cmd.ProcessState
}
