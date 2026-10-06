// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// mutantLine is the mutant whose line the cases of line write.
var mutantLine = record.Mutant{
	Kind: spec.RORBoundary, File: "a.go", Start: record.Position{Line: 7, Column: 5},
	Original: "a < b", Replacement: "a <= b",
}

// writers is the number of goroutines that write through one lockedWriter.
const writers = 100

func TestOutput(t *testing.T) {
	t.Parallel()

	t.Run("line", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			verdict spec.Verdict
			json    bool
			want    string
		}{
			{
				"writes the line of a survivor", spec.Survived, false,
				"a.go:7:5: survived: a < b became a <= b (ror-boundary)\n",
			},
			{
				"writes the line of a mutant without coverage", spec.NoCoverage, false,
				"a.go:7:5: not covered: a < b became a <= b (ror-boundary)\n",
			},
			{"writes no line of a killed mutant", spec.Killed, false, ""},
			{"writes no line in place of the records", spec.Survived, true, ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				o := &output{stdout: &out, stderr: io.Discard, json: tt.json}
				m := mutantLine
				m.Verdict = tt.verdict
				o.line(m, m.File)
				assert.Equal(t, out.String(), tt.want, "the output lists the mutants that a reader acts on")
			})
		}
	})

	t.Run("result", func(t *testing.T) {
		t.Parallel()
		failed := &record.Record{
			Target:  record.Target{Name: fixture},
			Errors:  []record.RunError{{Code: spec.ErrorControl, Message: "TestAdd failed"}},
			Mutants: []record.Mutant{{Verdict: spec.NotRun, Reason: "the opening control run failed"}},
		}

		t.Run("writes the notes to stderr and the summary to stdout", func(t *testing.T) {
			t.Parallel()
			var out, errs bytes.Buffer
			(&output{stdout: &out, stderr: &errs}).result(failed)
			assert.Equal(t, errs.String(), name+": fixture: control-failed: TestAdd failed\n"+
				name+": fixture: 1 mutant did not run: the opening control run failed\n",
				"the notes follow the command's name and the package")
			assert.Equal(t, out.String(), "fixture: the run failed: 1 not run\n", "the summary states the run")
		})

		t.Run("writes the record as one line of JSON in place of the summary", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			(&output{stdout: &out, stderr: io.Discard, json: true}).result(failed)
			assert.Equal(t, strings.Count(out.String(), "\n"), 1, "the record is one line")
			var got record.Record
			assert.NoError(t, json.Unmarshal(out.Bytes(), &got), "the line is JSON")
			assert.Equal(t, &got, failed, "the line is the record", assert.EquateEmpty())
		})
	})

	t.Run("listing", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the line of each mutant and the count of the mutants", func(t *testing.T) {
			t.Parallel()
			wd, err := os.Getwd()
			assert.NoError(t, err, "the working directory reads")
			suppressed := mutantLine
			suppressed.Verdict, suppressed.Rule = spec.Suppressed, "logging"
			rec := &record.Record{
				Target: record.Target{Name: fixture}, Root: wd,
				Mutants: []record.Mutant{mutantLine, suppressed},
			}
			var out bytes.Buffer
			(&output{stdout: &out, stderr: io.Discard}).listing(rec)
			assert.Equal(t, out.String(), "a.go:7:5: to test: a < b becomes a <= b (ror-boundary)\n"+
				"a.go:7:5: suppressed (logging): a < b becomes a <= b (ror-boundary)\n"+
				"fixture: 1 mutant to test, 1 suppressed\n", "the listing states each mutant and the count")
		})
	})

	t.Run("errorf", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the message after the command's name", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			(&output{stdout: io.Discard, stderr: &errs}).errorf("%s: %d", "x", 1)
			assert.Equal(t, errs.String(), name+": x: 1\n", "the message is one line")
		})
	})

	t.Run("lockedWriter", func(t *testing.T) {
		t.Parallel()

		t.Run("Write", func(t *testing.T) {
			t.Parallel()

			t.Run("writes the data of each goroutine whole", func(t *testing.T) {
				t.Parallel()
				var buf bytes.Buffer
				w := &lockedWriter{w: &buf}
				var wg sync.WaitGroup
				for range writers {
					wg.Go(func() { _, _ = w.Write([]byte("one line\n")) })
				}
				wg.Wait()
				assert.Equal(t, buf.String(), strings.Repeat("one line\n", writers), "no line interleaves")
			})
		})
	})
}
