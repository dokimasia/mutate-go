// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// The intervals of the progress lines in the cases of watch, and the time
// within which a line and the stop of the lines happen.
const (
	tickEvery = time.Millisecond
	tickWait  = 10 * time.Second
	tickPoll  = time.Millisecond
)

func TestProgress(t *testing.T) {
	t.Parallel()

	t.Run("tick", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
		// Each mutant that ran took 2 seconds, and tick reads the clock 2
		// seconds after the verdicts: 2 runs in 4 seconds.
		seconds := 2.0
		ran := []record.Mutant{
			{Verdict: spec.Killed, Seconds: &seconds},
			{Verdict: spec.Survived, Seconds: &seconds},
		}
		// stopped returns the mutants of ran and n mutants that do not run.
		stopped := func(n int) []record.Mutant {
			out := append([]record.Mutant{}, ran...)
			for range n {
				out = append(out, record.Mutant{Verdict: spec.NotRun})
			}
			return out
		}
		tests := []struct {
			name     string
			verdicts []record.Mutant
			deadline time.Time
			want     string
		}{
			{"states that a package runs before its first verdict", nil, time.Time{}, "running"},
			{
				"states the mutants done and the undetected ones among them",
				[]record.Mutant{{Verdict: spec.NoCoverage}, {Verdict: spec.Suppressed}},
				time.Time{},
				"2 of 10 mutants done, 1 undetected",
			},
			{
				"states the rate of the runs and the time left at that rate",
				ran,
				time.Time{},
				"2 of 10 mutants done, 1 undetected, 0.5 mutants a second, about 16s left",
			},
			{
				"states the mutants that do not run and the mutant runs that the run waits for",
				stopped(5),
				time.Time{},
				"2 of 10 mutants done, 1 undetected, 5 not run, waiting for 3 mutant runs and the closing control run",
			},
			{
				"states one mutant run that the run waits for in the singular",
				stopped(7),
				time.Time{},
				"2 of 10 mutants done, 1 undetected, 7 not run, waiting for 1 mutant run and the closing control run",
			},
			{
				"states that the run waits for the closing control run once each mutant has a verdict",
				stopped(8),
				time.Time{},
				"2 of 10 mutants done, 1 undetected, 8 not run, waiting for the closing control run",
			},
			{"states the time until -timeout", nil, start.Add(92 * time.Second), "running, -timeout in 1m30s"},
			{"states that -timeout passed", nil, start.Add(-time.Second), "running, -timeout passed"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var errs bytes.Buffer
				now := start
				p := &progress{stderr: &errs, now: func() time.Time { return now }, deadline: tt.deadline}
				p.begin(fixture)
				for _, m := range tt.verdicts {
					p.verdict(fixture, m, 10)
				}
				now = now.Add(2 * time.Second)
				p.tick()
				assert.Equal(t, errs.String(), name+": fixture: "+tt.want+"\n", "the line states the package's run")
			})
		}

		t.Run("writes the line of each package that runs in import path order", func(t *testing.T) {
			t.Parallel()
			var errs bytes.Buffer
			p := &progress{stderr: &errs, now: time.Now}
			for _, pkg := range []string{"c", "a", "b"} {
				p.begin(pkg)
			}
			p.end("c")
			p.tick()
			assert.Equal(t, errs.String(), name+": a: running\n"+name+": b: running\n",
				"the packages that run have a line each")
		})
	})

	t.Run("watch", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the lines at each interval until the stop", func(t *testing.T) {
			t.Parallel()
			errs := &lockedWriter{w: &bytes.Buffer{}}
			p := &progress{stderr: errs, every: tickEvery, now: time.Now}
			p.begin(fixture)
			stop := p.watch()
			assert.Eventually(t, tickWait, tickPoll, func(tb assert.TB) {
				errs.mu.Lock()
				defer errs.mu.Unlock()
				assert.Contains(tb, errs.w.(*bytes.Buffer).String(), name+": fixture: running\n", "a line arrives")
			}, "the progress writes a line at the interval")
			assert.CompletesWithin(t, tickWait, func(context.Context) error {
				stop()
				return nil
			}, "the stop ends the lines")
		})

		t.Run("writes nothing before the first interval", func(t *testing.T) {
			t.Parallel()
			p := &progress{stderr: io.Discard, every: time.Hour, now: time.Now}
			stop := p.watch()
			assert.CompletesWithin(t, tickWait, func(context.Context) error {
				stop()
				return nil
			}, "the stop ends the watch at once")
		})
	})
}
