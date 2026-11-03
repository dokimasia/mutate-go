// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// scaleFile is the source file of confirmFiles.
const scaleFile = "scale.go"

// confirmFiles is a package whose tests of double and triple run only in an
// ordinary build, as a test of a property of the build does, and whose
// other test calls double without checking the result. No test calls
// quarter.
var confirmFiles = map[string]string{
	scaleFile: `package fixture

func double(n int) int { return n * 2 }

func half(n int) int { return n / 2 }

func triple(n int) int { return n * 3 }

func quarter(n int) int { return n / 4 }
`,
	"scale_test.go": fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestDouble(t *testing.T) {
	if os.Getenv(%[1]q) != "" {
		t.Skip("a test of the ordinary build")
	}
	if double(3) != 6 {
		t.Error("double(3) != 6")
	}
}

func TestCalls(t *testing.T) {
	double(3)
	if half(4) == 0 {
		t.Error("half(4) == 0")
	}
}

func TestTriple(t *testing.T) {
	if os.Getenv(%[1]q) != "" {
		t.Skip("a test of the ordinary build")
	}
	if triple(2) != 6 {
		t.Error("triple(2) != 6")
	}
}
`, protocol.Instrumented),
}

// slowCountTest is countTest with a second test, TestSlow, that sleeps a
// second in every run and calls nothing of count.
const slowCountTest = `package fixture

import (
	"testing"
	"time"
)

func TestCount(t *testing.T) {
	if Count(3) != 3 {
		t.Error("Count(3) != 3")
	}
}

func TestSlow(t *testing.T) {
	time.Sleep(time.Second)
}
`

// signFiles is a package whose test runs four subtests of Sign. zero and
// positive both fail when Sign returns 0 for a number that is not negative,
// and wait waits a minute when Sign gives -3 another sign than -1, after
// negative failed.
var signFiles = map[string]string{
	"sign.go": `package fixture

func Sign(x int) int {
	if x < 0 {
		return -1
	}
	return 1
}
`,
	"sign_test.go": `package fixture

import (
	"testing"
	"time"
)

func TestSign(t *testing.T) {
	t.Run("negative", func(t *testing.T) {
		if Sign(-2) != -1 {
			t.Error("Sign(-2) != -1")
		}
	})
	t.Run("zero", func(t *testing.T) {
		if Sign(0) != 1 {
			t.Error("Sign(0) != 1")
		}
	})
	t.Run("positive", func(t *testing.T) {
		if Sign(2) != 1 {
			t.Error("Sign(2) != 1")
		}
	})
	t.Run("wait", func(t *testing.T) {
		if Sign(-3) != -1 {
			time.Sleep(time.Minute)
		}
	})
}
`,
}

// waitingConformance is a test of scale's package conformance that waits a
// minute when Scale is wrong.
const waitingConformance = `package conformance

import (
	"testing"
	"time"

	"fixture"
)

func TestScale(t *testing.T) {
	if fixture.Scale(6, 3) != 18 {
		time.Sleep(time.Minute)
	}
}
`

// confirmOverlay is the shell pattern of the overlay argument of a
// confirmation's build.
const confirmOverlay = "*/confirm*/overlay.json"

// confirmPrefix is the pin of the start of the name of a confirmation's
// directory in the run's work directory, which the mutant's index in the
// record ends.
const confirmPrefix = "confirm"

func TestMutant(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("confirms each survivor in its ordinary build", func(t *testing.T) {
			t.Parallel()
			dir := module(t, confirmFiles)
			lines := []selection.Lines{{Path: filepath.Join(dir, scaleFile), First: 3, Last: 5}}
			rec := runIn(t, dir, run.Config{Confirm: true, Lines: lines})
			assert.Equal(t, verdicts(rec), `double sbr-zero 0: killed [TestDouble] confirmed
double aor 0: killed [TestDouble] confirmed
half sbr-zero 0: killed [TestCalls]
half aor 0: survived confirmed
triple sbr-zero 0: not-selected
triple aor 0: not-selected
quarter sbr-zero 0: not-selected
quarter aor 0: not-selected
`, "the test of the ordinary build detects the survivors of double")
			assert.Empty(t, rec.Errors, "the run states no run error")
			assert.NotNil(t, rec.Control.Ordinary, "the ordinary control run runs")
			assert.NotNil(t, rec.Control.Closing, "the closing control run runs")
			for _, m := range rec.Mutants[:4] {
				assert.NotNil(t, m.Seconds, "each mutant that ran states its time")
			}
		})

		t.Run("confirms each mutant without coverage in its ordinary build", func(t *testing.T) {
			t.Parallel()
			// Only TestTriple, which skips in the instrumented build, calls
			// triple. No test calls quarter.
			dir := module(t, confirmFiles)
			lines := []selection.Lines{{Path: filepath.Join(dir, scaleFile), First: 7, Last: 9}}
			rec := runIn(t, dir, run.Config{Confirm: true, Lines: lines})
			assert.Equal(t, verdicts(rec), `double sbr-zero 0: not-selected
double aor 0: not-selected
half sbr-zero 0: not-selected
half aor 0: not-selected
triple sbr-zero 0: killed [TestTriple] confirmed
triple aor 0: killed [TestTriple] confirmed
quarter sbr-zero 0: no-coverage confirmed
quarter aor 0: no-coverage confirmed
`, "the test of the ordinary build detects the mutants of triple")
			assert.Empty(t, rec.Errors, "the run states no run error")
			for _, m := range rec.Mutants[4:] {
				assert.NotNil(t, m.Seconds, "each confirmed mutant states its time")
			}
		})

		t.Run("confirms no mutant without Confirm", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, confirmFiles), run.Config{})
			assert.Equal(t, verdicts(rec), `double sbr-zero 0: survived
double aor 0: survived
half sbr-zero 0: killed [TestCalls]
half aor 0: survived
triple sbr-zero 0: no-coverage
triple aor 0: no-coverage
quarter sbr-zero 0: no-coverage
quarter aor 0: no-coverage
`, "the tests of the ordinary build skip")
			assert.Nil(t, rec.Control.Ordinary, "the run runs no ordinary control run")
		})

		t.Run("runs for a mutant only the test binaries that executed its site", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"triple.go": "package fixture\n\nfunc Triple(x int) int { return x * 3 }\n",
				conformanceTestFile: `package conformance

import (
	"testing"

	"fixture"
)

func TestTriple(t *testing.T) {
	if fixture.Triple(2) != 6 {
		t.Error("Triple(2) != 6")
	}
}
`,
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, verdicts(rec), `Scale sbr-zero 0: killed [TestScaleByOne]
Scale aor 0: survived
Triple sbr-zero 0: killed [fixture/conformance: TestTriple]
Triple aor 0: killed [fixture/conformance: TestTriple]
`, "the conformance tests do not run for Scale's mutants")
			assert.Empty(t, rec.Errors, "the run states no run error")
		})

		t.Run("names the first test that fails without AllKillers", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, signFiles), run.Config{})
			assert.Equal(t, verdicts(rec), `Sign sbr-delete 0: killed [TestSign/negative]
Sign ror-boundary 0: killed [TestSign/zero]
Sign ror-false 0: killed [TestSign/negative]
Sign sbr-zero 0: killed [TestSign/negative]
Sign sbr-zero 1: killed [TestSign/zero]
`, "each run ends at its first failed test")
		})

		t.Run("names every test that fails under AllKillers", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, signFiles), run.Config{AllKillers: true, Workers: 2})
			assert.Equal(t, verdicts(rec), `Sign sbr-delete 0: killed [TestSign/negative]
Sign ror-boundary 0: killed [TestSign/zero] all killers
Sign ror-false 0: killed [TestSign/negative]
Sign sbr-zero 0: killed [TestSign/negative]
Sign sbr-zero 1: killed [TestSign/zero TestSign/positive] all killers
`, "each run lists the subtests that failed before it ended")
			assert.Empty(t, rec.Errors, "the run states no run error")
		})

		t.Run("goes on with the next test binary after a kill under AllKillers", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, scale), run.Config{AllKillers: true, Suite: []string{conformancePattern}})
			assert.Equal(t, verdicts(rec),
				"Scale sbr-zero 0: killed [TestScaleByOne fixture/conformance: TestScale] all killers\n"+
					"Scale aor 0: killed [fixture/conformance: TestScale] all killers\n",
				"the tests of both binaries kill the mutant that returns 0")
		})

		t.Run("ends a kill under AllKillers at a test binary whose run does not end on its own", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{conformanceTestFile: waitingConformance}))
			rec := runIn(t, dir, run.Config{AllKillers: true, Suite: []string{conformancePattern}, Workers: 2})
			assert.Equal(t, verdicts(rec), `Scale sbr-zero 0: killed [TestScaleByOne]
Scale aor 0: timed-out [fixture/conformance: TestScale]
`, "the conformance test waits until its binary's deadline")
		})

		t.Run("confirms each survivor under AllKillers in a run that names every killer", func(t *testing.T) {
			t.Parallel()
			dir := module(t, confirmFiles)
			lines := []selection.Lines{{Path: filepath.Join(dir, scaleFile), First: 3, Last: 5}}
			rec := runIn(t, dir, run.Config{Confirm: true, AllKillers: true, Lines: lines})
			assert.Equal(t, verdicts(rec), `double sbr-zero 0: killed [TestDouble] all killers confirmed
double aor 0: killed [TestDouble] all killers confirmed
half sbr-zero 0: killed [TestCalls] all killers
half aor 0: survived confirmed
triple sbr-zero 0: not-selected
triple aor 0: not-selected
quarter sbr-zero 0: not-selected
quarter aor 0: not-selected
`, "the confirmation of a survivor names every test that fails in its ordinary build")
		})

		t.Run("ends the run of a mutant's covering tests at a deadline from their own times", func(t *testing.T) {
			t.Parallel()
			// TestSlow sleeps a second in every run, which raises the test
			// binary's deadline above 12 seconds. The mutant that hangs in
			// TestCount, whose run alone takes milliseconds, ends about 2
			// seconds after its start.
			rec := runIn(t, module(t, map[string]string{countFile: count, countTestFile: slowCountTest}), run.Config{})
			assert.Equal(t, verdicts(rec), countVerdicts, "the hanging mutant times out")
			for _, m := range rec.Mutants {
				if m.Verdict == spec.TimedOut {
					assert.InRange(t, *m.Seconds, 0, rec.Limits[0].DeadlineSeconds/2,
						"the hang ends long before the binary's deadline")
				}
			}
		})

		t.Run("gives not-viable to a survivor whose ordinary build the toolchain rejects", func(t *testing.T) {
			t.Parallel()
			dir := module(t, arithFiles)
			var once sync.Once
			var written error
			rec := runIn(t, dir, run.Config{Confirm: true, Verdict: func(m record.Mutant, _ int, _ string) {
				if m.Verdict == spec.Killed {
					once.Do(func() {
						written = os.WriteFile(filepath.Join(dir, brokenFile), []byte(broken), fileMode)
					})
				}
			}})
			assert.NoError(t, written, "the file that does not compile is written")
			assert.Equal(t, verdicts(rec), addKilled+`Sub sbr-zero 0: not-viable confirmed
Sub aor 0: not-viable confirmed
Unused sbr-zero 0: not-viable confirmed
Unused aor 0: not-viable confirmed
`, "the ordinary builds after the write fail")
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorChangedFiles}, "the run states the written file")
			assert.That(t, rec.Mutants[2].Reason).
				Contains(brokenFile, "the reason is the toolchain's message").
				Matches(`^[^#]`, "without the line of the go command that names the package")
		})

		t.Run("stops a confirmation whose build the caller cancels", func(t *testing.T) {
			t.Parallel()
			// The confirmation's build waits a minute after it creates the
			// marker, so the caller cancels the run during that build.
			env, marker := blocking(t, confirmOverlay)
			rec := cancelAt(t, module(t, arithFiles), run.Config{Env: env, Confirm: true}, marker)
			assert.Equal(t, verdicts(rec), addKilled+`Sub sbr-zero 0: not-run
Sub aor 0: not-run
Unused sbr-zero 0: not-run
Unused aor 0: not-run
`, "no mutant has a verdict after the cancel")
			assert.Equal(t, rec.Mutants[2].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.NotNil(t, rec.Sample, "the record states the sample of the mutants that ran")
			assert.Equal(t, rec.Sample.Before, new(slices.Min(keys(rec)[2:])),
				"the sample ends at the least key that did not run")
		})

		t.Run("stops at a cancellation during a mutant's run", func(t *testing.T) {
			t.Parallel()
			rec := cancelInPhase(t, mutantPhase, run.Config{})
			assert.Equal(t, verdicts(rec), addNotRun, "no mutant has a verdict after the cancel")
			assert.Equal(t, rec.Mutants[0].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Nil(t, rec.Mutants[0].Seconds, "the cut run states no time")
			assert.Nil(t, rec.Control.Closing, "a cancelled run runs no closing control run")
		})
	})
}

// TestMutantEnv sets the temporary directory of the whole process, so
// neither it nor its subtests run in parallel.
func TestMutantEnv(t *testing.T) {
	t.Run("Run", func(t *testing.T) {
		t.Run("gives error to a survivor whose ordinary build is not written", func(t *testing.T) {
			base := t.TempDir()
			useTempDir(t, base)
			// The files take the places of the directories of the confirmations
			// of Sub's mutants, the record's mutants 2 and 3.
			var once sync.Once
			var written error
			verdict := func(m record.Mutant, _ int, _ string) {
				if m.Verdict != spec.Killed {
					return
				}
				once.Do(func() {
					works, _ := filepath.Glob(filepath.Join(base, workGlob))
					written = os.ErrNotExist
					for _, work := range works {
						for _, i := range []int{2, 3} {
							written = os.WriteFile(filepath.Join(work, confirmPrefix+strconv.Itoa(i)), nil, fileMode)
						}
					}
				})
			}
			rec := runIn(t, module(t, arithFiles), run.Config{Confirm: true, Verdict: verdict})
			assert.NoError(t, written, "the files in place of the confirmations' directories are written")
			assert.Equal(t, verdicts(rec), addKilled+`Sub sbr-zero 0: error confirmed
Sub aor 0: error confirmed
Unused sbr-zero 0: no-coverage confirmed
Unused aor 0: no-coverage confirmed
`, "the confirmations of Sub's mutants end in an error")
			assert.HasPrefix(t, rec.Mutants[2].Reason, "the ordinary build was not written: render: ",
				"the reason states the error of the write")
		})
	})
}
