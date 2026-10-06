// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package mutate

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/report"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// Check runs mutation testing on the package in the test's working
// directory, and fails tb with one line for each undetected mutant: a
// survivor, or a mutant whose site the tests never execute. Each line
// starts with the mutant's position, which go test does not precede with
// the position of the call to Check. When DOKIMI_MUTATE_RECORD_DIR is set,
// Check writes the run's record there.
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
	lines, err := selection.ParseList(os.Getenv(selection.Var))
	if err != nil {
		tb.Fatalf("mutate: %s: %v", selection.Var, err)
		return
	}
	var deadline time.Time
	if d, ok := tb.(interface{ Deadline() (time.Time, bool) }); ok {
		deadline, _ = d.Deadline()
	}
	rec, err := run.Run(
		context.Background(),
		run.Config{
			Dir: ".", Env: os.Environ(), Lines: lines, Suite: c.suite, Workers: c.workers, Deadline: deadline,
			Confirm: c.confirm, IncludeGenerated: c.includeGenerated,
		},
	)
	if err != nil {
		tb.Fatalf("mutate: %v", err)
		return
	}
	reportTo(tb, rec)
	if dir := os.Getenv("DOKIMI_MUTATE_RECORD_DIR"); dir != "" {
		if _, err := rec.Write(dir); err != nil {
			tb.Errorf("mutate: %v", err)
		}
	}
}

// reportTo fails tb with the line of each mutant that a report lists, in
// source order: an undetected mutant, or a mutant whose run ended in an
// error. It then fails tb once for each note on the run, the run errors and
// the mutants that did not run, and logs the run's summary.
func reportTo(tb testing.TB, rec *record.Record) {
	tb.Helper()
	for _, m := range rec.Mutants {
		if report.Listed(m.Verdict) {
			fail(tb, report.Line(m, rec.Path(m.File, ".")))
		}
	}
	for _, note := range report.Notes(rec) {
		tb.Errorf("mutate: %s", note)
	}
	tb.Log("mutate: " + report.Summary(rec, spec.Load().Protocol))
}

// fail writes line to the test's output without the source position that
// go test puts before a logged line, and marks the test failed. An editor
// then links the position at the start of line.
func fail(tb testing.TB, line string) {
	tb.Helper()
	fmt.Fprintln(tb.Output(), line)
	tb.Fail()
}

// Option configures one call of Check. The zero Option changes nothing.
type Option struct {
	set func(config) config
}

// config is the configuration of one call of Check.
type config struct {
	workers          int
	suite            []string
	confirm          bool
	includeGenerated bool
}

// IncludeGenerated makes Check mutate every generated file of the package,
// as if the comments before its package clause contained the line
// //dokimi:mutate-include. The record lists each such file as included, and
// the score counts its mutants.
func IncludeGenerated() Option {
	return Option{set: func(c config) config {
		c.includeGenerated = true
		return c
	}}
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
