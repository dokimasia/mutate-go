// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
)

// testSession returns a session whose runs have the environment env and
// write to stdout and stderr, with the records in records.
func testSession(env []string, stdout, stderr io.Writer, records string) *session {
	return &session{
		cfg:      run.Config{Env: env, Workers: 1},
		out:      &output{stdout: stdout, stderr: stderr},
		progress: &progress{stderr: io.Discard, every: time.Hour, now: time.Now},
		records:  records,
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	t.Run("check", func(t *testing.T) {
		t.Parallel()

		t.Run("returns exitDetected when the tests detect every mutant", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			s := testSession(os.Environ(), &out, io.Discard, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, addFiles)}, 1)
			assert.Equal(t, got, exitDetected, "every mutant is killed")
			assert.Equal(t, out.String(), addKilled, "the summary states the run")
		})

		t.Run("returns exitUndetected for a mutant that survived", func(t *testing.T) {
			t.Parallel()
			s := testSession(os.Environ(), io.Discard, io.Discard, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, 1)
			assert.Equal(t, got, exitUndetected, "Sub's mutants survive")
		})

		t.Run("returns exitFailed for a run that fails", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(append(os.Environ(), failVar+"=1"), io.Discard, &errs, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, 1)
			assert.Equal(t, got, exitFailed, "the opening control run fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: control-failed: ", "the note states the run error")
		})

		t.Run("returns exitFailed for a run that returns an error", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: filepath.Join(t.TempDir(), "missing")}, 1)
			assert.Equal(t, got, exitFailed, "a run without its directory fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: run: ", "the note states the run's error")
		})

		t.Run("writes the record of the run into the directory of -record", func(t *testing.T) {
			t.Parallel()
			records := t.TempDir()
			s := testSession(os.Environ(), io.Discard, io.Discard, records)
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, addFiles)}, 1)
			assert.Equal(t, got, exitDetected, "the run passes")
			_, err := os.Stat(filepath.Join(records, record.FileName(fixture)))
			assert.NoError(t, err, "the record file exists")
		})

		t.Run("returns exitFailed for a record that does not write", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addFiles)
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, filepath.Join(dir, addFile))
			got := s.check(t.Context(), load.Listed{ImportPath: fixture, Dir: dir}, 1)
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
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: module(t, arithFiles)}, 1)
			assert.Equal(t, got, exitDetected, "a listing runs no test, so failing tests do not fail it")
			assert.HasSuffix(t, out.String(), "fixture: 6 mutants to test\n", "the listing counts the mutants")
		})

		t.Run("returns exitFailed for an instrumentation that does not type-check", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			dir := module(
				t,
				with(addFiles, map[string]string{"names.go": "package fixture\n\nvar _mutateActive = 0\n"}),
			)
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: dir}, 1)
			assert.Equal(t, got, exitFailed, "the listing fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: build: ", "the note states the run error")
		})

		t.Run("returns exitFailed for a listing that returns an error", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			s := testSession(os.Environ(), io.Discard, &errs, "")
			got := s.list(t.Context(), load.Listed{ImportPath: fixture, Dir: filepath.Join(t.TempDir(), "missing")}, 1)
			assert.Equal(t, got, exitFailed, "a listing without its directory fails")
			assert.HasPrefix(t, errs.String(), name+": fixture: run: ", "the note states the error")
		})
	})
}
