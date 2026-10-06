// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// The pins of the starts of the messages of a failed control run.
const (
	openingFailure  = "the tests fail with no mutant active: "
	closingFailure  = "the tests fail with no mutant active after the mutant runs: "
	ordinaryFailure = "the tests fail in an ordinary build with no mutant active: "
)

// messageTail is the pin of the most bytes of a failed control run's output
// that a run error states.
const messageTail = 8 << 10

// The names in the run's work directory that the tests pin: the pattern of
// the work directory and the instrumented test binary of the package's own
// tests.
const (
	workGlob  = "mutate-*"
	ownBinary = "pkg.test"
)

// ordinaryBinary is the shell pattern of the argument that names the output
// of the ordinary control run's build.
const ordinaryBinary = "*/ordinary/pkg.test"

// slowConformance is a test of the package conformance that links count and
// sleeps a second in the opening control run, which raises its binary's
// deadline above 12 seconds.
var slowConformance = fmt.Sprintf(`package conformance

import (
	"os"
	"testing"
	"time"

	"fixture"
)

var _ = fixture.Count

func TestSlow(t *testing.T) {
	if os.Getenv(%q) != "" {
		time.Sleep(time.Second)
	}
}
`, render.TraceVar)

// markerTest is a test of add that fails when the file of markerVar exists,
// and creates it, so each run after the first fails.
var markerTest = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestMarker(t *testing.T) {
	marker := os.Getenv(%q)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the marker exists")
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	Add(2, 3)
}
`, markerVar)

// conformanceMarkerTest is markerTest in scale's package conformance.
var conformanceMarkerTest = fmt.Sprintf(`package conformance

import (
	"os"
	"testing"

	"fixture"
)

func TestMarker(t *testing.T) {
	marker := os.Getenv(%q)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the marker exists")
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.Scale(6, 3)
}
`, markerVar)

// exitMain is a test main that exits with the status 1 before any test runs.
const exitMain = `package fixture

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(1)
}
`

// ordinaryOnly is a test of add that fails in an ordinary build, where the
// protocol's mutant variable is set and the instrumented variable is not.
var ordinaryOnly = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "" && os.Getenv(%q) == "" {
		t.Fatal("a check of the ordinary build fails")
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, protocol.Variable, protocol.Instrumented)

// brokenWriter is a test of add that writes broken into the package's
// directory in the opening control run.
var brokenWriter = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "" {
		_ = os.WriteFile(%q, []byte(%q), 0o644)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, render.TraceVar, brokenFile, broken)

// hangingTest is a test of add that waits a minute in every run.
const hangingTest = `package fixture

import (
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	time.Sleep(time.Minute)
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`

func TestControl(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("sets the limits of a test binary from its opening control run", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, arithFiles), run.Config{})
			opening := rec.Control.Opening
			assert.Length(t, rec.Limits, 1, "the package's own test binary has limits")
			limits := rec.Limits[0]
			assert.Equal(t, limits.Target, "fixture", "the limits name the package whose tests the binary runs")
			// The deadline is a whole number of nanoseconds, so the comparison
			// allows the truncation to one and the rounding of the float.
			deadline := protocol.Limits.Deadline
			assert.CloseTo(t, limits.DeadlineSeconds, deadline.Factor*opening.Seconds+deadline.Seconds,
				2*time.Nanosecond.Seconds(),
				"the deadline is the protocol's factor times the control run's time, plus its constant")
			if runtime.GOOS != "linux" {
				assert.Nil(t, limits.MemoryCeilingBytes, "the engine measures no peak outside Linux")
				return
			}
			assert.NotNil(t, opening.PeakBytes, "the engine measures the control run's peak on Linux")
			memory := protocol.Limits.Memory
			ceiling := int64(memory.Factor*float64(*opening.PeakBytes)) + memory.Bytes
			assert.Equal(t, limits.MemoryCeilingBytes, &ceiling,
				"the ceiling is the protocol's factor times the control run's peak, plus its constant")
		})

		t.Run("ends a mutant's run of a test binary at that binary's deadline", func(t *testing.T) {
			t.Parallel()
			// The opening control run of the package conformance sleeps a
			// second, so its deadline is above 12 seconds, and the package's
			// own is below 3. Count's increment mutant hangs in the package's
			// own test binary, and the conformance tests do not call Count.
			dir := module(t, map[string]string{
				countFile:           count,
				countTestFile:       countTest,
				conformanceFile:     "package conformance\n",
				conformanceTestFile: slowConformance,
			})
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, verdicts(rec), countVerdicts, "the hanging mutant times out")
			assert.Empty(t, rec.Errors, "the run states no run error")
			assert.Length(t, rec.Limits, 2, "each test binary has its own limits")
			assert.Equal(t, rec.Limits[0].Target, "fixture", "the package's own binary comes first")
			assert.Equal(t, rec.Limits[1].Target, conformance, "and the binary of the package conformance second")
			own := rec.Limits[0].DeadlineSeconds
			assert.InRange(t, rec.Limits[1].DeadlineSeconds-own, 8, math.MaxFloat64,
				"the slow control run gives the package conformance the longer deadline")
			for _, m := range rec.Mutants {
				if m.Verdict == spec.TimedOut {
					assert.InRange(t, *m.Seconds, 0, own+5, "the hang ends at the deadline of the package's own binary")
				}
			}
		})

		t.Run("states control-failed when a test fails with no mutant active", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(arithFiles, map[string]string{
				arithTestFile: arithTest + "\nfunc TestBroken(t *testing.T) {\n\tfor i := 0; i < 10; i++ {\n" +
					"\t\tt.Log(\"line\", i, \"" + strings.Repeat("x", 1000) + "\")\n\t}\n\tt.Fatal(\"broken\")\n}\n",
			}))
			rec := runIn(t, dir, run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorControl}, "the opening control run fails")
			prefix := openingFailure + "TestBroken failed\n"
			assert.That(t, rec.Errors[0].Message).
				HasPrefix(prefix, "the message names the failed test").
				Contains("broken", "and contains the test's output").
				Length(len(prefix)+messageTail, "and keeps the end of the output alone")
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
			assert.Nil(t, rec.Control, "a failed opening control run states no control run")
			assert.Nil(t, rec.Score, "a run with a run error has no score")
		})

		t.Run("states control-failed without a test when the test binary fails before its tests", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{arithFile: arith, mainTestFile: exitMain}), run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorControl}, "the opening control run fails")
			assert.HasPrefix(t, rec.Errors[0].Message, openingFailure+"the test binary failed\n",
				"the message names the binary, because no test failed")
		})

		t.Run("states control-failed when a signal ends the opening control run", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("the fixture sends a Unix signal")
			}
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: killer}),
				run.Config{Env: append(os.Environ(), alwaysVar+"=1")})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorControl}, "the opening control run fails")
			assert.HasPrefix(t, rec.Errors[0].Message,
				openingFailure+"the run ended on the signal killed, which the engine did not send\n",
				"the message names the signal")
		})

		t.Run("states not-instrumented when the trace lacks the start mark", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(arithFiles, map[string]string{mainTestFile: traceRemover("fixture")}))
			rec := runIn(t, dir, run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorNotInstrumented}, "the binary did not trace")
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
		})

		t.Run("names the package of the suite whose test fails with no mutant active", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				conformanceTestFile: "package conformance\n\nimport (\n\t\"testing\"\n\n\t_ \"fixture\"\n)\n\n" +
					"func TestBroken(t *testing.T) {\n\tt.Error(\"broken\")\n}\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorControl}, "the opening control run fails")
			assert.HasPrefix(t, rec.Errors[0].Message, openingFailure+"fixture/conformance: TestBroken failed\n",
				"the message names the test after its package")
		})

		t.Run("names the package of the suite whose test binary does not trace", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{conformanceMainFile: traceRemover("conformance")}))
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorNotInstrumented}, "the binary did not trace")
			assert.HasPrefix(t, rec.Errors[0].Message,
				"the opening control run's trace of fixture/conformance has no start mark",
				"the message names the package")
		})

		t.Run("states closing-control-failed when a mutant run changes what the tests depend on", func(t *testing.T) {
			t.Parallel()
			marker := filepath.Join(t.TempDir(), markerName)
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: markerTest}),
				run.Config{Env: append(os.Environ(), markerVar+"="+marker)})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorClosing}, "the closing control run fails")
			assert.Equal(t, verdicts(rec), "Add sbr-zero 0: killed [TestMarker]\nAdd aor 0: killed [TestMarker]\n",
				"each mutant's run fails on the marker")
			assert.Nil(t, rec.Control.Closing, "a failed closing control run states no time")
			assert.Nil(t, rec.Score, "a run with a run error has no score")
		})

		t.Run("names the package of the suite whose test fails after the mutant runs", func(t *testing.T) {
			t.Parallel()
			marker := filepath.Join(t.TempDir(), markerName)
			dir := module(t, with(scale, map[string]string{conformanceTestFile: conformanceMarkerTest}))
			rec := runIn(t, dir, run.Config{
				Env:   append(os.Environ(), markerVar+"="+marker),
				Suite: []string{conformancePattern},
			})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorClosing}, "the closing control run fails")
			assert.Equal(t, verdicts(rec),
				"Scale sbr-zero 0: killed [TestScaleByOne]\nScale aor 0: killed [fixture/conformance: TestMarker]\n",
				"the survivor of the package's own tests fails on the marker")
			assert.HasPrefix(t, rec.Errors[0].Message, closingFailure+"fixture/conformance: TestMarker failed\n",
				"the message names the test after its package")
		})

		t.Run("states ordinary-control-failed when a test fails in the ordinary build", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: ordinaryOnly}),
				run.Config{Confirm: true})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorOrdinary}, "the ordinary control run fails")
			assert.Equal(t, verdicts(rec), addNotRun, "no mutant runs")
			assert.HasPrefix(t, rec.Errors[0].Message, ordinaryFailure+"TestAdd failed\n",
				"the message names the failed test")
			assert.Equal(t, rec.Mutants[0].Reason, "the ordinary control run failed",
				"the record states why no mutant runs")
			assert.Nil(t, rec.Control.Ordinary, "a failed ordinary control run states no time")
		})

		t.Run("states the build error when the unchanged source does not build for the ordinary control run",
			func(t *testing.T) {
				t.Parallel()
				rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: brokenWriter}),
					run.Config{Confirm: true})
				assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorBuild, spec.ErrorChangedFiles},
					"the ordinary build fails on the file that the opening control run wrote")
				assert.That(t, rec.Errors[0].Message).
					HasPrefix("the ordinary build of the unchanged source fails: ", "the message names the build").
					Contains(brokenFile, "and the file that does not compile")
				assert.Equal(t, rec.Mutants[0].Reason, "the ordinary build failed",
					"the record states why no mutant runs")
			},
		)

		t.Run("runs no control run when the caller's deadline has passed", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, arithFiles), run.Config{Deadline: time.Now().Add(-time.Second)})
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, openingReason, "the record states why no mutant runs")
			assert.Nil(t, rec.Control, "the run runs no control run")
			assert.Empty(t, rec.Errors, "the caller's deadline is no run error")
		})

		t.Run("states no run error when the caller's deadline ends the opening control run", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: hangingTest}),
				run.Config{Deadline: time.Now().Add(4 * time.Second)})
			assert.Equal(t, verdicts(rec), addNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, openingReason, "the record states why no mutant runs")
			assert.Nil(t, rec.Control, "the cut control run states no control run")
			assert.Empty(t, rec.Errors, "the caller's deadline is no run error")
		})

		t.Run("runs no ordinary control run when the time left is shorter than its deadline and the closing run's",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and less than 10 are
				// left after the opening control run.
				rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowOpening}),
					run.Config{Confirm: true, Deadline: time.Now().Add(10 * time.Second)})
				assert.Equal(t, verdicts(rec), addNotRun, "no mutant runs")
				assert.Equal(t, rec.Mutants[0].Reason,
					"the caller's deadline leaves too little time for the ordinary control run",
					"the record states why no mutant runs")
				assert.Nil(t, rec.Control.Ordinary, "the ordinary control run does not run")
				assert.Empty(t, rec.Errors, "the caller's deadline is no run error")
			},
		)

		t.Run("stops at a cancellation during the opening control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelInPhase(t, openingPhase, run.Config{})
			assert.Equal(t, verdicts(rec), addNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Nil(t, rec.Control, "the cut control run states no control run")
			assert.Empty(t, rec.Errors, "a cancellation is no run error")
		})

		t.Run("stops at a cancellation during the ordinary control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelInPhase(t, ordinaryPhase, run.Config{Confirm: true})
			assert.Equal(t, verdicts(rec), addNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Nil(t, rec.Control.Ordinary, "the cut ordinary control run states no time")
			assert.Empty(t, rec.Errors, "a cancellation is no run error")
		})

		t.Run("stops at a cancellation during the closing control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelInPhase(t, closingPhase, run.Config{})
			assert.Equal(t, verdicts(rec), addKilled, "each mutant has its verdict")
			assert.Nil(t, rec.Control.Closing, "the cut closing control run states no time")
			assert.Empty(t, rec.Errors, "a cancellation is no run error")
		})

		t.Run("stops a run whose ordinary build the caller cancels", func(t *testing.T) {
			t.Parallel()
			env, marker := blocking(t, ordinaryBinary)
			rec := cancelAt(t, module(t, arithFiles), run.Config{Env: env, Confirm: true}, marker)
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Nil(t, rec.Control.Ordinary, "the cut ordinary control run states no time")
			assert.Empty(t, rec.Errors, "a cancellation is no run error")
		})
	})
}

// TestControlEnv sets TMPDIR, the state of the whole process, so neither it
// nor its subtests run in parallel.
func TestControlEnv(t *testing.T) {
	t.Run("Run", func(t *testing.T) {
		t.Run("states closing-control-failed when the closing control run does not start", func(t *testing.T) {
			base := t.TempDir()
			t.Setenv(tmpVar, base)
			// The fourth mutant's verdict removes the test binary, so the
			// closing control run does not start.
			runs := 0
			var removed error
			rec := runIn(t, module(t, arithFiles), run.Config{Verdict: func(m record.Mutant, _ int, _ string) {
				if m.Verdict != spec.Killed && m.Verdict != spec.Survived {
					return
				}
				if runs++; runs == 4 {
					bins, _ := filepath.Glob(filepath.Join(base, workGlob, ownBinary))
					removed = os.ErrNotExist
					for _, bin := range bins {
						removed = os.Remove(bin)
					}
				}
			}})
			assert.NoError(t, removed, "the test binary is removed")
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorClosing}, "the closing control run fails")
			assert.HasPrefix(t, rec.Errors[0].Message, closingFailure+"the run did not start: ",
				"the message states that the binary did not start")
			assert.Equal(t, runs, 4, "each of the four mutants ran")
		})
	})
}
