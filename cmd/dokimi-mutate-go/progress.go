// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// progress tracks the packages that run, and writes a line on each to
// stderr at every interval every, measured by now. deadline is when
// -timeout ends the runs, or zero without -timeout.
//
// # Concurrency
//
// A progress is safe for concurrent use.
type progress struct {
	stderr   io.Writer
	every    time.Duration
	now      func() time.Time
	deadline time.Time
	mu       sync.Mutex
	// running maps the import path of each package that runs to its tally.
	running map[string]*tally
}

// tally counts the mutants of one package's run: those with a final verdict
// other than not-run, the undetected ones among them, the ones whose run
// ended, the ones that will not run, and all, which are 0 until the first
// verdict. started is when the run of the first mutant that ended started.
type tally struct {
	done, undetected, ran, stopped, total int
	started                               time.Time
}

// begin starts the tally of the package importPath, whose run starts.
func (p *progress) begin(importPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running == nil {
		p.running = map[string]*tally{}
	}
	p.running[importPath] = &tally{}
}

// verdict counts m, one of the mutants of the package importPath whose
// verdict is final: as done, or as stopped when m is not-run. mutants is the
// number of the package's mutants. A mutant with Seconds ran, and the first
// such mutant marks when the runs started.
func (p *progress) verdict(importPath string, m record.Mutant, mutants int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.running[importPath]
	if m.Verdict == spec.NotRun {
		s.stopped++
	} else {
		s.done++
	}
	s.total = mutants
	if m.Seconds != nil {
		if s.ran == 0 {
			s.started = p.now().Add(-time.Duration(*m.Seconds * float64(time.Second)))
		}
		s.ran++
	}
	if m.Verdict == spec.Survived || m.Verdict == spec.NoCoverage {
		s.undetected++
	}
}

// end ends the tally of the package importPath, whose run ended.
func (p *progress) end(importPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, importPath)
}

// watch writes the line of every package that runs at each interval every,
// until the function that it returns is called.
func (p *progress) watch() (stop func()) {
	ticker := time.NewTicker(p.every)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				p.tick()
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		<-stopped
	}
}

// tick writes one line for each package that runs, in import path order.
// The line states that the package runs while none of its mutants has a
// verdict. Then it states how many are done, and how many of those are
// undetected. Once a mutant's run ended, it states the rate of the runs and
// the time left at that rate. Once a mutant is not-run, no further mutant
// starts, and the line states how many will not run and that the run waits
// for the mutants that still run and the closing control run. Under
// -timeout, the line ends with the time until the timeout.
func (p *progress) tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for _, pkg := range slices.Sorted(maps.Keys(p.running)) {
		s := p.running[pkg]
		line := "running"
		if s.total > 0 {
			line = fmt.Sprintf("%d of %d mutants done, %d undetected", s.done, s.total, s.undetected)
		}
		switch running := s.total - s.done - s.stopped; {
		case s.stopped > 0:
			line += fmt.Sprintf(", %d not run, waiting for ", s.stopped)
			if running == 1 {
				line += "1 mutant run and "
			} else if running > 1 {
				line += fmt.Sprintf("%d mutant runs and ", running)
			}
			line += "the closing control run"
		case s.ran > 0:
			rate := float64(s.ran) / now.Sub(s.started).Seconds()
			left := time.Duration(float64(s.total-s.done) / rate * float64(time.Second))
			line += fmt.Sprintf(", %.1f mutants a second, about %s left", rate, left.Round(time.Second))
		}
		if left := p.deadline.Sub(now); !p.deadline.IsZero() && left > 0 {
			line += fmt.Sprintf(", -%s in %s", flagTimeout, left.Round(time.Second))
		} else if !p.deadline.IsZero() {
			line += fmt.Sprintf(", -%s passed", flagTimeout)
		}
		fmt.Fprintf(p.stderr, "%s: %s: %s\n", name, pkg, line)
	}
}
