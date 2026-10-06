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

// mutants runs each mutant without a verdict alone, on cfg.Workers
// workers. It starts the runs in the order of the mutants' keys, and of the
// record for two equal keys, so a run that the caller's deadline ends has
// run a uniform sample of the mutants. Under the caller's deadline, a
// mutant starts only while the time left covers the reserve of a mutant, and
// a timer ends the wait for a free worker once the time left no longer
// covers it.
//
// When ctx or the caller's deadline ends the starts, or cfg.Sample mutants
// have started, each mutant that no worker took is not-run at once, while
// the mutants that the workers took still run, so the caller sees that no
// further mutant starts. The run is limited when cfg.Sample alone ended the
// starts and no mutant that started is not-run.
func (r *runner) mutants(ctx context.Context) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, r.cfg.Workers) {
		wg.Go(func() {
			for i := range jobs {
				r.mutant(ctx, i, r.prog.Ordinals[r.order[i]])
			}
		})
	}
	pending := r.pending()
	slices.SortStableFunc(pending, func(a, b int) int {
		return strings.Compare(r.rec.Mutants[a].Key, r.rec.Mutants[b].Key)
	})
	tooLate := "the caller's deadline leaves too little time for the mutant and the closing control run"
	if r.cfg.Confirm {
		tooLate = "the caller's deadline leaves too little time for the mutant, its confirmation and the closing control run"
	}
	// expired receives the time when the time left no longer covers the
	// reserve of a mutant. It is nil, and receives nothing, without a
	// deadline.
	var expired <-chan time.Time
	if !r.cfg.Deadline.IsZero() {
		timer := time.NewTimer(time.Until(r.cfg.Deadline.Add(-r.reserve())))
		defer timer.Stop()
		expired = timer.C
	}
	next, reason, limited := 0, "", false
	for next < len(pending) && reason == "" {
		if next == r.cfg.Sample && next > 0 {
			reason, limited = "the caller limits the run to "+strconv.Itoa(next)+" mutants", true
			continue
		}
		// The timer's channel can be empty for a moment after its time, so
		// the time left is checked before each wait too.
		if !r.cfg.Deadline.IsZero() && time.Until(r.cfg.Deadline) < r.reserve() {
			reason = tooLate
			continue
		}
		select {
		case jobs <- pending[next]:
			next++
		case <-ctx.Done():
			reason = cancelled
		case <-expired:
			reason = tooLate
		}
	}
	for _, i := range pending[next:] {
		r.set(i, outcome{verdict: spec.NotRun, reason: reason})
	}
	close(jobs)
	wg.Wait()
	r.limited = limited && !slices.ContainsFunc(pending[:next], func(i int) bool {
		return r.rec.Mutants[i].Verdict == spec.NotRun
	})
}
