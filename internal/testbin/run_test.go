// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package testbin_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/testbin"
)

// The limits of the runs of the fake test binary: a deadline that a run
// which ends on its own never reaches, a backup delay for the runs that the
// deadline ends, and the time that a run which Run ends early completes
// within.
const (
	longDeadline = time.Minute
	shortBackup  = 100 * time.Millisecond
	ended        = 30 * time.Second
)

// ceiling is the memory ceiling of a run of the fake test binary that
// grows to 256 MiB.
const ceiling = 128 << 20

// windowsOS is the GOOS of Windows, where Run ends the binary alone.
const windowsOS = "windows"

// orphanDeadline is the deadline of a run whose binary leaves a process
// that keeps the output open. It is shorter than the 5 s for which Run reads
// output after an exit, and longer than the 1 s that a binary of the race
// detector sleeps when it exits.
const orphanDeadline = 3 * time.Second

// runFake runs the fake test binary with the behaviour mode and cfg, whose
// Binary, Work and Env it sets, within the time ended.
func runFake(t *testing.T, ctx context.Context, mode string, cfg testbin.Config) *testbin.Result {
	t.Helper()
	cfg.Binary, cfg.Work = binary(t), t.TempDir()
	cfg.Env = fakeEnv(mode, cfg.Env...)
	if cfg.Dir == "" {
		cfg.Dir = t.TempDir()
	}
	var res *testbin.Result
	assert.CompletesWithin(t, ended, func(context.Context) error {
		res = testbin.Run(ctx, cfg)
		return nil
	}, "the run ends within its limits")
	return res
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the state, the time and the output of a run that exits", func(t *testing.T) {
			t.Parallel()
			res := runFake(t, context.Background(), passes, testbin.Config{Timeout: longDeadline})
			assert.NoError(t, res.Err, "the run starts")
			expect.Equal(t, res.Ended, testbin.Exited, "the run ends on its own")
			expect.Equal(t, res.State.ExitCode(), 0, "with the status 0")
			expect.InRange(t, res.Seconds, 0, ended.Seconds(), "the run states its wall time")
			expect.Equal(t, res.Output.Tests, []string{"TestA"}, "the output states the test that ran")
			expect.Contains(t, res.Output.Tail, logLine, "and keeps the line that the test wrote")
		})

		t.Run("returns the exit status of a binary that fails", func(t *testing.T) {
			t.Parallel()
			res := runFake(t, context.Background(), exits3, testbin.Config{Timeout: longDeadline})
			expect.Equal(t, res.Ended, testbin.Exited, "the run ends on its own")
			expect.Equal(t, res.State.ExitCode(), 3, "with the binary's status")
		})

		t.Run("ends the run at the first failed test under StopAtFailure", func(t *testing.T) {
			t.Parallel()
			res := runFake(t, context.Background(), failsAndHangs,
				testbin.Config{Timeout: longDeadline, StopAtFailure: true})
			expect.Equal(t, res.Ended, testbin.Failed, "the run ends at the failure")
			expect.Equal(t, res.Output.Failed, []string{"TestA"}, "and names the failed test")
		})

		t.Run("lets a run with a failed test go on without StopAtFailure", func(t *testing.T) {
			t.Parallel()
			res := runFake(t, context.Background(), failsAndHangs,
				testbin.Config{Timeout: 200 * time.Millisecond, Backup: shortBackup})
			expect.Equal(t, res.Ended, testbin.Deadline, "the run lasts until its deadline")
		})

		t.Run("ends the run at the deadline and the backup delay", func(t *testing.T) {
			t.Parallel()
			res := runFake(t, context.Background(), hangs,
				testbin.Config{Timeout: 100 * time.Millisecond, Backup: shortBackup})
			expect.Equal(t, res.Ended, testbin.Deadline, "the run ends at the backup deadline")
			expect.InRange(t, res.Seconds, 0.2, ended.Seconds(), "after the deadline and the backup delay")
		})

		t.Run("ends the run when the caller's context ends", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			res := runFake(t, ctx, hangs, testbin.Config{Timeout: longDeadline})
			expect.Equal(t, res.Ended, testbin.Cancelled, "the run ends with the caller's context")
		})

		t.Run("ends the run at the binary's exit while its child keeps the output open", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == windowsOS {
				t.Skip("Run ends the binary alone on Windows")
			}
			pidFile := filepath.Join(t.TempDir(), "pid")
			cfg := testbin.Config{
				Env:     []string{pidFileVar + "=" + pidFile},
				Timeout: orphanDeadline,
				Backup:  shortBackup,
			}
			res := runFake(t, context.Background(), orphan, cfg)
			expect.Equal(t, res.Ended, testbin.Exited, "the run ends with the binary, before its deadline")
			expect.Equal(t, res.State.ExitCode(), 0, "with the binary's status")
			expect.InRange(t, res.Seconds, 0, orphanDeadline.Seconds(), "within the deadline")
			data, err := os.ReadFile(pidFile)
			assert.NoError(t, err, "the binary writes its child's process ID")
			pid, err := strconv.Atoi(string(data))
			assert.NoError(t, err, "the process ID is a number")
			assert.EventuallyTrue(t, 5*time.Second, func() bool {
				p, err := os.FindProcess(pid)
				return err != nil || p.Signal(syscall.Signal(0)) != nil
			}, "the child that the binary left no longer exists")
		})

		t.Run("ends the run when its resident memory crosses the ceiling", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS != "linux" {
				t.Skip("the package reads a process's resident memory on Linux alone")
			}
			res := runFake(t, context.Background(), grows, testbin.Config{Timeout: longDeadline, Ceiling: ceiling})
			expect.Equal(t, res.Ended, testbin.Memory, "the run ends at its memory ceiling")
		})

		t.Run("runs the binary in its directory with a temporary directory of its own", func(t *testing.T) {
			t.Parallel()
			dir, work := t.TempDir(), t.TempDir()
			res := testbin.Run(context.Background(), testbin.Config{
				Binary: binary(t), Dir: dir, Work: work, Env: fakeEnv(environment), Timeout: longDeadline,
			})
			assert.NoError(t, res.Err, "the run starts")
			lines := strings.Split(res.Output.Tail, "\n")
			assert.InRange(t, len(lines), 4, 100, "the binary writes its directory and three variables")
			expect.Equal(t, lines[0], dir, "the binary runs in the directory")
			expect.Equal(t, filepath.Dir(lines[1]), work, "TMPDIR is a directory in the work directory")
			expect.Equal(t, lines[2], lines[1], "TMP names the same directory")
			expect.Equal(t, lines[3], lines[1], "and so does TEMP")
			entries, err := os.ReadDir(work)
			assert.NoError(t, err, "the work directory reads")
			expect.Empty(t, entries, "Run removes the temporary directory")
		})

		t.Run("returns the error of a binary that does not start", func(t *testing.T) {
			t.Parallel()
			res := testbin.Run(context.Background(), testbin.Config{
				Binary: filepath.Join(t.TempDir(), "missing"), Work: t.TempDir(), Timeout: longDeadline,
			})
			assert.HasError(t, res.Err, "a missing binary does not start")
			expect.Nil(t, res.State, "a run that does not start has no state")
		})

		t.Run("returns the error of a work directory that does not exist", func(t *testing.T) {
			t.Parallel()
			res := testbin.Run(context.Background(), testbin.Config{
				Binary: binary(t), Work: filepath.Join(t.TempDir(), "missing"), Timeout: longDeadline,
			})
			assert.HasError(t, res.Err, "a run without its temporary directory does not start")
		})
	})
}
