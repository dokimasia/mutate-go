// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// one is the share of a run of one thread, without threads to spare or to
// borrow.
var one = share{procs: 1}

// earlierStart is the start of the earlier run of a history, before every
// run of a test.
const earlierStart = "2026-01-01T00:00:00Z"

func TestCheck(t *testing.T) {
	t.Parallel()

	t.Run("check", func(t *testing.T) {
		t.Parallel()

		t.Run("returns exitDetected when the tests detect every mutant", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			s := testSession(os.Environ(), &out, io.Discard, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, addFiles)}, one)
			assert.Equal(t, got, exitDetected, "every mutant is killed")
			assert.Equal(t, out.String(), addKilled, "the summary states the run")
		})

		t.Run("returns exitUndetected for a mutant that survived", func(t *testing.T) {
			t.Parallel()
			s := testSession(os.Environ(), io.Discard, io.Discard, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
			assert.Equal(t, got, exitUndetected, "Sub's mutants survive")
		})

		t.Run("tests no mutant of a package that -skip names", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			s := testSession(os.Environ(), &out, io.Discard, "")
			s.skip = []string{fixture}
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
			assert.Equal(t, got, exitDetected, "the survivors of Sub do not run")
			assert.Equal(t, out.String(), "fixture: no mutant to detect: 6 not selected\n",
				"the summary counts every mutant as not selected")
		})

		t.Run("returns exitFailed for a run that fails", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(append(os.Environ(), failVar+"=1"), io.Discard, &errs, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
			assert.Equal(t, got, exitFailed, "the opening control run fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: control-failed: ", "the note states the run error")
		})

		t.Run("returns exitFailed for a run that returns an error", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			got := s.check(
				t.Context(),
				load.Listed{ImportPath: fixture, Dir: filepath.Join(t.TempDir(), "missing")},
				one,
			)
			assert.Equal(t, got, exitFailed, "a run without its directory fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: run: ", "the note states the run's error")
		})

		t.Run("writes the record of the run into the directory of -record", func(t *testing.T) {
			t.Parallel()
			records := t.TempDir()
			s := testSession(os.Environ(), io.Discard, io.Discard, records)
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, addFiles)}, one)
			assert.Equal(t, got, exitDetected, "the run passes")
			_, err := os.Stat(filepath.Join(records, record.FileName(fixture)))
			assert.NoError(t, err, "the record file exists")
		})

		t.Run("reuses the verdicts of the record in the directory of --history", func(t *testing.T) {
			t.Parallel()
			dir, history := module(t, addFiles), t.TempDir()
			pkg := load.Listed{ImportPath: fixture, Dir: dir}
			s := testSession(os.Environ(), io.Discard, io.Discard, "")
			s.history, s.cfg.Sample = history, 1
			assert.Equal(t, s.check(t.Context(), pkg, one), exitDetected, "the first run tests one mutant")
			// The earlier run started a day before, so its verdict is older than
			// any verdict of the next run.
			earlier, err := record.Read(history, fixture, &definition.Protocol, run.Engine)
			assert.NoError(t, err, "the first run writes its record into the history")
			earlier.StartedAt = earlierStart
			earlier.Observations[0].StartedAt = earlierStart
			for i := range earlier.Mutants {
				if earlier.Mutants[i].ObservedAt != "" {
					earlier.Mutants[i].ObservedAt = earlierStart
				}
			}
			_, err = earlier.Write(history)
			assert.NoError(t, err, "the earlier record writes")
			var out bytes.Buffer
			s.out.stdout = &out
			assert.Equal(t, s.check(t.Context(), pkg, one), exitDetected, "the second run tests the other mutant")
			assert.Equal(t, out.String(), "fixture: 2 of 2 mutants detected (100%): 2 killed, 1 reused\n",
				"the summary counts the reused verdict")
		})

		t.Run("notes a record of the history that does not count", func(t *testing.T) {
			t.Parallel()
			history := t.TempDir()
			foreign := &record.Record{
				Record:  definition.Protocol.Record.Name,
				Version: definition.Protocol.Record.Version,
				Engine:  record.Engine{Name: "mutate-other"},
				Target:  record.Target{Name: fixture},
			}
			_, err := foreign.Write(history)
			assert.NoError(t, err, "the record of another engine writes")
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			s.history = history
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, addFiles)}, one)
			assert.Equal(t, got, exitDetected, "the run tests every mutant")
			assert.HasPrefix(t, errs.String(), name+": fixture: the history does not count: "+record.ErrForeign.Error(),
				"the note states why the run reuses no verdict")
			rec, err := record.Read(history, fixture, &definition.Protocol, run.Engine)
			assert.NoError(t, err, "the run's record replaces the record of the other engine")
			assert.Equal(t, rec.Engine.Name, run.Engine, "the history's record is the engine's")
		})

		histories := []struct {
			name string
			// env and skip are the environment of the runs and the packages of
			// --skip.
			env  []string
			skip []string
		}{
			{"writes no record into the history for a run with a run error", append(os.Environ(), failVar+"=1"), nil},
			{"writes no record into the history for a package that --skip names", os.Environ(), []string{fixture}},
		}
		for _, tt := range histories {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				history := t.TempDir()
				s := testSession(tt.env, io.Discard, io.Discard, "")
				s.history, s.skip = history, tt.skip
				s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
				files.Absent(t, filepath.Join(history, record.FileName(fixture)), "the history has no record")
			})
		}

		t.Run("returns exitFailed for a record that does not write", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addFiles)
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, filepath.Join(dir, addFile))
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: dir}, one)
			assert.Equal(t, got, exitFailed, "a record under a file does not write")
			assert.HasPrefix(t, errs.String(), name+": record: ", "the note states the record's error")
		})
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the listing and returns exitDetected", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			s := testSession(append(os.Environ(), failVar+"=1"), &out, io.Discard, "")
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
			assert.Equal(t, got, exitDetected, "a listing runs no test, so failing tests do not fail it")
			assert.HasSuffix(t, out.String(), "fixture: 6 mutants to test\n", "the listing counts the mutants")
		})

		t.Run("lists every mutant of a package that -skip names as not selected", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			s := testSession(os.Environ(), &out, io.Discard, "")
			s.skip = []string{fixture}
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, one)
			assert.Equal(t, got, exitDetected, "the listing passes")
			assert.HasSuffix(t, out.String(), "fixture: 0 mutants to test, 6 not selected\n",
				"the listing counts every mutant as not selected")
		})

		t.Run("returns exitFailed for an instrumentation that does not type-check", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			// The file hides nil, which the instrumentation's helper file uses.
			dir := module(t, with(addFiles, map[string]string{"hiding.go": "package fixture\n\nvar nil = 0\n"}))
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: dir}, one)
			assert.Equal(t, got, exitFailed, "the listing fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: build: ", "the note states the run error")
		})

		t.Run("returns exitFailed for a listing that returns an error", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			got := s.list(
				t.Context(),
				load.Listed{ImportPath: fixture, Dir: filepath.Join(t.TempDir(), "missing")},
				one,
			)
			assert.Equal(t, got, exitFailed, "a listing without its directory fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: run: ", "the note states the error")
		})
	})

	t.Run("exitStatus", func(t *testing.T) {
		t.Parallel()
		limit := 1
		tests := []struct {
			name string
			give *record.Record
			want int
		}{
			{
				name: "returns exitDetected for a sample whose mutants the tests detect",
				give: &record.Record{
					Mutants: []record.Mutant{
						{Key: "a", Verdict: spec.Killed},
						{Key: "b", Verdict: spec.NotRun},
						{Key: "c", Verdict: spec.NoCoverage},
					},
					Sample: &record.Sample{Before: new("b"), Limit: &limit, Detected: 1},
				},
				want: exitDetected,
			},
			{
				name: "returns exitUndetected for a sample with a mutant that no test covers",
				give: &record.Record{
					Mutants: []record.Mutant{{Key: "a", Verdict: spec.NoCoverage}, {Key: "b", Verdict: spec.NotRun}},
					Sample:  &record.Sample{Before: new("b"), Limit: &limit, Undetected: 1},
				},
				want: exitUndetected,
			},
			{
				name: "returns exitUndetected for a mutant that no test covers in a run without a sample",
				give: &record.Record{
					Mutants: []record.Mutant{{Key: "a", Verdict: spec.Killed}, {Key: "b", Verdict: spec.NoCoverage}},
				},
				want: exitUndetected,
			},
			{
				name: "returns exitFailed for a sample that the deadline ended",
				give: &record.Record{
					Mutants: []record.Mutant{{Key: "a", Verdict: spec.Killed}, {Key: "b", Verdict: spec.NotRun}},
					Sample:  &record.Sample{Before: new("b"), Detected: 1},
				},
				want: exitFailed,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, exitStatus(tt.give), tt.want, "the status follows the counted mutants")
			})
		}
	})
}
