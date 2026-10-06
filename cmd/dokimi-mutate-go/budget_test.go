// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestBudget(t *testing.T) {
	t.Parallel()
	t.Run("admit", func(t *testing.T) {
		t.Parallel()
		t.Run("admits runs that fit beside the admitted runs at once", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, ok1 := b.admit(context.Background(), 6)
			second, ok2 := b.admit(context.Background(), 4)
			if !ok1 || !ok2 {
				t.Fatalf("admit() = %v, %v, want both admitted", ok1, ok2)
			}
			first()
			second()
		})
		t.Run("admits runs that need more than the budget when no runs are admitted", func(t *testing.T) {
			t.Parallel()
			release, ok := newBudget(10).admit(context.Background(), 20)
			if !ok {
				t.Fatal("admit() = false, want the runs admitted alone")
			}
			release()
		})
		t.Run("waits until a release leaves room", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, _ := b.admit(context.Background(), 8)
			admitted := make(chan func())
			go func() {
				release, _ := b.admit(context.Background(), 8)
				admitted <- release
			}()
			select {
			case <-admitted:
				t.Fatal("admit() returned while the first runs used the budget")
			case <-time.After(50 * time.Millisecond):
			}
			first()
			(<-admitted)()
		})
		t.Run("returns false when the context ends before room is left", func(t *testing.T) {
			t.Parallel()
			b := newBudget(10)
			first, _ := b.admit(context.Background(), 8)
			defer first()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if release, ok := b.admit(ctx, 8); ok || release != nil {
				t.Errorf("admit() = %v, %v, want nil and false", release != nil, ok)
			}
		})
	})
	t.Run("memoryFlag", func(t *testing.T) {
		t.Parallel()
		t.Run("Set", func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				give string
				want int64
			}{
				{"0", 0},
				{"512", 512},
				{"3K", 3 << 10},
				{"2M", 2 << 20},
				{"12G", 12 << 30},
				{"1T", 1 << 40},
			}
			for _, tt := range tests {
				var f memoryFlag
				err := f.Set(tt.give)
				if err != nil || int64(f) != tt.want || f.String() != strconv.FormatInt(tt.want, 10) {
					t.Errorf("Set(%q) = %d, %v, String() = %s, want %d", tt.give, f, err, f.String(), tt.want)
				}
			}
		})
		t.Run("Set returns an error for a value that is not a number of bytes", func(t *testing.T) {
			t.Parallel()
			for _, give := range []string{"", "G", "-1", "1.5G", "8E", "9000000000T"} {
				var f memoryFlag
				if err := f.Set(give); err == nil {
					t.Errorf("Set(%q) = nil, want an error", give)
				}
			}
		})
	})
}
