// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// Count returns the number of mutants that Run would test in the package
// in cfg.Dir: each mutant that cfg.Lines selects and that no rule or
// annotation suppresses, and at most cfg.Sample of them. Count loads and
// enumerates the package as Run does, with the environment of Run's go
// commands, and renders, builds and runs nothing, so it counts a mutant that
// the type checker would reject too. A package that does not load, and a
// package with an annotation that is not valid, count 0, because Run stops
// before it tests a mutant of them.
func Count(ctx context.Context, cfg Config) int {
	r := &runner{cfg: cfg, procs: threads(cfg), def: spec.Load(), rec: &record.Record{}}
	result, err := r.loadMutants(ctx)
	if err != nil || len(result.Problems) > 0 {
		return 0
	}
	n := 0
	for _, m := range result.Mutants {
		if m.Status == enumerate.Runnable {
			n++
		}
	}
	if cfg.Sample > 0 {
		return min(n, cfg.Sample)
	}
	return n
}
