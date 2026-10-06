// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"context"
	"os"
	"slices"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/run"
)

// packages returns the packages that the patterns name, as go list resolves
// them in the working directory. It writes the error of each package that
// go list states, and reports false when go list fails or a package has an
// error, so the command line is invalid.
func packages(ctx context.Context, patterns []string, out *output) ([]load.Listed, bool) {
	listed, err := load.List(ctx, load.Config{Dir: workingDir, Env: os.Environ()}, patterns)
	if err != nil {
		out.errorf("%v", err)
		return nil, false
	}
	var pkgs []load.Listed
	for _, pkg := range listed {
		if pkg.Error != nil {
			out.errorf("%s: %s", pkg.ImportPath, pkg.Error.Err)
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, len(pkgs) == len(listed)
}

// suite resolves the patterns of -suite in the working directory, as the
// command resolves the packages, and returns the import paths of the
// packages that they name. Each run resolves its suite in its package's
// directory, where an import path names the same package as in the working
// directory, whatever symbolic links lead to either. It writes the error of
// each pattern that does not resolve, and of each package that go list
// states an error of, and reports false for either.
func suite(ctx context.Context, patterns []string, out *output) ([]string, bool) {
	if len(patterns) == 0 {
		return nil, true
	}
	listed, err := load.List(ctx, load.Config{Dir: workingDir, Env: os.Environ()}, patterns)
	if err != nil {
		out.errorf("-%s: %v", flagSuite, err)
		return nil, false
	}
	var paths []string
	for _, l := range listed {
		if problem := l.Problem(); problem != "" {
			out.errorf("-%s: %s: %s", flagSuite, l.ImportPath, problem)
			continue
		}
		paths = append(paths, l.ImportPath)
	}
	return paths, len(paths) == len(listed)
}

// weigh returns the number of mutants that a run of cfg would test in each
// of pkgs, as run.Count counts them, for at most parallel packages at once.
// Each count's go commands get threads divided by parallel, and at least 1.
func weigh(ctx context.Context, pkgs []load.Listed, cfg run.Config, threads, parallel int) []int {
	weights := make([]int, len(pkgs))
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		slots <- struct{}{}
		c := cfg
		c.Dir, c.Procs = pkg.Dir, max(1, threads/parallel)
		wg.Go(func() {
			weights[i] = run.Count(ctx, c)
			<-slots
		})
	}
	wg.Wait()
	return weights
}

// schedule calls test for each of pkgs, for at most parallel packages at
// once, and returns the highest exit status that test returns. It starts
// the packages in the order of their weights, the heaviest first, and in the
// order of pkgs among equal weights. nil weights weigh every package
// equally. A package starts only while ctx is not done and deadline, when
// it is not zero, has not passed. schedule writes a note on each package
// that does not start, and returns exitFailed for it.
//
// A package's share of the threads follows these rules:
//
//   - It is a part of the threads that the running packages leave free, in
//     proportion to the package's weight against the weights of the
//     packages that may start beside it: the free slots of parallel, or the
//     packages left to start when they are fewer.
//   - It is rounded, at least 1 and at most the free threads, and equal
//     where those packages weigh 0.
//   - While threads is at least parallel, a package starts only when a
//     thread is free. The packages that run at once then use at most
//     threads threads, and fewer of them than parallel run where large
//     packages take more threads. Below parallel threads, each package gets
//     one thread.
//   - A package returns its share when its run ends.
//
// test receives the package's share, and a function that returns the
// threads that the running packages leave free, divided among them, for the
// package's go commands.
func schedule(
	ctx context.Context,
	pkgs []load.Listed,
	weights []int,
	threads, parallel int,
	deadline time.Time,
	out *output,
	test func(ctx context.Context, pkg load.Listed, procs int, spare func() int) int,
) int {
	weight := func(i int) int64 {
		if weights == nil {
			return 1
		}
		return int64(weights[i])
	}
	order := make([]int, len(pkgs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(weight(b), weight(a)) })
	// mu guards the status and the threads and the packages that run.
	// released receives a signal when a package returns its share.
	var mu sync.Mutex
	status, used, running := exitDetected, 0, 0
	released := make(chan struct{}, 1)
	spare := func() int {
		mu.Lock()
		defer mu.Unlock()
		if running == 0 || used >= threads {
			return 0
		}
		return (threads - used) / running
	}
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for n, i := range order {
		pkg := pkgs[i]
		slots <- struct{}{}
		mu.Lock()
		for threads >= parallel && used >= threads {
			mu.Unlock()
			<-released
			mu.Lock()
		}
		why := ""
		if ctx.Err() != nil {
			why = "the command was interrupted"
		} else if !deadline.IsZero() && time.Now().After(deadline) {
			why = "-" + flagTimeout + " passed before the package started"
		}
		if why != "" {
			status = max(status, exitFailed)
			mu.Unlock()
			<-slots
			out.errorf("%s: not tested, because %s", pkg.ImportPath, why)
			continue
		}
		beside := order[n : n+min(parallel-running, len(order)-n)]
		var window int64
		for _, j := range beside {
			window += weight(j)
		}
		free, w := int64(threads-used), weight(i)
		if window == 0 {
			window, w = int64(len(beside)), 1
		}
		procs := 1
		if free > 1 {
			procs = int(min(max((2*free*w+window)/(2*window), 1), free))
		}
		used, running = used+procs, running+1
		mu.Unlock()
		wg.Go(func() {
			s := test(ctx, pkg, procs, spare)
			mu.Lock()
			status, used, running = max(status, s), used-procs, running-1
			mu.Unlock()
			select {
			case released <- struct{}{}:
			default:
			}
			<-slots
		})
	}
	wg.Wait()
	return status
}
