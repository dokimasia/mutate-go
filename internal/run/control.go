// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.dokimi.dev/mutate/internal/memory"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// openingLimit bounds the opening control run of each test binary of a
// caller without a deadline, as go test bounds a test binary by default.
const openingLimit = 10 * time.Minute

// tracePrefix starts the name of a trace file in the run's work directory.
const tracePrefix = "trace"

// messageTail is the most bytes of a failed control run's output that the
// run error states.
const messageTail = 8 << 10

// openingLimit returns the limit of one test binary's opening control run,
// and whether the caller's deadline sets it: at most half of the time left.
func (r *runner) openingLimit() (time.Duration, bool) {
	if !r.cfg.Deadline.IsZero() {
		if half := time.Until(r.cfg.Deadline) / 2; half < openingLimit {
			return half, true
		}
	}
	return openingLimit, false
}

// opening runs the opening control run of each program, and sets each
// program's limits and its top-level tests from its run. Without
// cfg.Confirm, it gives the verdict no-coverage to each mutant whose site
// the programs never executed. It returns the reason that the run stops
// before the mutant runs, or "" when they start. A run that the caller's
// deadline ends is not a failure of the tests.
func (r *runner) opening(ctx context.Context) string {
	tooLate := "the caller's deadline leaves too little time for the opening control run"
	limits := r.def.Protocol.Limits
	var seconds float64
	var peak *int64
	executed := map[int]bool{}
	for i, p := range r.programs {
		limit, late := r.openingLimit()
		if limit <= 0 {
			return tooLate
		}
		r.ran = true
		trace := filepath.Join(r.work, tracePrefix+strconv.Itoa(i))
		res := r.execute(ctx, p, p.bin, r.env(0, trace), testbin.Flags(limit, false), limit, testbin.BackupDelay, false)
		verdict, _, _ := classify(res)
		switch {
		case res.Ended == testbin.Cancelled:
			return cancelled
		case late && verdict == spec.TimedOut:
			return tooLate
		case failed(res):
			r.fail(spec.ErrorControl, describe("the tests fail with no mutant active", res, p.target))
			return "the opening control run failed"
		}
		data, _ := os.ReadFile(trace)
		var started bool
		p.executed, started = render.ParseTrace(data)
		if !started {
			r.fail(
				spec.ErrorNotInstrumented,
				"the opening control run's trace"+of(p.target)+
					" has no start mark, so the test binary did not run the instrumented package",
			)
			return "the opening control run failed"
		}
		for o := range p.executed {
			executed[o] = true
		}
		p.tests = res.Output.Tests
		p.deadline = time.Duration(
			(limits.Deadline.Factor*res.Seconds + limits.Deadline.Seconds) * float64(time.Second),
		)
		seconds += res.Seconds
		if b := memory.Peak(res.State); b != nil {
			p.ceiling = int64(limits.Memory.Factor*float64(*b)) + limits.Memory.Bytes
			if peak == nil || *b > *peak {
				peak = b
			}
		}
	}
	r.rec.Control = &record.Control{
		Opening: record.Opening{Seconds: seconds, PeakBytes: peak, Sites: len(r.prog.Sites)},
	}
	for _, s := range r.prog.Sites {
		if executed[r.prog.Ordinals[s.Mutants[0]]] {
			r.rec.Control.Opening.SitesExecuted++
		}
	}
	for _, p := range r.programs {
		l := record.Limits{Target: r.rec.Target.Name, DeadlineSeconds: p.deadline.Seconds()}
		if p.target != "" {
			l.Target = p.target
		}
		if p.ceiling > 0 {
			ceiling := p.ceiling
			l.MemoryCeilingBytes = &ceiling
		}
		r.rec.Limits = append(r.rec.Limits, l)
		r.total += p.deadline
	}
	if !r.cfg.Confirm {
		r.uncovered(executed)
	}
	return ""
}

// ordinary runs the ordinary control run. It builds each program from the
// package's unchanged source, as the toolchain builds it without the
// engine, and runs it with no mutant active and without the instrumented
// variable, under the program's limits. It states the run's time in the
// record, and returns the reason that the run stops before the mutants
// run, or "" when every program passed.
func (r *runner) ordinary(ctx context.Context) string {
	if !r.cfg.Deadline.IsZero() && time.Until(r.cfg.Deadline) < 2*r.total {
		return "the caller's deadline leaves too little time for the ordinary control run"
	}
	dir := filepath.Join(r.work, ordinaryDir)
	defer os.RemoveAll(dir)
	var seconds float64
	for _, p := range r.programs {
		start := time.Now()
		bin, stop := r.ordinaryBinary(ctx, dir, p, "")
		r.ordinaryBuild += time.Since(start)
		if stop != nil {
			reason := stop.reason
			if stop.verdict == spec.NotViable {
				r.fail(spec.ErrorBuild, "the ordinary build of the unchanged source fails: "+stop.reason)
				reason = "the ordinary build failed"
			}
			return reason
		}
		res := r.execute(ctx, p, bin, r.ordinaryEnv(0), testbin.Flags(p.deadline, false), p.deadline,
			testbin.BackupDelay, false)
		if res.Ended == testbin.Cancelled {
			return cancelled
		}
		if failed(res) {
			r.fail(
				spec.ErrorOrdinary,
				describe("the tests fail in an ordinary build with no mutant active", res, p.target),
			)
			return "the ordinary control run failed"
		}
		seconds += res.Seconds
	}
	r.rec.Control.Ordinary = &record.Ordinary{Seconds: seconds}
	return ""
}

// closing runs the closing control run of each program, each under the
// program's limits, unless the caller cancelled the run. A run that the
// caller cancels before or during it states no closing control run, and
// fails, as [record.Record.Failed] states, without a run error.
func (r *runner) closing(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	var seconds float64
	for _, p := range r.programs {
		res := r.execute(ctx, p, p.bin, r.env(0, ""), testbin.Flags(p.deadline, false), p.deadline,
			testbin.BackupDelay, false)
		if res.Ended == testbin.Cancelled {
			return
		}
		if failed(res) {
			r.fail(
				spec.ErrorClosing,
				describe("the tests fail with no mutant active after the mutant runs", res, p.target),
			)
			return
		}
		seconds += res.Seconds
	}
	r.rec.Control.Closing = &record.Closing{Seconds: seconds}
}

// failed reports whether a control run failed: it did not start, a test
// failed, the binary exited with a failure status, or the engine ended it.
func failed(res *testbin.Result) bool {
	return res.Err != nil || res.Ended != testbin.Exited || res.State.ExitCode() != 0
}

// describe states a control run's failure after prefix: the tests that the
// run names and what they did, or the reason of a run that ended in an
// error, and then the end of the run's output. target is the package whose
// tests the binary runs, or empty for the run's own package.
func describe(prefix string, res *testbin.Result, target string) string {
	verdict, tests, reason := classify(res)
	cause := reason
	if did, ok := failures[verdict]; ok {
		subject := strings.Join(named(tests, target), ", ")
		if subject == "" {
			subject = "the test binary"
		}
		cause = subject + " " + did
	}
	tail := res.Output.Tail
	if len(tail) > messageTail {
		tail = tail[len(tail)-messageTail:]
	}
	return prefix + ": " + cause + "\n" + tail
}
