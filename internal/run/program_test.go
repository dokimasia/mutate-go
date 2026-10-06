// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/testbin"
)

// goCommands is the number of go commands of a run of add with one test
// binary: go env, go list, go test -c, and go tool buildid for the test
// binary and for the engine's executable.
const goCommands = 5

// logName is the name of the file to which the wrapper in goLog appends.
const logName = "log"

func TestProgram(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("divides the engine's GOMAXPROCS among the workers", func(t *testing.T) {
			t.Parallel()
			procs := strconv.Itoa(max(1, runtime.GOMAXPROCS(0)/2))
			env := testbin.Setenv(os.Environ(), procsVar, expectedProcsVar+"="+procs)
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: procsTest}),
				run.Config{Env: env, Workers: 2})
			assert.Equal(t, verdicts(rec), addKilled, "each run has half of the engine's threads")
			assert.Empty(t, rec.Errors, "the control runs have them too")
		})

		tests := []struct {
			name           string
			procs, workers int
			want           int
		}{
			{"runs each test binary with Procs threads for one worker", 3, 1, 3},
			{"runs each test binary with Procs divided among the workers", 3, 2, 1},
			{"runs each test binary with at least one thread", 1, 2, 1},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				env := append(os.Environ(), expectedProcsVar+"="+strconv.Itoa(tt.want))
				rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: procsTest}),
					run.Config{Env: env, Procs: tt.procs, Workers: tt.workers})
				assert.Equal(t, verdicts(rec), addKilled, "each mutant's run has the expected threads")
				assert.Empty(t, rec.Errors, "the control runs have them too")
			})
		}

		t.Run("runs every go command with GOMAXPROCS set to Procs", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("the go wrapper is a shell script")
			}
			goCmd, err := exec.LookPath(goCommand)
			assert.NoError(t, err, "the go command is on the PATH")
			log := filepath.Join(t.TempDir(), logName)
			env := testbin.Setenv(
				os.Environ(),
				pathVar+"="+goLog+string(filepath.ListSeparator)+os.Getenv(pathVar),
				goVar+"="+goCmd,
				goLogVar+"="+log,
				expectedProcsVar+"=3",
			)
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: procsTest}),
				run.Config{Env: env, Procs: 3})
			assert.Equal(t, verdicts(rec), addKilled, "each mutant's run has three threads")
			data, err := os.ReadFile(log)
			assert.NoError(t, err, "the wrapper's log reads")
			lines := strings.Fields(string(data))
			assert.Length(t, lines, goCommands, "the run runs the go command for each step")
			assert.Equal(t, slices.Compact(lines), []string{"3"}, "every go command has three threads")
		})
	})
}
