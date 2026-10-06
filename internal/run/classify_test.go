// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// movedSuffix ends the name to which a test moves a fixture's directory.
const movedSuffix = ".moved"

// pausingCountTest is a test of count whose parallel subtests pause until
// TestCount's function returns, so they never continue while Count hangs.
const pausingCountTest = `package fixture

import "testing"

func TestCount(t *testing.T) {
	t.Run("a", func(t *testing.T) { t.Parallel() })
	t.Run("b", func(t *testing.T) { t.Parallel() })
	if Count(3) != 3 {
		t.Error("Count(3) != 3")
	}
}
`

// parallelSleeper is a test file of add whose TestSlow sleeps a minute
// under a mutant, in parallel with TestAdd, which fails at once.
var parallelSleeper = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	t.Parallel()
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSlow(t *testing.T) {
	t.Parallel()
	if os.Getenv(%q) != "0" {
		time.Sleep(time.Minute)
	}
}
`, protocol.Variable)

// steps is a package whose initialization runs a loop that its increment
// and condition mutants never end.
var steps = map[string]string{
	"steps.go": `package fixture

var steps = count(3)

func count(n int) int {
	c := 0
	for c != n {
		c++
	}
	return c
}

func Steps() int { return steps }
`,
	"steps_test.go": `package fixture

import "testing"

func TestSteps(t *testing.T) {
	if Steps() != 3 {
		t.Error("Steps() != 3")
	}
}
`,
}

// exitTest returns a test of add that exits with the status code when Add is
// wrong.
func exitTest(code int) string {
	return fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		os.Exit(%d)
	}
}
`, code)
}

// grow is a package whose loop's mutants grow a buffer without end.
var grow = map[string]string{
	"grow.go": `package fixture

func Grow(n int) int {
	var buf []byte
	for i := 0; i != n; i++ {
		buf = append(buf, make([]byte, 1<<20)...)
	}
	return len(buf)
}
`,
	// TestGrow sleeps half a second in the control runs, so the deadline of
	// a mutant's run, above 7 seconds, leaves the mutants that grow without
	// end the time to cross the memory ceiling first, also when other test
	// binaries load the machine.
	"grow_test.go": fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestGrow(t *testing.T) {
	if os.Getenv(%q) == "0" {
		time.Sleep(500 * time.Millisecond)
	}
	if Grow(3) != 3<<20 {
		t.Error("Grow(3) != 3 MiB")
	}
}
`, protocol.Variable),
}

func TestClassify(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("gives timed-out to a mutant that hangs past the deadline", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{countFile: count, countTestFile: countTest}),
				run.Config{Workers: 2})
			assert.Equal(t, verdicts(rec), countVerdicts, "the mutant that never ends the loop times out")
		})

		t.Run("names only the running test of a mutant that hangs while parallel subtests pause", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{countFile: count, countTestFile: pausingCountTest}),
				run.Config{})
			assert.Equal(t, verdicts(rec), countVerdicts, "the paused subtests are not among the tests")
		})

		t.Run("ends a mutant's run at the first failed test while another test runs", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: parallelSleeper}),
				run.Config{Procs: 2})
			assert.Equal(t, verdicts(rec), addKilled, "TestAdd kills each mutant")
			assert.Empty(t, rec.Errors, "the run states no run error")
			for _, m := range rec.Mutants {
				assert.InRange(t, *m.Seconds, 0, rec.Limits[0].DeadlineSeconds,
					"the run ends at the failure, before the deadline")
			}
		})

		t.Run("gives timed-out to a mutant that hangs while the package initializes", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, steps), run.Config{Workers: 2})
			assert.Equal(t, verdicts(rec), `count sbr-delete 0: killed [TestSteps]
count ror-true 0: timed-out
count ror-false 0: killed [TestSteps]
count uoi-incdec 0: timed-out
count sbr-zero 0: killed [TestSteps]
Steps sbr-zero 0: killed [TestSteps]
`, "a hang before the first test names no test")
		})

		t.Run("kills a mutant whose test ends the binary with status 0", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: exitTest(0)}), run.Config{})
			assert.Equal(t, verdicts(rec), addKilled, "an exit with status 0 during a test fails the test")
		})

		t.Run("names the running test of a mutant whose test exits with status 1", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: exitTest(1)}), run.Config{})
			assert.Equal(t, verdicts(rec), addKilled, "the test that was running fails")
		})

		t.Run("gives error to a mutant whose run does not start", func(t *testing.T) {
			t.Parallel()
			// The first verdict of a mutant without coverage moves the package's
			// directory away, so the first mutant's run does not start, and its
			// verdict moves the directory back.
			dir := module(t, arithFiles)
			moved := dir + movedSuffix
			var away, back sync.Once
			var moveErr, backErr error
			rec := runIn(t, dir, run.Config{Verdict: func(m record.Mutant, _ int, _ string) {
				switch m.Verdict {
				case spec.NoCoverage:
					away.Do(func() { moveErr = os.Rename(dir, moved) })
				case spec.Error:
					back.Do(func() { backErr = os.Rename(moved, dir) })
				}
			}})
			assert.NoError(t, moveErr, "the directory moves away")
			assert.NoError(t, backErr, "the directory moves back")
			assert.Equal(t, verdicts(rec), `Add sbr-zero 0: error
Add aor 0: killed [TestAdd]
Sub sbr-zero 0: survived
Sub aor 0: survived
Unused sbr-zero 0: no-coverage
Unused aor 0: no-coverage
`, "the run that does not start ends in an error")
			assert.HasPrefix(t, rec.Mutants[0].Reason, "the run did not start: ", "the reason states the error")
			assert.Nil(t, rec.Mutants[0].Seconds, "a run that did not start states no time")
		})

		t.Run("gives error to a mutant whose run a signal that the engine did not send ends", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("the fixture sends a Unix signal")
			}
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: killer}), run.Config{})
			assert.Equal(t, verdicts(rec), "Add sbr-zero 0: error\nAdd aor 0: error\n", "the signal ends each run")
			assert.Equal(t, rec.Mutants[0].Reason, "the run ended on the signal killed, which the engine did not send",
				"the reason names the signal")
			assert.Nil(t, rec.Score, "a run with a mutant in error has no score")
		})

		t.Run("gives exhausted to a mutant whose run crosses the memory ceiling", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS != "linux" {
				t.Skip("the engine applies a memory ceiling on Linux alone")
			}
			rec := runIn(t, module(t, grow), run.Config{Workers: 2})
			assert.Equal(t, verdicts(rec), `Grow sbr-delete 0: killed [TestGrow]
Grow ror-true 0: exhausted [TestGrow]
Grow ror-false 0: killed [TestGrow]
Grow uoi-incdec 0: exhausted [TestGrow]
Grow sbr-delete 1: killed [TestGrow]
Grow sbr-zero 0: killed [TestGrow]
`, "the mutants that grow without end cross the ceiling")
		})
	})
}
