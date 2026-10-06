// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// The command's exit statuses. When more than one applies, the command
// exits with the highest.
const (
	// exitDetected states that the tests detected every counted mutant,
	// and that every run completed.
	exitDetected = 0
	// exitUndetected states a mutant that survived or that no test covers.
	exitUndetected = 1
	// exitInvalid states a command line or an input that is not valid.
	exitInvalid = 2
	// exitFailed states a run that failed.
	exitFailed = 3
)

// session is the state that every package's run of one command line
// shares: the configuration of each run, the output, the progress, and the
// directory of -record, empty for none.
type session struct {
	cfg      run.Config
	out      *output
	progress *progress
	records  string
}

// check runs mutation testing on pkg with procs threads, and returns the
// exit status of its run: exitFailed for a run that fails, that returns an
// error or whose record does not write, exitUndetected for a run with a
// mutant that survived or that no test covers, and exitDetected otherwise.
// It writes each listed mutant's line when the mutant's verdict is final,
// and the run's result when the run ends.
func (s *session) check(ctx context.Context, pkg load.Listed, procs int) int {
	cfg := s.cfg
	cfg.Dir, cfg.Procs = pkg.Dir, procs
	cfg.Verdict = func(m record.Mutant, mutants int, path string) {
		s.progress.verdict(pkg.ImportPath, m, mutants)
		s.out.line(m, path)
	}
	s.progress.begin(pkg.ImportPath)
	rec, err := run.Run(ctx, cfg)
	s.progress.end(pkg.ImportPath)
	if err != nil {
		s.out.errorf("%s: %v", pkg.ImportPath, err)
		return exitFailed
	}
	s.out.result(rec)
	status := exitDetected
	for _, m := range rec.Mutants {
		if definition.Protocol.Class(m.Verdict) == spec.Undetected {
			status = exitUndetected
		}
	}
	if s.records != "" {
		if _, err := rec.Write(s.records); err != nil {
			s.out.errorf("%v", err)
			return exitFailed
		}
	}
	if rec.Failed() {
		return exitFailed
	}
	return status
}

// list lists the mutants of pkg with procs threads, and returns the exit
// status of the listing: exitFailed when the package does not load, an
// annotation is not valid or the instrumentation does not type-check, and
// exitDetected otherwise. It writes the listing when it ends.
func (s *session) list(ctx context.Context, pkg load.Listed, procs int) int {
	cfg := s.cfg
	cfg.Dir, cfg.Procs, cfg.List = pkg.Dir, procs, true
	rec, err := run.Run(ctx, cfg)
	if err != nil {
		s.out.errorf("%s: %v", pkg.ImportPath, err)
		return exitFailed
	}
	s.out.listing(rec)
	if len(rec.Errors) > 0 {
		return exitFailed
	}
	return exitDetected
}
