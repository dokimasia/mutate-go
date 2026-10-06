// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/load"
)

// overlap is how long each package of the case of the concurrency holds
// its slot, so the packages that may run at once do.
const overlap = 20 * time.Millisecond

// listed returns packages of the import paths, in a directory of none.
func listed(paths ...string) []load.Listed {
	var pkgs []load.Listed
	for _, p := range paths {
		pkgs = append(pkgs, load.Listed{ImportPath: p})
	}
	return pkgs
}

func TestPlan(t *testing.T) {
	t.Parallel()

	t.Run("schedule", func(t *testing.T) {
		t.Parallel()
		out := &output{stdout: io.Discard, stderr: io.Discard}

		t.Run("returns the highest status of the packages", func(t *testing.T) {
			t.Parallel()
			statuses := map[string]int{"a": exitDetected, "b": exitFailed, "c": exitUndetected}
			got := schedule(t.Context(), listed("a", "b", "c"), 2, time.Time{}, out,
				func(_ context.Context, pkg load.Listed, _ int) int { return statuses[pkg.ImportPath] })
			assert.Equal(t, got, exitFailed, "the failed run decides the status")
		})

		t.Run("divides GOMAXPROCS among the packages that run at once", func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			procs := map[string]int{}
			schedule(t.Context(), listed("a", "b", "c"), 2, time.Time{}, out,
				func(_ context.Context, pkg load.Listed, n int) int {
					mu.Lock()
					defer mu.Unlock()
					procs[pkg.ImportPath] = n
					return exitDetected
				})
			threads := runtime.GOMAXPROCS(0)
			assert.Equal(t, procs, map[string]int{
				"a": max(1, threads/2),
				"b": max(1, threads/2),
				"c": threads,
			}, "the last package to start shares the threads with no package left to start")
		})

		t.Run("runs at most the given number of packages at once", func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			running, most := 0, 0
			schedule(t.Context(), listed("a", "b", "c", "d", "e"), 2, time.Time{}, out,
				func(context.Context, load.Listed, int) int {
					mu.Lock()
					running++
					most = max(most, running)
					mu.Unlock()
					time.Sleep(overlap)
					mu.Lock()
					running--
					mu.Unlock()
					return exitDetected
				})
			assert.InRange(t, most, 1, 2, "two slots run two packages at once at most")
		})

		tests := []struct {
			name     string
			ctx      func() context.Context
			deadline time.Time
			want     string
		}{
			{
				"starts no package after the deadline",
				context.Background,
				time.Now().Add(-time.Second),
				name + ": a: not tested, because -timeout passed before the package started\n",
			},
			{
				"starts no package after the context ends",
				func() context.Context {
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					return ctx
				},
				time.Time{},
				name + ": a: not tested, because the command was interrupted\n",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var errs bytes.Buffer
				called := false
				got := schedule(tt.ctx(), listed("a"), 1, tt.deadline, &output{stdout: io.Discard, stderr: &errs},
					func(context.Context, load.Listed, int) int {
						called = true
						return exitDetected
					})
				assert.False(t, called, "the package does not start")
				assert.Equal(t, got, exitFailed, "a package that does not start fails the command")
				assert.Equal(t, errs.String(), tt.want, "the note states why the package did not start")
			})
		}
	})
}

// TestPlanEnv changes the working directory and the environment of the
// test process, so neither it nor its subtests run in parallel.
func TestPlanEnv(t *testing.T) {
	t.Run("packages", func(t *testing.T) {
		t.Run("returns the packages that the patterns name", func(t *testing.T) {
			dir := enter(t, twoPackages)
			var errs bytes.Buffer
			pkgs, ok := packages(t.Context(), []string{"./..."}, &output{stdout: io.Discard, stderr: &errs})
			assert.True(t, ok, "every pattern resolves")
			resolved, err := filepath.EvalSymlinks(dir)
			assert.NoError(t, err, "the module's directory resolves")
			assert.Equal(t, pkgs, []load.Listed{
				{ImportPath: fixture, Dir: resolved},
				{ImportPath: fixture + "/sub", Dir: filepath.Join(resolved, "sub")},
			}, "go list states each package with its directory")
			assert.Empty(t, errs.String(), "the packages state no error")
		})

		t.Run("writes the error of a pattern that names no package and reports false", func(t *testing.T) {
			enter(t, addFiles)
			var errs bytes.Buffer
			pkgs, ok := packages(t.Context(), []string{".", "./missing"}, &output{stdout: io.Discard, stderr: &errs})
			assert.False(t, ok, "a pattern does not resolve")
			assert.Length(t, pkgs, 1, "the package that resolves remains")
			assert.HasPrefix(t, errs.String(), name+": ./missing: ", "the error names the pattern")
		})

		t.Run("writes the error of a go list that fails and reports false", func(t *testing.T) {
			t.Setenv(pathVar, fakeGoDir+string(filepath.ListSeparator)+os.Getenv(pathVar))
			t.Setenv(fakeVar, fakeFail)
			var errs bytes.Buffer
			_, ok := packages(t.Context(), []string{"."}, &output{stdout: io.Discard, stderr: &errs})
			assert.False(t, ok, "go list fails")
			assert.Contains(t, errs.String(), "no list today", "the error states the go command's message")
		})
	})

	t.Run("suite", func(t *testing.T) {
		t.Run("returns no import path for no pattern", func(t *testing.T) {
			paths, ok := suite(t.Context(), nil, &output{stdout: io.Discard, stderr: io.Discard})
			assert.True(t, ok, "no pattern resolves trivially")
			assert.Nil(t, paths, "and names no package")
		})

		t.Run("returns the import paths of the packages that the patterns name", func(t *testing.T) {
			enter(t, twoPackages)
			paths, ok := suite(t.Context(), []string{"./sub"}, &output{stdout: io.Discard, stderr: io.Discard})
			assert.True(t, ok, "the pattern resolves")
			assert.Equal(t, paths, []string{fixture + "/sub"}, "the suite names the package by its import path")
		})

		t.Run("writes the error of a package whose dependency does not resolve", func(t *testing.T) {
			enter(
				t,
				with(addFiles, map[string]string{"check/check.go": "package check\n\nimport _ \"fixture/missing\"\n"}),
			)
			var errs bytes.Buffer
			_, ok := suite(t.Context(), []string{"./check"}, &output{stdout: io.Discard, stderr: &errs})
			assert.False(t, ok, "the package does not load")
			assert.HasPrefix(t, errs.String(), name+": -suite: fixture/check: package fixture/missing is not in ",
				"the error names the package and its dependency")
		})

		t.Run("writes the error of a go list that fails", func(t *testing.T) {
			t.Setenv(pathVar, fakeGoDir+string(filepath.ListSeparator)+os.Getenv(pathVar))
			t.Setenv(fakeVar, fakeFail)
			var errs bytes.Buffer
			_, ok := suite(t.Context(), []string{"./check"}, &output{stdout: io.Discard, stderr: &errs})
			assert.False(t, ok, "go list fails")
			assert.HasPrefix(t, errs.String(), name+": -suite: go list ", "the error names the flag")
		})
	})
}
