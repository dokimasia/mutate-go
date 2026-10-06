// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build unix

package testbin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/testbin"
)

func TestProcess(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("ends the processes that the binary leaves running", func(t *testing.T) {
			t.Parallel()
			pidFile := filepath.Join(t.TempDir(), "pid")
			res := testbin.Run(context.Background(), testbin.Config{
				Binary: binary(t), Dir: t.TempDir(), Work: t.TempDir(),
				Env: fakeEnv(orphan, pidFileVar+"="+pidFile), Timeout: longDeadline,
			})
			assert.NoError(t, res.Err, "the run starts")
			assert.Equal(t, res.State.ExitCode(), 0, "the binary starts its child and exits")
			data, err := os.ReadFile(pidFile)
			assert.NoError(t, err, "the binary writes its child's process ID")
			pid, err := strconv.Atoi(string(data))
			assert.NoError(t, err, "the process ID is a number")
			assert.EventuallyTrue(t, 5*time.Second, func() bool {
				return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
			}, "the child that the binary left no longer exists")
		})
	})
}
