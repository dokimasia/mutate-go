// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/run"
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

// shares records the share of each package that a test function of
// schedule receives, by import path. It is safe for concurrent use.
type shares struct {
	mu     sync.Mutex
	procs  map[string]int
	starts []string
}

// add records the share procs of the package path, and that it started.
func (s *shares) add(path string, procs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procs == nil {
		s.procs = map[string]int{}
	}
	s.procs[path] = procs
	s.starts = append(s.starts, path)
}

// got returns a copy of the shares that the packages received.
func (s *shares) got() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.procs)
}

func TestPlan(t *testing.T) {
	t.Parallel()

	t.Run("weigh", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the number of mutants that a run tests in each package", func(t *testing.T) {
			t.Parallel()
			pkgs := []load.Listed{
				{ImportPath: fixture, Dir: module(t, addFiles)},
				{ImportPath: fixture, Dir: module(t, arithFiles)},
			}
			got := weigh(t.Context(), pkgs, run.Config{Env: os.Environ()}, 4, 2)
			assert.Equal(t, got, []int{2, 6}, "add has 2 mutants to test and arith 6")
		})
	})

	t.Run("schedule", func(t *testing.T) {
		t.Parallel()
		out := &output{stdout: io.Discard, stderr: io.Discard}

		t.Run("returns the highest status of the packages", func(t *testing.T) {
			t.Parallel()
			statuses := map[string]int{"a": exitDetected, "b": exitFailed, "c": exitUndetected}
			got := schedule(t.Context(), listed("a", "b", "c"), nil, 4, 2, time.Time{}, out,
				func(_ context.Context, pkg load.Listed, _ int, _ func() int) int { return statuses[pkg.ImportPath] })
			assert.Equal(t, got, exitFailed, "the failed run decides the status")
		})

		t.Run("starts the packages in the order of their weights", func(t *testing.T) {
			t.Parallel()
			var s shares
			schedule(t.Context(), listed("a", "b", "c", "d"), []int{1, 3, 2, 3}, 4, 1, time.Time{}, out,
				func(_ context.Context, pkg load.Listed, procs int, _ func() int) int {
					s.add(pkg.ImportPath, procs)
					return exitDetected
				})
			assert.Equal(t, s.starts, []string{"b", "d", "c", "a"},
				"the heaviest package starts first, and equal weights keep the order of the packages")
		})

		tests := []struct {
			name    string
			weights []int
			threads int
			want    map[string]int
		}{
			{
				name:    "gives a package a share in proportion to its weight against the packages that may start beside it",
				weights: []int{3, 1},
				threads: 8,
				want:    map[string]int{"a": 6, "b": 2},
			},
			{
				name:    "gives equal shares where the packages that may start weigh nothing",
				weights: []int{0, 0},
				threads: 4,
				want:    map[string]int{"a": 2, "b": 2},
			},
			{
				name:    "rounds the share of a package",
				threads: 5,
				want:    map[string]int{"a": 3, "b": 2},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var s shares
				// a ends only after b started, so both run at once.
				started := make(chan struct{})
				schedule(t.Context(), listed("a", "b"), tt.weights, tt.threads, 2, time.Time{}, out,
					func(_ context.Context, pkg load.Listed, procs int, _ func() int) int {
						s.add(pkg.ImportPath, procs)
						if pkg.ImportPath == "a" {
							<-started
						} else {
							close(started)
						}
						return exitDetected
					})
				assert.Equal(t, s.got(), tt.want, "each package gets its share of the threads")
			})
		}

		t.Run("gives each package the threads that the running packages leave free", func(t *testing.T) {
			t.Parallel()
			var s shares
			started := map[string]chan struct{}{
				"a": make(chan struct{}),
				"b": make(chan struct{}),
				"c": make(chan struct{}),
			}
			// Each package ends only after the next one started, so a and b
			// run at once, and c starts beside b after a ended.
			next := map[string]string{"a": "b", "b": "c"}
			schedule(t.Context(), listed("a", "b", "c"), nil, 5, 2, time.Time{}, out,
				func(_ context.Context, pkg load.Listed, procs int, _ func() int) int {
					s.add(pkg.ImportPath, procs)
					close(started[pkg.ImportPath])
					if after, ok := next[pkg.ImportPath]; ok {
						<-started[after]
					}
					return exitDetected
				})
			assert.Equal(t, s.got(), map[string]int{"a": 3, "b": 2, "c": 3},
				"a package shares the threads that the running packages leave free among the slots it may fill")
		})

		t.Run("starts a package only when a thread is free", func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				var s shares
				release := map[string]chan struct{}{
					"a": make(chan struct{}),
					"b": make(chan struct{}),
					"c": make(chan struct{}),
				}
				done := make(chan int)
				go func() {
					done <- schedule(t.Context(), listed("a", "b", "c"), []int{6, 1, 1}, 4, 3, time.Time{}, out,
						func(_ context.Context, pkg load.Listed, procs int, _ func() int) int {
							s.add(pkg.ImportPath, procs)
							<-release[pkg.ImportPath]
							return exitDetected
						})
				}()
				synctest.Wait()
				assert.Equal(t, s.got(), map[string]int{"a": 3, "b": 1},
					"a and b take every thread, so c waits although a slot is free")
				close(release["a"])
				synctest.Wait()
				assert.Equal(t, s.got()["c"], 3, "c starts with the threads that a returned")
				close(release["b"])
				close(release["c"])
				<-done
			})
		})

		t.Run("gives each package one thread where the threads are fewer than the packages", func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				var s shares
				release := make(chan struct{})
				done := make(chan int)
				go func() {
					done <- schedule(t.Context(), listed("a", "b", "c"), nil, 2, 3, time.Time{}, out,
						func(_ context.Context, pkg load.Listed, procs int, _ func() int) int {
							s.add(pkg.ImportPath, procs)
							<-release
							return exitDetected
						})
				}()
				synctest.Wait()
				assert.Equal(t, s.got(), map[string]int{"a": 1, "b": 1, "c": 1},
					"every package runs at once on one thread")
				close(release)
				<-done
			})
		})

		t.Run("passes the threads that the running packages leave free to a package", func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				release, proceed := make(chan struct{}), make(chan struct{})
				var before, after int
				done := make(chan int)
				go func() {
					done <- schedule(t.Context(), listed("a", "b"), nil, 4, 2, time.Time{}, out,
						func(_ context.Context, pkg load.Listed, _ int, spare func() int) int {
							if pkg.ImportPath == "a" {
								<-release
								return exitDetected
							}
							before = spare()
							<-proceed
							after = spare()
							return exitDetected
						})
				}()
				synctest.Wait()
				close(release)
				synctest.Wait()
				close(proceed)
				<-done
				assert.Equal(t, before, 0, "no thread is free while a and b run")
				assert.Equal(t, after, 2, "b may use the threads that a returned")
			})
		})

		t.Run("runs at most the given number of packages at once", func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			running, most := 0, 0
			schedule(t.Context(), listed("a", "b", "c", "d", "e"), nil, 4, 2, time.Time{}, out,
				func(context.Context, load.Listed, int, func() int) int {
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

		stops := []struct {
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
		for _, tt := range stops {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var errs bytes.Buffer
				called := false
				got := schedule(tt.ctx(), listed("a"), nil, 4, 1, tt.deadline,
					&output{stdout: io.Discard, stderr: &errs},
					func(context.Context, load.Listed, int, func() int) int {
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
