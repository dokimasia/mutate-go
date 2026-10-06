// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// subFirst is arith with Sub before Add. The keys of Add's mutants sort
// before the keys of Sub's, so the record order of the mutants that run is
// not the order of their keys.
const subFirst = `package fixture

func Sub(a, b int) int { return a - b }

func Add(a, b int) int { return a + b }
`

// sleepingMutant is a test of add that sleeps a minute under a mutant before
// it checks Add.
var sleepingMutant = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "0" {
		time.Sleep(time.Minute)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, protocol.Variable)

// slowSleepingMutant is slowOpening that also sleeps a minute under a
// mutant, so the first mutant runs until the test binary's deadline.
var slowSleepingMutant = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if os.Getenv(%q) != "0" {
		time.Sleep(time.Minute)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, render.TraceVar, protocol.Variable)

// unconfirmedReason is the pin of the reason of a survivor whose
// confirmation does not start because the caller's deadline is too near.
const unconfirmedReason = "the caller's deadline leaves too little time for the confirmation and the closing " +
	"control run"

// survivors is a package whose test calls Sub without checking it, so both
// of Sub's mutants survive.
var survivors = map[string]string{
	arithFile:     "package fixture\n\nfunc Sub(a, b int) int { return a - b }\n",
	arithTestFile: "package fixture\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {\n\tSub(1, 1)\n}\n",
}

// uncovered is a package whose test calls nothing, so neither of Add's
// mutants has coverage.
var uncovered = map[string]string{
	addFile:     add,
	addTestFile: "package fixture\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
}

func TestSchedule(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("starts the mutants' runs in the order of their keys", func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var ran []string
			rec := runIn(t, module(t, map[string]string{arithFile: subFirst, arithTestFile: arithTest}),
				run.Config{Verdict: func(m record.Mutant, _ int, _ string) {
					mu.Lock()
					defer mu.Unlock()
					if m.Seconds != nil {
						ran = append(ran, m.Key)
					}
				}})
			assert.Length(t, ran, 4, "the four mutants of Add and Sub run")
			assert.Pairwise(t, ran, func(earlier, later string) bool { return earlier < later },
				"the runs end in the order of the mutants' keys")
			assert.False(t, slices.IsSorted(keys(rec)), "the record lists the mutants in another order")
		})

		t.Run("starts only the first mutants in key order under Sample", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, arithFiles), run.Config{Sample: 2})
			var ran, stopped []string
			for _, m := range rec.Mutants {
				switch m.Verdict {
				case spec.NotRun:
					stopped = append(stopped, m.Key)
					assert.Equal(t, m.Reason, "the caller limits the run to 2 mutants",
						"the record states the caller's limit")
				case spec.Killed, spec.Survived:
					ran = append(ran, m.Key)
				default:
					// Unused's mutants have no coverage, and neither run nor
					// wait.
				}
			}
			assert.Length(t, ran, 2, "two mutants run")
			assert.Length(t, stopped, 2, "the two other mutants of Add and Sub do not")
			assert.Pairwise(t, []string{slices.Max(ran), slices.Min(stopped)},
				func(earlier, later string) bool { return earlier < later },
				"each mutant that ran has a lesser key than each mutant that did not")
			assert.NotNil(t, rec.Sample, "the record states the sample")
			assert.Equal(t, rec.Sample.Before, slices.Min(stopped), "the sample ends at the least key that did not run")
		})

		t.Run("states the limit of a run that Sample alone ends", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, arithFiles), run.Config{Sample: 2})
			assert.NotNil(t, rec.Sample, "a run that Sample ends states a sample")
			assert.Equal(t, rec.Sample.Limit, new(2), "the sample states the caller's limit")
			assert.False(t, rec.Failed(), "a run that Sample alone ends does not fail")
			assert.That(t, rec.Score).
				NotNil("a run that Sample alone ends has a score").
				Equal(rec.Sample.Score, "the run's score is the sample's")
		})

		t.Run("states no limit for a run that the caller's deadline ends under Sample", func(t *testing.T) {
			t.Parallel()
			// A mutant's deadline is above 7 seconds, and less than 10 are left
			// after the opening control run, so the deadline ends the runs
			// before Sample does.
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowOpening}),
				run.Config{Sample: 5, Deadline: time.Now().Add(10 * time.Second)})
			assert.Equal(t, verdicts(rec), addNotRun, "the deadline ends the runs before the first mutant")
			assert.NotNil(t, rec.Sample, "a run that the deadline ends states a sample")
			assert.Nil(t, rec.Sample.Limit, "the sample of a deadline states no limit")
			assert.True(t, rec.Failed(), "a run that the deadline ends fails")
		})

		t.Run("states no limit for a sampled run whose started mutant the caller cancels", func(t *testing.T) {
			t.Parallel()
			// The caller cancels when Sample ends the starts, while the first
			// mutant still sleeps.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := runWith(t, ctx, module(t, map[string]string{addFile: add, addTestFile: sleepingMutant}),
				run.Config{Sample: 1, Verdict: func(m record.Mutant, _ int, _ string) {
					if m.Verdict == spec.NotRun {
						cancel()
					}
				}})
			assert.Equal(t, verdicts(rec), addNotRun, "the cancel ends the mutant that started, and Sample the other")
			assert.NotNil(t, rec.Sample, "a cancelled run states a sample")
			assert.Nil(t, rec.Sample.Limit, "a sample that the cancel ends states no limit")
			assert.True(t, rec.Failed(), "a cancelled run fails")
		})

		t.Run("starts no mutant when the time left is shorter than its deadline and the closing run's",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and less than 10 are
				// left after the opening control run.
				rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowOpening}),
					run.Config{Deadline: time.Now().Add(10 * time.Second)})
				assert.Equal(t, verdicts(rec), addNotRun, "no mutant starts")
				assert.Equal(t, rec.Mutants[0].Reason, tooLateReason, "the record states why no mutant starts")
				assert.NotNil(t, rec.Control.Closing, "the closing control run runs")
				assert.Empty(t, rec.Errors, "the caller's deadline is no run error")
				assert.NotNil(t, rec.Sample, "the record states the empty sample")
				assert.Nil(t, rec.Sample.Score, "an empty sample has no score")
			},
		)

		t.Run("starts a mutant while the time left covers its deadline and the closing run's", func(t *testing.T) {
			t.Parallel()
			// A mutant's deadline is above 7 seconds, and less than 24 are left
			// after the opening control run: twice the deadline, but not twice
			// the deadline and the backup delay.
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowOpening}),
				run.Config{Deadline: time.Now().Add(24 * time.Second)})
			assert.Equal(t, verdicts(rec), addKilled, "both mutants run")
			assert.Empty(t, rec.Errors, "the run states no run error")
		})

		t.Run("stops starting mutants when the time left no longer covers a mutant", func(t *testing.T) {
			t.Parallel()
			// The test binary's deadline is above 7 seconds, and the first
			// mutant runs until that deadline. Less than 20 seconds are left
			// after the opening control run: twice the deadline, but not three
			// times, so the time left falls below twice the deadline while the
			// first mutant runs.
			var mu sync.Mutex
			var order []spec.Verdict
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowSleepingMutant}), run.Config{
				Deadline: time.Now().Add(20 * time.Second),
				Verdict: func(m record.Mutant, _ int, _ string) {
					mu.Lock()
					defer mu.Unlock()
					order = append(order, m.Verdict)
				},
			})
			assert.Equal(t, order, []spec.Verdict{spec.NotRun, spec.TimedOut},
				"the mutant that did not start gets its verdict while the first one runs")
			first, second := rec.Mutants[0], rec.Mutants[1]
			if second.Key < first.Key {
				first, second = second, first
			}
			assert.Equal(t, first.Verdict, spec.TimedOut, "the mutant with the least key runs until its deadline")
			assert.Equal(t, second.Verdict, spec.NotRun, "the other one does not start")
			assert.Equal(t, second.Reason, tooLateReason, "the record states why it does not start")
		})

		t.Run("starts a mutant under Confirm while the time left covers its deadline and the closing run's",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and about 19 seconds are
				// left when the mutants start: twice the deadline, but less than
				// three times the deadline and the ordinary control run's builds.
				rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: slowOpening}),
					run.Config{Confirm: true, Deadline: time.Now().Add(22 * time.Second)})
				assert.Equal(t, verdicts(rec), addKilled, "both mutants run, and no survivor needs a confirmation")
				assert.Empty(t, rec.Errors, "the run states no run error")
			},
		)

		t.Run("gives a survivor not-run when the time left does not cover its confirmation", func(t *testing.T) {
			t.Parallel()
			// The ordinary control run's build waits slowBuild, and about 9
			// seconds are left when the mutants start. That time covers twice
			// a mutant's deadline of about 2 seconds, and not a confirmation,
			// which also needs the time of the builds.
			rec := runIn(t, module(t, survivors), run.Config{
				Env:      slowBuilds(t, ordinaryOutput),
				Confirm:  true,
				Deadline: time.Now().Add(22 * time.Second),
			})
			assert.Equal(t, verdicts(rec), "Sub sbr-zero 0: not-run\nSub aor 0: not-run\n",
				"both mutants run and survive, and neither confirmation starts")
			assert.Equal(t, rec.Mutants[0].Reason, unconfirmedReason, "the record states why the survivor is not-run")
		})

		t.Run("starts no mutant without coverage when the time left does not cover its confirmation",
			func(t *testing.T) {
				t.Parallel()
				// As for the survivors, about 9 seconds are left when the mutants
				// start, and a mutant without coverage runs in its confirmation
				// alone, which needs the ordinary control run's builds too.
				rec := runIn(t, module(t, uncovered), run.Config{
					Env:      slowBuilds(t, ordinaryOutput),
					Confirm:  true,
					Deadline: time.Now().Add(22 * time.Second),
				})
				assert.Equal(t, verdicts(rec), addNotRun, "no mutant starts")
				assert.Equal(t, rec.Mutants[0].Reason, tooLateReason, "the record states why no mutant starts")
			},
		)

		t.Run("stops the runs when the caller cancels", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := runWith(t, ctx, module(t, arithFiles), run.Config{Verdict: func(m record.Mutant, _ int, _ string) {
				if m.Verdict == spec.Killed {
					cancel()
				}
			}})
			assert.Equal(t, verdicts(rec), `Add sbr-zero 0: killed [TestAdd]
Add aor 0: not-run
Sub sbr-zero 0: not-run
Sub aor 0: not-run
Unused sbr-zero 0: no-coverage
Unused aor 0: no-coverage
`, "no mutant starts after the cancel")
			assert.Equal(t, rec.Mutants[1].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Nil(t, rec.Control.Closing, "a cancelled run runs no closing control run")
			// The key of Add's aor, the least key of a mutant that did not run,
			// sorts after the key of Add's sbr-zero and before the keys of
			// Unused's mutants.
			assert.Equal(t, rec.Sample, &record.Sample{Before: rec.Mutants[1].Key, Detected: 1, Score: new(1.0)},
				"the sample counts the mutants before Add's aor")
		})
	})
}
