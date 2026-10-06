// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.dokimi.dev/mutate/internal/spec"
)

// reserve returns the time that one more mutant needs before the caller's
// deadline when it starts: the deadlines of its run and of the closing
// control run. A survivor's confirmation needs time of its own, which
// confirmable checks when the survivor's run has ended.
func (r *runner) reserve() time.Duration {
	return 2 * r.total
}

// fits reports whether the time left before the caller's deadline covers
// d. Without a deadline, every duration fits.
func (r *runner) fits(d time.Duration) bool {
	return r.cfg.Deadline.IsZero() || time.Until(r.cfg.Deadline) >= d
}

// dispatch starts the jobs 0 to n-1 in order on the run's workers, each by
// a call of do with the job's index. Job k starts only while ctx is not done
// and, under the caller's deadline, while the time left covers need(k). A
// timer ends the wait for a free worker once the time left no longer covers
// need(k), so no job starts after its time because the workers were busy.
//
// Each worker's test binaries get cfg.Procs divided by cfg.Workers threads,
// and at least 1, and dispatch starts as many workers as the run's threads
// allow, at most cfg.Workers. While every worker runs a job and fewer than
// cfg.Workers run, dispatch asks cfg.Borrow for the threads of another
// worker. It asks again before the next job, and when the channel of a
// refusal closes.
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
	most := max(1, r.cfg.Workers)
	each := max(1, r.procs/most)
	workers := min(most, max(1, r.procs/each))
	jobs := make(chan int)
	// busy counts the jobs that started and have not ended.
	var busy atomic.Int64
	work := func() {
		for k := range jobs {
			do(k)
			busy.Add(-1)
		}
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Go(work)
	}
	for started < n && !late && ctx.Err() == nil {
		// freed closes when threads that cfg.Borrow refused may have become
		// free. It is nil, and receives nothing, while dispatch asks for no
		// threads.
		var freed <-chan struct{}
		if workers < most && r.cfg.Borrow != nil && busy.Load() == int64(workers) {
			release, refused := r.cfg.Borrow(each)
			if release != nil {
				workers++
				wg.Go(func() {
					defer release()
					work()
				})
			}
			freed = refused
		}
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
			busy.Add(1)
		case <-ctx.Done():
		case <-expired:
			late = true
		case <-freed:
		}
		if timer != nil {
			timer.Stop()
		}
	}
	close(jobs)
	return started, late, wg.Wait
}

// mutants runs each mutant without a verdict alone, on the workers that
// dispatch starts. It starts the runs in the order of the
// mutants' keys, and of the record for two equal keys, so a run that the
// caller's deadline ends has run a uniform sample of the mutants. Under the
// caller's deadline, a mutant starts only while the time left covers the
// reserve of a mutant. Under cfg.Confirm, a mutant without coverage runs
// in its confirmation alone, so it also needs the time of the ordinary
// control run's builds.
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
	need := func(k int) time.Duration {
		if r.cfg.Confirm && len(r.covering(pending[k])) == 0 {
			return r.reserve() + r.ordinaryBuild
		}
		return r.reserve()
	}
	started, late, wait := r.dispatch(ctx, n, need, func(k int) {
		i := pending[k]
		r.mutant(ctx, i, r.prog.Ordinals[r.order[i]])
	})
	var reason string
	switch {
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
