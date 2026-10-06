// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package report_test

import (
	"regexp"
	"strconv"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/report"
	"go.dokimi.dev/mutate/internal/spec"
)

// protocol is the run protocol of the vendored definition, whose verdicts a
// summary counts in order.
var protocol = spec.Load().Protocol

// target is the import path of the records of the cases.
const target = "example.com/wire"

// path is the path of the file of the mutants of the cases of Line.
const path = "f.go"

// mutant is the mutant whose line the cases of Line write.
var mutant = record.Mutant{
	Kind:        spec.RORBoundary,
	Start:       record.Position{Line: 7, Column: 5},
	Original:    "a < b",
	Replacement: "a <= b",
}

// detection matches the head of a summary that states the detected
// mutants: the detected ones, the counted ones and the percentage.
var detection = regexp.MustCompile(`^` + regexp.QuoteMeta(target) + `: (\d+) of (\d+) mutants? detected \((\d+)%\)`)

// run returns a record of target whose mutants have the verdicts.
func run(verdicts ...spec.Verdict) *record.Record {
	rec := &record.Record{Target: record.Target{Name: target}}
	for _, v := range verdicts {
		rec.Mutants = append(rec.Mutants, record.Mutant{Verdict: v})
	}
	return rec
}

func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("Listed", func(t *testing.T) {
		t.Parallel()

		t.Run("reports true for a survivor, a mutant without coverage and a mutant in error", func(t *testing.T) {
			t.Parallel()
			var listed []spec.Verdict
			for _, v := range protocol.Verdicts {
				if report.Listed(v.ID) {
					listed = append(listed, v.ID)
				}
			}
			assert.Permutation(t, listed, []spec.Verdict{spec.Survived, spec.NoCoverage, spec.Error},
				"a report lists the mutants that a reader acts on")
		})
	})

	t.Run("Line", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name                string
			verdict             spec.Verdict
			replacement, reason string
			want                string
		}{
			{
				"writes the position, the verdict, the change and the kind of a survivor",
				spec.Survived, "a <= b", "",
				"f.go:7:5: survived: a < b became a <= b (ror-boundary)",
			},
			{
				"writes a mutant without coverage as not covered",
				spec.NoCoverage, "a <= b", "",
				"f.go:7:5: not covered: a < b became a <= b (ror-boundary)",
			},
			{
				"writes a verdict's hyphens as spaces",
				spec.TimedOut, "a <= b", "",
				"f.go:7:5: timed out: a < b became a <= b (ror-boundary)",
			},
			{
				"writes an empty replacement as the removal of the original",
				spec.Survived, "", "",
				"f.go:7:5: survived: a < b removed (ror-boundary)",
			},
			{
				"ends the line of a mutant in error with the reason",
				spec.Error, "a <= b", "the run did not start",
				"f.go:7:5: error: a < b became a <= b (ror-boundary): the run did not start",
			},
			{
				"leaves out the reason of a suppressed mutant",
				spec.Suppressed, "a <= b", "an equal value",
				"f.go:7:5: suppressed: a < b became a <= b (ror-boundary)",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				m := mutant
				m.Verdict, m.Replacement, m.Reason = tt.verdict, tt.replacement, tt.reason
				assert.Equal(t, report.Line(m, path), tt.want, "the line states the mutant at its position")
			})
		}
	})

	t.Run("Planned", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name              string
			verdict           spec.Verdict
			rule              spec.Family
			replacement, want string
		}{
			{
				"writes a mutant to test with its position, its change and its kind",
				"", "", "a <= b",
				"f.go:7:5: to test: a < b becomes a <= b (ror-boundary)",
			},
			{
				"writes a suppressed mutant with its rule family",
				spec.Suppressed, "logging", "a <= b",
				"f.go:7:5: suppressed (logging): a < b becomes a <= b (ror-boundary)",
			},
			{
				"writes a mutant that is not viable with its verdict",
				spec.NotViable, "", "a <= b",
				"f.go:7:5: not viable: a < b becomes a <= b (ror-boundary)",
			},
			{
				"writes an empty replacement as the removal of the original",
				"", "", "",
				"f.go:7:5: to test: a < b removed (ror-boundary)",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				m := mutant
				m.Verdict, m.Rule, m.Replacement = tt.verdict, tt.rule, tt.replacement
				assert.Equal(t, report.Planned(m, path), tt.want, "the line lists the mutant at its position")
			})
		}
	})

	t.Run("Plan", func(t *testing.T) {
		t.Parallel()

		t.Run("counts the mutants to test and the mutants of each verdict", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, report.Plan(run("", "", spec.Suppressed, spec.NotViable), protocol),
				target+": 2 mutants to test, 1 not viable, 1 suppressed", "the line counts every mutant")
		})

		t.Run("states one mutant to test in the singular", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, report.Plan(run(""), protocol), target+": 1 mutant to test", "the line counts the mutant")
		})
	})

	t.Run("Notes", func(t *testing.T) {
		t.Parallel()
		notRun := func(reason string) record.Mutant { return record.Mutant{Verdict: spec.NotRun, Reason: reason} }
		tests := []struct {
			name    string
			errors  []record.RunError
			mutants []record.Mutant
			want    []string
		}{
			{
				"returns nil for a run without a run error and without a mutant that did not run",
				nil,
				[]record.Mutant{{Verdict: spec.Killed}},
				nil,
			},
			{
				"states each run error by its code and its message",
				[]record.RunError{{Code: spec.ErrorBuild, Message: "x"}, {Code: spec.ErrorChangedFiles, Message: "y"}},
				nil,
				[]string{"build: x", "changed-files: y"},
			},
			{
				"states the number of the mutants that did not run and their reason",
				nil,
				[]record.Mutant{notRun("the caller cancelled the run"), notRun("the caller cancelled the run")},
				[]string{"2 mutants did not run: the caller cancelled the run"},
			},
			{
				"states one mutant that did not run in the singular",
				nil,
				[]record.Mutant{{Verdict: spec.Killed}, notRun("the caller cancelled the run")},
				[]string{"1 mutant did not run: the caller cancelled the run"},
			},
			{
				"states each reason on a line of its own in the order of its first mutant",
				nil,
				[]record.Mutant{notRun("b"), notRun("a"), notRun("b")},
				[]string{"2 mutants did not run: b", "1 mutant did not run: a"},
			},
			{
				"states the run errors before the mutants that did not run",
				[]record.RunError{{Code: spec.ErrorControl, Message: "x"}},
				[]record.Mutant{notRun("the opening control run failed")},
				[]string{"control-failed: x", "1 mutant did not run: the opening control run failed"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec := &record.Record{Errors: tt.errors, Mutants: tt.mutants}
				assert.Equal(t, report.Notes(rec), tt.want, "the notes state what the summary leaves out")
			})
		}
	})

	t.Run("Summary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			give   *record.Record
			errors []record.RunError
			sample *record.Sample
			want   string
		}{
			{
				"states the detected mutants of those that the score counts and each verdict's count",
				run(spec.Killed, spec.Survived), nil, nil,
				"1 of 2 mutants detected (50%): 1 killed, 1 survived",
			},
			{
				"states one counted mutant in the singular",
				run(spec.Killed, spec.Suppressed), nil, nil,
				"1 of 1 mutant detected (100%): 1 killed, 1 suppressed",
			},
			{
				"writes each verdict as a phrase in the protocol's order",
				run(spec.NotSelected, spec.TimedOut, spec.NoCoverage, spec.NotViable, spec.Killed), nil, nil,
				"2 of 3 mutants detected (66%): 1 killed, 1 timed out, 1 not covered, 1 not viable, 1 not selected",
			},
			{
				"states a run that fails in place of the detected mutants",
				run(spec.Killed, spec.NotRun), nil, nil,
				"the run failed: 1 killed, 1 not run",
			},
			{
				"states the detected mutants of the sample of a run that fails",
				run(spec.Killed, spec.Survived, spec.NotRun), nil,
				&record.Sample{Before: "2", Detected: 1, Undetected: 1, Score: new(0.5)},
				"the run failed, 1 of a sample of 2 mutants detected (50%): 1 killed, 1 survived, 1 not run",
			},
			{
				"states the detected mutants of the sample of a run that the caller's limit ended",
				run(spec.Killed, spec.Survived, spec.NotRun), nil,
				&record.Sample{Before: "2", Limit: new(2), Detected: 1, Undetected: 1, Score: new(0.5)},
				"1 of a sample of 2 mutants detected (50%): 1 killed, 1 survived, 1 not run",
			},
			{
				"states no mutant to detect for a sample of the caller's limit that counts none",
				run(spec.NotViable, spec.NoCoverage, spec.NotRun), nil,
				&record.Sample{Before: "1", Limit: new(1)},
				"no mutant to detect: 1 not covered, 1 not viable, 1 not run",
			},
			{
				"states a run that fails with an empty sample as a failed run",
				run(spec.Suppressed, spec.NotRun), nil,
				&record.Sample{Before: "1"},
				"the run failed: 1 suppressed, 1 not run",
			},
			{
				"states a run error as a failed run",
				run(),
				[]record.RunError{{Code: spec.ErrorLoad, Message: "x"}},
				nil,
				"the run failed",
			},
			{
				"states a run whose score counts no mutant",
				run(spec.Suppressed), nil, nil,
				"no mutant to detect: 1 suppressed",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				tt.give.Errors, tt.give.Sample = tt.errors, tt.sample
				assert.Equal(t, report.Summary(tt.give, protocol), target+": "+tt.want,
					"the summary states the run's outcome and its verdicts")
			})
		}

		generated := []struct {
			name string
			give []record.Generated
			want string
		}{
			{
				"states one generated file with one mutant in the singular",
				[]record.Generated{{File: "a_gen.go", Mutants: 1}},
				"1 generated file with 1 mutant left out",
			},
			{
				"states the generated files that the run leaves out with their mutants",
				[]record.Generated{{File: "a_gen.go", Mutants: 2}, {File: "b_gen.go", Mutants: 3}},
				"2 generated files with 5 mutants left out",
			},
			{
				"states the generated files that the run includes with their mutants",
				[]record.Generated{
					{File: "a_gen.go", Mutants: 2, Included: true},
					{File: "b_gen.go", Mutants: 3, Included: true},
				},
				"2 generated files with 5 mutants included",
			},
		}
		for _, tt := range generated {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec := run(spec.Killed)
				rec.Generated = tt.give
				assert.Equal(t, report.Summary(rec, protocol),
					target+": 1 of 1 mutant detected (100%): 1 killed, "+tt.want,
					"the summary ends with the generated files")
			})
		}

		t.Run("states the percentage of the detected mutants rounded down", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "a summary's percentage p of d detected among n mutants has p·n ≤ 100·d < (p+1)·n",
				func(c *prop.Case) {
					detected := c.Draw(prop.Integer(0, 300), "detected")
					undetected := c.Draw(prop.Integer(0, 300), "undetected")
					c.Assume(detected+undetected > 0)
					var verdicts []spec.Verdict
					for range detected {
						verdicts = append(verdicts, spec.Killed)
					}
					for range undetected {
						verdicts = append(verdicts, spec.Survived)
					}
					head := detection.FindStringSubmatch(report.Summary(run(verdicts...), protocol))
					assert.Length(c, head, 4, "the summary states the detected mutants")
					d, _ := strconv.Atoi(head[1])
					n, _ := strconv.Atoi(head[2])
					p, _ := strconv.Atoi(head[3])
					assert.Equal(c, [2]int{d, n}, [2]int{detected, detected + undetected},
						"the summary counts the detected and the counted mutants")
					assert.InRange(c, 100*d-p*n, 0, float64(n-1), "the percentage is rounded down")
				})
		})
	})
}
