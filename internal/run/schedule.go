// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/spec"
)

// reserve returns the time that one more mutant needs before the caller's
// deadline: the deadlines of its run and of the closing control run, and
// under cfg.Confirm also the deadline of its confirmation run and the time
// of the ordinary control run's builds.
func (r *runner) reserve() time.Duration {
	if r.cfg.Confirm {
		return 3*r.total + r.ordinaryBuild
	}
	return 2 * r.total
}

// dispatch starts the jobs 0 to n-1 in order on cfg.Workers workers, each by
// a call of do with the job's index. Job k starts only while ctx is not done
// and, under the caller's deadline, while the time left covers need(k). A
// timer ends the wait for a free worker once the time left no longer covers
// need(k), so no job starts after its time because the workers were busy.
//
// dispatch returns when the starts end, while the jobs that started still
// run: the number of jobs that started, whether the caller's deadline ended
// the starts, and the function that waits until every started job has
// ended, which the caller calls once. When fewer than n jobs started and
// late is false, ctx ended the starts.
func (r *runner) dispatch(
	ctx context.Context,
	n int,
	need func(k int) time.Duration,
	do func(k int),
) (started int, late bool, wait func()) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, r.cfg.Workers) {
		wg.Go(func() {
			for k := range jobs {
				do(k)
			}
		})
	}
	for started < n && !late && ctx.Err() == nil {
		// expired receives the time when the time left no longer covers the
		// job. It is nil, and receives nothing, without a deadline. A timer's
		// channel can be empty for a moment after its time, so the time left
		// is checked before the wait too.
		var expired <-chan time.Time
		var timer *time.Timer
		if !r.cfg.Deadline.IsZero() {
			left := time.Until(r.cfg.Deadline) - need(started)
			if left < 0 {
				late = true
				break
			}
			timer = time.NewTimer(left)
			expired = timer.C
		}
		select {
		case jobs <- started:
			started++
		case <-ctx.Done():
		case <-expired:
			late = true
		}
		if timer != nil {
			timer.Stop()
		}
	}
	close(jobs)
	return started, late, wg.Wait
}

// mutants runs each mutant without a verdict alone, on cfg.Workers
// workers, as dispatch starts them. It starts the runs in the order of the
// mutants' keys, and of the record for two equal keys, so a run that the
// caller's deadline ends has run a uniform sample of the mutants. Under the
// caller's deadline, a mutant starts only while the time left covers the
// reserve of a mutant.
//
// When ctx or the caller's deadline ends the starts, or cfg.Sample mutants
// have started, each mutant that no worker took is not-run at once, while
// the mutants that the workers took still run, so the caller sees that no
// further mutant starts. The run is limited when cfg.Sample alone ended the
// starts and no mutant that started is not-run.
func (r *runner) mutants(ctx context.Context) {
	pending := r.pending()
	slices.SortStableFunc(pending, func(a, b int) int {
		return strings.Compare(r.rec.Mutants[a].Key, r.rec.Mutants[b].Key)
	})
	n := len(pending)
	if r.cfg.Sample > 0 {
		n = min(n, r.cfg.Sample)
	}
	started, late, wait := r.dispatch(ctx, n, func(int) time.Duration { return r.reserve() }, func(k int) {
		i := pending[k]
		r.mutant(ctx, i, r.prog.Ordinals[r.order[i]])
	})
	var reason string
	switch {
	case late && r.cfg.Confirm:
		reason = "the caller's deadline leaves too little time for the mutant, its confirmation and the closing control run"
	case late:
		reason = "the caller's deadline leaves too little time for the mutant and the closing control run"
	case started < n:
		reason = cancelled
	default:
		reason = "the caller limits the run to " + strconv.Itoa(started) + " mutants"
	}
	for _, i := range pending[started:] {
		r.set(i, outcome{verdict: spec.NotRun, reason: reason})
	}
	wait()
	r.limited = started == n && n < len(pending) && !slices.ContainsFunc(pending[:started], func(i int) bool {
		return r.rec.Mutants[i].Verdict == spec.NotRun
	})
}
