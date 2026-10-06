// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package mutate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// Check runs mutation testing on the package in the test's working
// directory, and fails tb with one line for each undetected mutant: a
// survivor, or a mutant whose site the tests never execute. Each line
// starts with the mutant's position, which go test does not precede with
// the position of the call to Check when the toolchain is Go 1.25 or later.
// When DOKIMI_MUTATE_RECORD_DIR is set, Check writes the run's record there.
//
// Check builds the package's test binary once, with every mutant behind a
// runtime switch, and runs it in a fresh process per mutant. That binary
// contains every test of the package, so the test that calls Check belongs
// in a file whose build constraint an ordinary go test run does not
// satisfy, such as //go:build mutation. Check skips tb when it runs inside a
// binary that Check started.
//
// Check fails tb with the run error when the package does not load, an
// annotation lacks a reason or suppresses nothing, the instrumented build
// fails, a test fails with no mutant active, or a run changes a file of the
// package. When the test's deadline leaves too little time for the next
// mutant and the closing control run, Check marks the remaining mutants
// not-run and fails tb. Run it with -timeout 0, or with a timeout longer
// than the run.
func Check(tb testing.TB, opts ...Option) {
	tb.Helper()
	if _, inside := os.LookupEnv(spec.Load().Protocol.Variable); inside {
		tb.Skip("mutate: Check does not run in a test binary that Check started")
		return
	}
	c := config{workers: 1}
	for _, o := range opts {
		if o.set != nil {
			c = o.set(c)
		}
	}
	var lines []run.Lines
	if spec := os.Getenv("DOKIMI_MUTATE_LINES"); spec != "" {
		for _, entry := range strings.Split(spec, ",") {
			l, err := run.ParseLines(entry)
			if err != nil {
				tb.Fatalf("mutate: DOKIMI_MUTATE_LINES: %v", err)
				return
			}
			lines = append(lines, l)
		}
	}
	var deadline time.Time
	if d, ok := tb.(interface{ Deadline() (time.Time, bool) }); ok {
		deadline, _ = d.Deadline()
	}
	rec, err := run.Run(
		context.Background(),
		run.Config{
			Dir: ".", Env: os.Environ(), Lines: lines, Suite: c.suite, Workers: c.workers, Deadline: deadline,
			Confirm: c.confirm,
		},
	)
	if err != nil {
		tb.Fatalf("mutate: %v", err)
		return
	}
	report(tb, rec)
	if dir := os.Getenv("DOKIMI_MUTATE_RECORD_DIR"); dir != "" {
		if _, err := rec.Write(dir); err != nil {
			tb.Errorf("mutate: %v", err)
		}
	}
}

// report fails tb with one line for each undetected mutant and each mutant
// whose run ended in an error, in source order. It then fails tb once for
// each run error, and once for the mutants that did not run, and logs the
// run's summary.
func report(tb testing.TB, rec *record.Record) {
	tb.Helper()
	notRun, reason := 0, ""
	for _, m := range rec.Mutants {
		switch m.Verdict {
		case record.Survived, record.NoCoverage, record.Error:
			fail(tb, m.Line(rec.Path(m.File, ".")))
		case record.NotRun:
			notRun, reason = notRun+1, m.Reason
		}
	}
	for _, e := range rec.Errors {
		tb.Errorf("mutate: %s: %s", e.Code, e.Message)
	}
	if notRun > 0 {
		tb.Errorf("mutate: %d mutants did not run: %s", notRun, reason)
	}
	tb.Log("mutate: " + rec.Summary(spec.Load().Protocol))
}

// Option configures one call of Check. The zero Option changes nothing.
type Option struct {
	set func(config) config
}

// config is the configuration of one call of Check.
type config struct {
	workers int
	suite   []string
	confirm bool
}

// Confirm makes Check confirm each survivor, and each mutant whose site the
// tests never execute, in the mutant's ordinary build. Check writes the
// mutant alone into the package's source, without a switch, builds the test
// binary from that source, and runs it once more without
// DOKIMI_MUTATE_INSTRUMENTED. A test that checks a property of the build,
// such as an allocation count, and skips while that variable is set, then
// runs against the mutant. The verdict of that run is the mutant's verdict,
// and a mutant whose site the tests never execute keeps the verdict not
// covered when the run passes.
//
// Each such mutant costs one more build and one more run of the tests.
// Before the mutants run, Check builds and runs the tests from the
// unchanged source once, and fails the test when they fail there.
func Confirm() Option {
	return Option{set: func(c config) config {
		c.confirm = true
		return c
	}}
}

// Suite adds the tests of other packages to the tests that count for the
// package's mutants. The patterns name the packages as go list resolves
// them in the package directory, such as ../conformance or ./... . Check
// builds the test binary of each such package whose tests link the
// package, with the package instrumented, and leaves out the others. A
// mutant runs each binary whose tests executed its site, the package's own
// first, until one fails. The record's suite names the other packages, and
// a test of one of them reads as the package's import path, a colon and the
// test's name.
func Suite(patterns ...string) Option {
	return Option{set: func(c config) config {
		c.suite = append(c.suite, patterns...)
		return c
	}}
}

// Workers sets the number of mutants that run concurrently, 1 by default.
//
// Each mutant runs in a process of its own, so n above 1 runs n copies of
// the package's tests concurrently. Tests that share a resource outside
// their temporary directory, such as a fixed port, then fail each other,
// and the failures count as kills. Every run, the control runs included,
// gets GOMAXPROCS divided by n, and at least 1. Workers panics for n below
// 1.
func Workers(n int) Option {
	if n < 1 {
		panic(fmt.Sprintf("mutate: Workers(%d) states fewer than one worker", n))
	}
	return Option{set: func(c config) config {
		c.workers = n
		return c
	}}
}
