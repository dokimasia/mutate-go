// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package process_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/process"
)

func TestGroup(t *testing.T) {
	t.Parallel()

	t.Run("Start", func(t *testing.T) {
		t.Parallel()

		t.Run("copies the standard output and the standard error to their writers", func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			g, err := process.Start(command(writes), &stdout, &stderr)
			assert.NoError(t, err, "the command starts")
			assert.NoError(t, g.Wait(), "the command exits with the status 0")
			g.Drain(longDelay)
			expect.Equal(t, stdout.String(), outLine, "the standard output reaches its writer")
			expect.Equal(t, stderr.String(), errLine, "and the standard error reaches its own")
		})

		t.Run("copies both through one pipe in the order of the writes when they are one writer", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			g, err := process.Start(command(writes), &out, &out)
			assert.NoError(t, err, "the command starts")
			assert.NoError(t, g.Wait(), "the command exits with the status 0")
			g.Drain(longDelay)
			assert.Equal(t, out.String(), outLine+errLine, "the writer receives both in order")
		})

		t.Run("returns the error of a command that does not start", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			_, err := process.Start(exec.Command(filepath.Join(t.TempDir(), "missing")), &out, &out)
			assert.HasError(t, err, "a missing command does not start")
		})
	})

	t.Run("Wait", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the exit status of a command that fails", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			g, err := process.Start(command(exits3), &out, &out)
			assert.NoError(t, err, "the command starts")
			exit := assert.ErrorAs[*exec.ExitError](t, g.Wait(), "the command fails")
			assert.Equal(t, exit.ExitCode(), 3, "with its status")
			g.Drain(longDelay)
		})
	})

	t.Run("Kill", func(t *testing.T) {
		t.Parallel()

		t.Run("ends the command", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			g, err := process.Start(command(sleeps), &out, &out)
			assert.NoError(t, err, "the command starts")
			assert.CompletesWithin(t, ended, func(context.Context) error {
				g.Kill()
				assert.HasError(t, g.Wait(), "the killed command fails")
				g.Drain(longDelay)
				return nil
			}, "the command ends at once")
		})
	})
}
