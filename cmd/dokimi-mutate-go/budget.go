// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
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

// memoryFlag is the value of -memory: a number of bytes, written as a
// number with the suffix K, M, G or T for a power of 1024, or without one.
type memoryFlag int64

func (f *memoryFlag) String() string { return strconv.FormatInt(int64(*f), 10) }

// Set parses s. It returns an error for a value that is not a number of at
// least 0 with an optional suffix, and for one beyond the largest int64.
func (f *memoryFlag) Set(s string) error {
	units := map[byte]int64{'K': 1 << 10, 'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}
	unit, digits := int64(1), s
	if n := len(s); n > 0 && units[s[n-1]] > 0 {
		unit, digits = units[s[n-1]], s[:n-1]
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || v < 0 || v > math.MaxInt64/unit {
		return fmt.Errorf("%q is not a number of bytes with an optional K, M, G or T for a power of 1024", s)
	}
	*f = memoryFlag(v * unit)
	return nil
}
