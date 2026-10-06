// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/load"
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

// schedule calls test for each of pkgs, for at most parallel packages at
// once, in the order of pkgs, and returns the highest exit status that test
// returns. A package starts only while ctx is not done and deadline, when
// it is not zero, has not passed. schedule writes a note on each package
// that does not start, and returns exitFailed for it.
//
// Each package gets a share of the threads that the running packages leave
// free, GOMAXPROCS minus their shares, divided by the packages that may
// start beside it: the free slots of parallel, or the packages left to
// start when they are fewer, and at least 1. A package returns its share
// when its run ends, so the packages that run at once use at most
// GOMAXPROCS threads, or one each where GOMAXPROCS is less than parallel.
func schedule(
	ctx context.Context,
	pkgs []load.Listed,
	parallel int,
	deadline time.Time,
	out *output,
	test func(ctx context.Context, pkg load.Listed, procs int) int,
) int {
	threads := runtime.GOMAXPROCS(0)
	// mu guards the status and the threads and the packages that run.
	var mu sync.Mutex
	status, used, running := exitDetected, 0, 0
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		slots <- struct{}{}
		why := ""
		if ctx.Err() != nil {
			why = "the command was interrupted"
		} else if !deadline.IsZero() && time.Now().After(deadline) {
			why = "-" + flagTimeout + " passed before the package started"
		}
		mu.Lock()
		if why != "" {
			status = max(status, exitFailed)
			mu.Unlock()
			<-slots
			out.errorf("%s: not tested, because %s", pkg.ImportPath, why)
			continue
		}
		procs := max(1, (threads-used)/min(parallel-running, len(pkgs)-i))
		used, running = used+procs, running+1
		mu.Unlock()
		wg.Go(func() {
			s := test(ctx, pkg, procs)
			mu.Lock()
			status, used, running = max(status, s), used-procs, running-1
			mu.Unlock()
			<-slots
		})
	}
	wg.Wait()
	return status
}
