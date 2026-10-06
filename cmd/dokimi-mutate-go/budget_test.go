// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"

	"go.dokimi.dev/assert"
)

// The time within which an admission that must not wait returns, and the
// time that a case waits to see that an admission waits.
const (
	admitWait = 10 * time.Second
	stillWait = 50 * time.Millisecond
)

func TestBudget(t *testing.T) {
	t.Parallel()

	t.Run("admit", func(t *testing.T) {
		t.Parallel()

		t.Run("admits runs that fit beside the admitted runs at once", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, ok := b.admit(t.Context(), 6)
			assert.True(t, ok, "the first runs fit")
			second, ok := b.admit(t.Context(), 4)
			assert.True(t, ok, "the second runs fit beside them")
			first()
			second()
		})

		t.Run("admits runs that need more than the budget when no runs are admitted", func(t *testing.T) {
			t.Parallel()
			release, ok := newBudget(10).admit(t.Context(), 20)
			assert.True(t, ok, "the runs start alone")
			release()
		})

		t.Run("waits until a release leaves room", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, _ := b.admit(t.Context(), 8)
			admitted := make(chan func(), 1)
			go func() {
				release, _ := b.admit(context.Background(), 8)
				admitted <- release
			}()
			time.Sleep(stillWait)
			assert.Length(t, admitted, 0, "the second runs wait while the first use the budget")
			first()
			assert.CompletesWithin(t, admitWait, func(context.Context) error {
				(<-admitted)()
				return nil
			}, "the release admits the waiting runs")
		})

		t.Run("returns false when the context ends before room is left", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, _ := b.admit(t.Context(), 8)
			defer first()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			release, ok := b.admit(ctx, 8)
			assert.False(t, ok, "the cancelled caller is not admitted")
			assert.Nil(t, release, "and gets no release")
		})
	})
}
