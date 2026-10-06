// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package memory_test

import (
	"os"
	"os/exec"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/memory"
)

// TestMain exits at once in a copy of the test binary that the tests start,
// so the copy's peak is the peak of a small process.
func TestMain(m *testing.M) {
	if os.Getenv(childVar) != "" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// childVar makes the test binary exit at once.
const childVar = "MEMORY_TEST_CHILD"

func TestPeak(t *testing.T) {
	t.Parallel()

	t.Run("Peak", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the peak resident memory of a process that exited", func(t *testing.T) {
			t.Parallel()
			self, err := os.Executable()
			assert.NoError(t, err, "the test binary's path reads")
			cmd := exec.Command(self)
			cmd.Env = append(os.Environ(), childVar+"=1")
			assert.NoError(t, cmd.Run(), "the copy of the test binary exits")
			peak := memory.Peak(cmd.ProcessState)
			assert.NotNil(t, peak, "Linux reports the peak")
			assert.InRange(t, *peak, 1<<20, 1<<30, "a small Go process peaks between 1 MiB and 1 GiB")
		})
	})
}
