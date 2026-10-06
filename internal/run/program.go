// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"strconv"
	"time"

	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/testbin"
)

// procsVar is the variable of the environment that sets the threads of a
// Go program.
const procsVar = "GOMAXPROCS"

// instrumentedValue is the value of the protocol's instrumented variable in
// every run of the instrumented test binary.
const instrumentedValue = "1"

// ownPattern is the pattern that names the run's own package to the go
// command in the package's directory.
const ownPattern = "."

// program is one test binary of the run's suite.
type program struct {
	// target is the import path of the package whose tests the binary
	// runs, or empty for the tests of the run's own package.
	target string
	// dir is the directory that the binary runs in: its package's.
	dir     string
	bin     string
	buildID string
	// executed contains the first ordinal of each site that the binary's
	// opening control run executed.
	executed map[int]bool
	// deadline and ceiling are the limits of each run of the binary, from
	// its opening control run. ceiling is 0 where the engine applies none.
	deadline time.Duration
	ceiling  int64
	// tests lists the binary's top-level tests in the order in which its
	// opening control run started them. sites contains, for each of them,
	// the first ordinal of each site that the test executed when it ran
	// alone, and seconds the wall time of that run. Both are nil when the
	// engine did not run every test alone.
	tests   []string
	sites   map[string]map[int]bool
	seconds map[string]float64
}

// part is one run of a test binary within a mutant's run: the flags that
// select its tests, nil for the whole suite, and the deadline of the part
// alone, or 0 where only the binary's deadline applies.
type part struct {
	filter   []string
	deadline time.Duration
}

// pattern returns the pattern that names p's package to go test in the run's
// package directory: ownPattern for the run's own package, and the import
// path for another.
func (p *program) pattern() string {
	if p.target == "" {
		return ownPattern
	}
	return p.target
}

// of returns the words that name the package target in a message: empty
// for the run's own package, and " of" and the import path otherwise.
func of(target string) string {
	if target == "" {
		return ""
	}
	return " of " + target
}

// named returns tests, the names of tests of the package target, as a
// record states them: as they are for the run's own package, where target
// is empty, and after the import path and a colon for another package.
func named(tests []string, target string) []string {
	if target == "" {
		return tests
	}
	out := make([]string, len(tests))
	for i, t := range tests {
		out[i] = target + ": " + t
	}
	return out
}

// env returns the environment of a run of the instrumented test binary with
// the mutant ordinal active, 0 for none, and the trace file trace, empty for
// none. The run's threads are divided among the workers.
func (r *runner) env(ordinal int, trace string) []string {
	return testbin.Setenv(
		r.cfg.Env,
		r.def.Protocol.Variable+"="+strconv.Itoa(ordinal),
		r.def.Protocol.Instrumented+"="+instrumentedValue,
		render.TraceVar+"="+trace,
		procsVar+"="+strconv.Itoa(max(1, r.procs/max(1, r.cfg.Workers))),
	)
}

// ordinaryEnv returns the environment of a run of an ordinary build, the
// ordinary control run for the ordinal 0 and a confirmation run for a
// mutant's ordinal: the environment of env without the instrumented
// variable and the trace.
func (r *runner) ordinaryEnv(ordinal int) []string {
	return testbin.Setenv(r.env(ordinal, ""), r.def.Protocol.Instrumented, render.TraceVar)
}

// execute runs the test binary bin of the program p with env and args,
// under the deadline timeout, the backup delay backup and p's memory
// ceiling, in a temporary directory under the run's work directory. Under
// stopAtFailure, the run ends at its first failed test.
func (r *runner) execute(
	ctx context.Context,
	p *program,
	bin string,
	env, args []string,
	timeout, backup time.Duration,
	stopAtFailure bool,
) *testbin.Result {
	return testbin.Run(ctx, testbin.Config{
		Binary: bin, Dir: p.dir, Work: r.work, Env: env, Args: args, Timeout: timeout, Backup: backup,
		Ceiling: p.ceiling, StopAtFailure: stopAtFailure,
	})
}
