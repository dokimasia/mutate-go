// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"sync"
)

// budget admits the runs of packages while the memory that they may use at
// once fits in total bytes. A package's runs may use its workers times the
// largest memory ceiling of its test binaries. A package whose runs need
// more than total on their own starts when no other package's runs are
// admitted, so every package starts.
//
// # Concurrency
//
// A budget is safe for concurrent use. Its zero value is not usable:
// newBudget returns one.
type budget struct {
	total int64
	mu    sync.Mutex
	// used is the memory of the admitted runs, and admitted their number.
	used     int64
	admitted int
	// changed is closed and replaced at each release, which wakes every
	// package that waits.
	changed chan struct{}
}

// newBudget returns a budget of total bytes.
func newBudget(total int64) *budget {
	return &budget{total: total, changed: make(chan struct{})}
}

// admit waits until runs that may use bytes fit in the budget beside the
// admitted runs, or until no runs are admitted, and returns the function
// that releases them. It returns false when ctx ends first.
func (b *budget) admit(ctx context.Context, bytes int64) (func(), bool) {
	for {
		b.mu.Lock()
		if b.admitted == 0 || b.used+bytes <= b.total {
			b.used += bytes
			b.admitted++
			b.mu.Unlock()
			return func() { b.release(bytes) }, true
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, false
		}
	}
}

// release returns the memory of runs that admit admitted, and wakes the
// packages that wait.
func (b *budget) release(bytes int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= bytes
	b.admitted--
	close(b.changed)
	b.changed = make(chan struct{})
}
