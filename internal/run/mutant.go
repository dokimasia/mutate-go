// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// minimumLeft is the least deadline of a part of a mutant's run, so that
// the testing package's alarm is set when the parts before used the whole
// deadline.
const minimumLeft = time.Millisecond

// mutant runs the mutant at index i, whose ordinal is ordinal, and gives the
// mutant the outcome. It runs the instrumented test binaries whose opening
// control run executed the mutant's site, with the mutant active, as
// runPrograms states, and under cfg.Confirm confirms a survivor in those
// binaries. A survivor's confirmation starts only while the time left
// covers it, as confirmable states, and a survivor whose confirmation does
// not fit is not-run. A mutant without coverage runs only under
// cfg.Confirm, in its confirmation in every binary, and keeps no-coverage
// when every binary passes. When ctx is done, the run ends at once and
// gives the mutant not-run.
func (r *runner) mutant(ctx context.Context, i, ordinal int) {
	covering := r.covering(i)
	if len(covering) == 0 {
		o := r.confirm(ctx, i, ordinal, r.programs)
		if o.verdict == spec.Survived {
			o.verdict = spec.NoCoverage
		}
		r.set(i, o)
		return
	}
	o := r.runPrograms(ctx, i, covering, r.env(ordinal, ""), true, func(p *program) (string, *outcome) {
		return p.bin, nil
	})
	if o.verdict == spec.Survived && r.cfg.Confirm {
		if r.confirmable(covering) {
			o = r.confirm(ctx, i, ordinal, covering)
		} else {
			o = outcome{
				verdict: spec.NotRun,
				reason:  "the caller's deadline leaves too little time for the confirmation and the closing control run",
			}
		}
	}
	r.set(i, o)
}

// covering returns the programs whose opening control run executed the
// site of the mutant at index i, in the order of the programs.
func (r *runner) covering(i int) []*program {
	var programs []*program
	for _, p := range r.programs {
		if p.executed[r.first(i)] {
			programs = append(programs, p)
		}
	}
	return programs
}

// confirmable reports whether the time left before the caller's deadline
// covers a confirmation in programs: the programs' deadlines, the time of
// the ordinary control run's builds and the closing control run's
// deadline.
func (r *runner) confirmable(programs []*program) bool {
	need := r.ordinaryBuild + r.total
	for _, p := range programs {
		need += p.deadline
	}
	return r.fits(need)
}

// runPrograms runs each of programs in order, with the environment env,
// until one does not pass, and returns the outcome of the run that did not
// pass, or survived. binary returns a program's test binary, or the outcome
// of a build that gave none, which ends the runs. Each program's run ends
// at the program's deadline and memory ceiling. When first is true, the
// run of a program starts with the part that runs states for the mutant at
// index i, which ends at its own deadline, and the program's deadline
// applies to the parts together.
func (r *runner) runPrograms(
	ctx context.Context,
	i int,
	programs []*program,
	env []string,
	first bool,
	binary func(*program) (string, *outcome),
) outcome {
	var seconds float64
	for _, p := range programs {
		bin, stop := binary(p)
		if stop != nil {
			return *stop
		}
		parts := []part{{}}
		if first {
			parts = r.runs(p, i)
		}
		var used time.Duration
		for _, pt := range parts {
			left := max(p.deadline-used, minimumLeft)
			if pt.deadline > 0 {
				left = min(left, pt.deadline)
			}
			// A run ends at its deadline, unless it hangs before the testing
			// package's alarm starts. The engine then ends it, at the latest
			// when the time left falls to the closing control run's deadline.
			backup := testbin.BackupDelay
			if !r.cfg.Deadline.IsZero() {
				backup = min(backup, max(0, time.Until(r.cfg.Deadline)-2*r.total))
			}
			args := append(testbin.Flags(left, true), pt.filter...)
			res := r.execute(ctx, p, bin, env, args, left, backup, true)
			used += time.Duration(res.Seconds * float64(time.Second))
			seconds += res.Seconds
			if v, ts, why := classify(res); v != spec.Survived {
				o := outcome{verdict: v, tests: named(ts, p.target), reason: why}
				if res.Err == nil && v != spec.NotRun {
					o.seconds = &seconds
				}
				return o
			}
		}
	}
	return outcome{verdict: spec.Survived, seconds: &seconds}
}

// runs returns the parts of the runs of program p for the mutant at index
// i: the tests that executed the mutant's site when they ran alone, and then
// p's whole suite. The first part ends at the protocol's factor times the
// tests' wall times in their runs alone, plus its constant. When p's tests
// did not run alone, or no test or every test executed the site, the whole
// suite is the only part.
func (r *runner) runs(p *program, i int) []part {
	var tests []string
	var seconds float64
	for _, test := range p.tests {
		if p.sites[test][r.first(i)] {
			tests = append(tests, test)
			seconds += p.seconds[test]
		}
	}
	if len(tests) == 0 || len(tests) == len(p.tests) {
		return []part{{}}
	}
	limit := r.def.Protocol.Limits.Deadline
	return []part{
		{
			filter:   []string{testbin.Only(tests...)},
			deadline: time.Duration((limit.Factor*seconds + limit.Seconds) * float64(time.Second)),
		},
		{},
	}
}

// confirm runs the ordinary build of the mutant at index i, whose ordinal
// is ordinal, in programs, and returns the outcome of that build's runs. It
// writes the mutant into the package's source as render.Plain writes it,
// with the names of the instrumented program. It builds each of programs
// from that source, and runs its whole suite with the mutant's ordinal and
// without the instrumented variable, as runPrograms states.
//
// The outcome is confirmed unless ctx ended the confirmation, which makes
// the mutant not-run. A build that the toolchain rejects makes the mutant
// not-viable, with the toolchain's message as its reason.
func (r *runner) confirm(ctx context.Context, i, ordinal int, programs []*program) outcome {
	dir := filepath.Join(r.work, confirmPrefix+strconv.Itoa(i))
	defer os.RemoveAll(dir)
	overlay, err := render.Plain(r.pkg, r.order[i], r.prog.Prefix).Write(filepath.Join(dir, sourceDir))
	if err != nil {
		return outcome{
			verdict:   spec.Error,
			reason:    "the ordinary build was not written: " + err.Error(),
			confirmed: true,
		}
	}
	o := r.runPrograms(ctx, i, programs, r.ordinaryEnv(ordinal), false, func(p *program) (string, *outcome) {
		return r.ordinaryBinary(ctx, dir, p, overlay)
	})
	o.confirmed = o.verdict != spec.NotRun
	return o
}
