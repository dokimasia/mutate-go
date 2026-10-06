// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/testbin"
)

// alone runs each top-level test of a program alone, with no mutant active
// and a trace on, on cfg.Workers workers and under the program's limits,
// and records the sites that each test executed. It does so for each
// program that has more than one test and fewer tests than the mutants
// without a verdict whose sites the program executed, because each test
// costs a run.
//
// A test's run starts, as dispatch starts it, only while the time left
// covers the program's deadline and the reserve of a mutant. alone drops a
// program's record when one of its tests fails alone, when the trace of a
// test's run does not read or lacks the start mark, because the trace then
// states no site that the engine can trust, or when ctx or the caller's
// deadline ends the runs before each of its tests ran.
func (r *runner) alone(ctx context.Context) {
	type job struct {
		p           *program
		test, trace string
	}
	var jobs []job
	pending := r.pending()
	for k, p := range r.programs {
		covered := 0
		for _, i := range pending {
			if p.executed[r.first(i)] {
				covered++
			}
		}
		if len(p.tests) < 2 || len(p.tests) >= covered {
			continue
		}
		p.sites, p.seconds = map[string]map[int]bool{}, map[string]float64{}
		for n, test := range p.tests {
			trace := filepath.Join(r.work, tracePrefix+strconv.Itoa(k)+"-"+strconv.Itoa(n))
			jobs = append(jobs, job{p: p, test: test, trace: trace})
		}
	}
	var mu sync.Mutex
	broken := map[*program]bool{}
	need := func(k int) time.Duration { return r.reserve() + jobs[k].p.deadline }
	_, _, wait := r.dispatch(ctx, len(jobs), need, func(k int) {
		j := jobs[k]
		args := append(testbin.Flags(j.p.deadline, true), testbin.Only(j.test))
		res := r.execute(ctx, j.p, j.p.bin, r.env(0, j.trace), args, j.p.deadline, testbin.BackupDelay, true)
		data, err := os.ReadFile(j.trace)
		sites, started := render.ParseTrace(data)
		mu.Lock()
		defer mu.Unlock()
		broken[j.p] = broken[j.p] || failed(res) || err != nil || !started
		j.p.sites[j.test], j.p.seconds[j.test] = sites, res.Seconds
	})
	wait()
	for _, p := range r.programs {
		if broken[p] || len(p.sites) < len(p.tests) {
			p.sites, p.seconds = nil, nil
		}
	}
}
