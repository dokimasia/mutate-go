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
// returns. Each package gets GOMAXPROCS divided by the number of packages
// that run at once, parallel or the number of packages left to start when
// that is less, and at least 1. A package starts only while ctx is not done
// and deadline, when it is not zero, has not passed. schedule writes a note
// on each package that does not start, and returns exitFailed for it.
func schedule(
	ctx context.Context,
	pkgs []load.Listed,
	parallel int,
	deadline time.Time,
	out *output,
	test func(ctx context.Context, pkg load.Listed, procs int) int,
) int {
	var mu sync.Mutex
	status := exitDetected
	raise := func(s int) {
		mu.Lock()
		defer mu.Unlock()
		status = max(status, s)
	}
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
		if why != "" {
			<-slots
			out.errorf("%s: not tested, because %s", pkg.ImportPath, why)
			raise(exitFailed)
			continue
		}
		procs := max(1, runtime.GOMAXPROCS(0)/min(parallel, len(pkgs)-i))
		wg.Go(func() {
			raise(test(ctx, pkg, procs))
			<-slots
		})
	}
	wg.Wait()
	return status
}
