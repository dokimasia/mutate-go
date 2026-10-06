// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
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
// exit status of its run: exitFailed for a run that returns an error or
// whose record does not write, and the status that exitStatus states
// otherwise. It writes each listed mutant's line when the mutant's verdict
// is final, and the run's result when the run ends.
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
	if s.records != "" {
		if _, err := rec.Write(s.records); err != nil {
			s.out.errorf("%v", err)
			return exitFailed
		}
	}
	return exitStatus(rec)
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

// exitStatus returns the exit status of the run whose record is rec:
// exitFailed for a run that fails, exitUndetected for a run with a mutant
// that survived or that no test covers, and exitDetected otherwise. A run
// that the limit of -sample ended exits by its sample, whose mutants the
// score counts, so a mutant without coverage after the sample does not
// change the status.
func exitStatus(rec *record.Record) int {
	if rec.Failed() {
		return exitFailed
	}
	_, undetected := rec.Tally(definition.Protocol, "")
	if rec.Sample != nil && rec.Sample.Limit != nil {
		undetected = rec.Sample.Undetected
	}
	if undetected > 0 {
		return exitUndetected
	}
	return exitDetected
}
