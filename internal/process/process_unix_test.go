// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build unix

package process_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/process"
)

// escapes starts a copy of itself that sleeps in a session of its own,
// outside its process group, writes the copy's process ID, and exits.
const escapes = "escapes"

// shortDelay is the delay of a drain that a process outside the group
// holds open.
const shortDelay = 200 * time.Millisecond

// platformFakes are the behaviours of the fake command that need Unix.
var platformFakes = map[string]func() int{
	escapes: func() int { return startChild(&syscall.SysProcAttr{Setsid: true}) },
}

// lockedBuffer is a buffer that a group's copy writes while a test reads
// it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the buffer.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the buffer's content.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestProcess(t *testing.T) {
	t.Parallel()

	t.Run("Start", func(t *testing.T) {
		t.Parallel()

		t.Run("starts the command in a process group of its own", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			cmd := command(sleeps)
			g, err := process.Start(cmd, &out, &out)
			assert.NoError(t, err, "the command starts")
			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			g.Kill()
			assert.HasError(t, g.Wait(), "the killed command fails")
			g.Drain(longDelay)
			assert.NoError(t, err, "the command's group reads")
			assert.Equal(t, pgid, cmd.Process.Pid, "the command leads a group of its own")
		})
	})

	t.Run("Wait", func(t *testing.T) {
		t.Parallel()

		t.Run("ends the processes that the command left in its group", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			g, err := process.Start(command(leaves), &out, &out)
			assert.NoError(t, err, "the command starts")
			assert.NoError(t, g.Wait(), "the command exits with the status 0")
			assert.CompletesWithin(t, ended, func(context.Context) error {
				g.Drain(longDelay)
				return nil
			}, "the output ends with the group, long before the drain's delay")
			child := childPID(t, out.String())
			assert.EventuallyTrue(t, ended, func() bool { return !alive(child) },
				"the child that the command left no longer runs")
		})
	})

	t.Run("Kill", func(t *testing.T) {
		t.Parallel()

		t.Run("ends the processes that the command started", func(t *testing.T) {
			t.Parallel()
			var out lockedBuffer
			g, err := process.Start(command(waits), &out, &out)
			assert.NoError(t, err, "the command starts")
			assert.EventuallyTrue(t, ended, func() bool { return out.String() != "" },
				"the command writes its child's process ID")
			child := childPID(t, out.String())
			g.Kill()
			assert.HasError(t, g.Wait(), "the killed command fails")
			g.Drain(longDelay)
			assert.EventuallyTrue(t, ended, func() bool { return !alive(child) },
				"the command's child ends with the group")
		})
	})

	t.Run("Drain", func(t *testing.T) {
		t.Parallel()

		t.Run("closes the output after its delay when a process outside the group keeps it open", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			g, err := process.Start(command(escapes), &out, &out)
			assert.NoError(t, err, "the command starts")
			assert.NoError(t, g.Wait(), "the command exits with the status 0")
			start := time.Now()
			g.Drain(shortDelay)
			waited := time.Since(start)
			child := childPID(t, out.String())
			t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
			expect.InRange(t, waited, float64(shortDelay), float64(ended), "the drain waits its delay for the output")
			expect.True(t, alive(child), "while the child outside the group still runs")
		})
	})
}

// alive reports whether the process pid exists.
func alive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
