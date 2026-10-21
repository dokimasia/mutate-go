// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	gocli "go.stealthscale.io/cli"

	"go.dokimi.dev/mutate/internal/memory"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// name is the command's name, dokimi-mutate and the language, which starts
// every line that the command writes to stderr and every environment
// variable of its options.
const name = "dokimi-mutate-go"

// progressEvery is the interval between two progress lines of a package
// that runs.
const progressEvery = 10 * time.Second

// The default memory budget is budgetShare quarters of the memory that the
// process may use.
const (
	budgetShare = 3
	quarters    = 4
)

// rootDir is the root of the file system, under which the process reads
// the memory that it may use.
const rootDir = "/"

// definition is the vendored definition, and includeDirective the line that
// makes a generated file a target.
var (
	definition       = spec.Load()
	includeDirective = definition.Overlay.Comment + definition.Catalogue.Include
)

func main() {
	os.Exit(command())
}

// command runs the command line of the process and returns its exit
// status. SIGINT and SIGTERM cancel the runs. main exits with the status
// after the deferred calls of command have returned.
func command() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	limit := memory.Limit(os.DirFS(rootDir))
	budget := limit / quarters * budgetShare
	c := &cli{every: progressEvery, now: time.Now, budget: budget, limit: limit, note: budgetDefault(budget)}
	return c.main(ctx, gocli.OS())
}

// cli is one run of the command line. It has the clock of the run, the
// interval of its progress lines, the default memory budget of the process,
// the memory that the process may use and the exit status that the run
// sets.
type cli struct {
	now func() time.Time
	// note is the sentence of the help on the default memory budget, or
	// empty for a help that does not depend on the process.
	note   string
	every  time.Duration
	budget int64
	limit  int64
	status int
}

// main runs the command line of env and returns the exit status. env's
// Environ, Stdout and Stderr are required. The help goes to the standard
// output for -h and --help, and the versions for --version. main writes
// the failure of a command line that is not valid, and returns exitInvalid
// for it. It returns exitFailed when the help or the versions do not write.
// Otherwise the handler of the app runs the command line and sets the
// status, as handle states.
func (c *cli) main(ctx context.Context, env *gocli.Env) int {
	app := c.app()
	renderer, err := app.ExecuteRenderer(ctx, env)
	f := app.Failure(err)
	if f == nil {
		return c.status
	}
	// The status states the failure also when the failure does not write.
	_ = renderer.Error(env.Stderr, env.Environ(), f)
	if f.Usage {
		return exitInvalid
	}
	return exitFailed
}

// app returns the app of the command line with its options, its versions
// and the handler handle. The help text has the note of c, and the app
// converts each entry of --lines with selection.ParseEntry.
func (c *cli) app() *gocli.App[options] {
	app := gocli.New[options](name, helpText(c.note))
	app.Convert(selection.ParseEntry)
	app.Version(versions())
	app.Handle(c.handle)
	return app
}

// handle runs the command line of r, and sets the exit status of the run
// that test returns.
func (c *cli) handle(ctx context.Context, r *gocli.Run[options, options]) error {
	c.status = c.test(ctx, r)
	return nil
}

// test runs the options of r and returns the exit status. An option that
// the command does not run with gets one line that states the problem.
// Otherwise test changes to the directory of --directory, resolves the
// selection, the suite, the packages of --skip and the packages, and tests
// or lists each package. To test more than one package at once, test first
// counts each package's mutants, and orders the packages and sizes their
// shares of the threads by the counts. The memory budget bounds the memory
// ceilings and the workers of each package's run, and without a budget the
// memory that the process may use bounds them. Every go command and every
// test binary runs with the environment of r. When a write to the standard
// output fails, test states the first error on the standard error and
// returns at least exitFailed, because the command did not write its
// results.
func (c *cli) test(ctx context.Context, r *gocli.Run[options, options]) (status int) {
	o := r.Config()
	stdout := &lockedWriter{w: r.Stdout()}
	defer func() {
		if err := stdout.Err(); err != nil {
			fmt.Fprintf(r.Stderr(), "%s: standard output: %v\n", name, err)
			status = max(status, exitFailed)
		}
	}()
	out := &output{stdout: stdout, stderr: &lockedWriter{w: r.Stderr()}, json: o.JSON}
	if problem := o.problem(r.Source); problem != "" {
		out.errorf("%s", problem)
		return exitInvalid
	}
	if o.Directory != "" {
		if err := os.Chdir(o.Directory); err != nil {
			out.errorf("%s%s: %v", flagPrefix, flagDirectory, err)
			return exitInvalid
		}
	}
	var lines []selection.Lines
	for _, l := range o.Lines {
		lines = append(lines, l.Abs())
	}
	if o.Diff != "" {
		changed, err := readDiff(r, o.Diff)
		if err != nil {
			out.errorf("%s%s: %v", flagPrefix, flagDiff, err)
			return exitInvalid
		}
		lines = append(append([]selection.Lines{}, lines...), changed...)
	}
	env := r.Environ()
	suitePaths, ok := importPaths(ctx, env, flagSuite, o.Suite, out)
	if !ok {
		return exitInvalid
	}
	skipPaths, ok := importPaths(ctx, env, flagSkip, o.Skip, out)
	if !ok {
		return exitInvalid
	}
	pkgs, ok := packages(ctx, env, o.Packages, out)
	if !ok {
		status = exitInvalid
	}
	var deadline time.Time
	if o.Timeout > 0 {
		deadline = c.now().Add(o.Timeout)
	}
	s := &session{
		cfg: run.Config{
			Env:              env,
			Lines:            lines,
			Suite:            suitePaths,
			Workers:          o.Workers,
			Deadline:         deadline,
			Sample:           o.Sample,
			IncludeGenerated: o.IncludeGenerated,
			Confirm:          o.Confirm,
		},
		out:      out,
		progress: &progress{stderr: out.stderr, every: c.every, now: c.now, deadline: deadline},
		records:  o.Record,
		skip:     skipPaths,
	}
	threads := runtime.GOMAXPROCS(0)
	if o.List {
		return max(status, schedule(ctx, pkgs, nil, threads, o.Parallel, deadline, out, s.list))
	}
	s.cfg.Memory, s.cfg.Admit = c.admission(o)
	stop := s.progress.watch()
	defer stop()
	// The counts order the packages only when two or more of them may run at
	// once.
	var weights []int
	if min(o.Parallel, len(pkgs)) > 1 {
		weights = s.weigh(ctx, pkgs, threads, o.Parallel)
	}
	return max(status, schedule(ctx, pkgs, weights, threads, o.Parallel, deadline, out, s.check))
}

// admission returns the memory that bounds the memory ceilings and the
// workers of each package's run under o, and the admission of the runs. The
// budget is the size of --memory-budget, or the default budget of the
// process without the flag. A budget above 0 bounds the ceilings and the
// workers. The runs of each package then wait until they fit in the budget
// beside the runs of the other packages. Without a budget the memory that
// the process may use bounds them, and the admission is nil.
func (c *cli) admission(o *options) (int64, func(context.Context, int64) (func(), bool)) {
	budget := c.budget
	if o.MemoryBudget != nil {
		budget = int64(*o.MemoryBudget)
	}
	if budget > 0 {
		return budget, newBudget(budget).admit
	}
	return c.limit, nil
}

// readDiff returns the selection of the unified diff in the input file f of
// r, which is the standard input for stdinPath, as selection.ParseDiff
// states it. A diff that selects no line returns an empty selection, which
// selects none.
func readDiff(r *gocli.Run[options, options], f gocli.InputFile) (lines []selection.Lines, err error) {
	in, err := r.Open(f)
	if err != nil {
		return nil, err //nolint:wrapcheck // test adds the flag to the error of the open
	}
	defer func() { err = errors.Join(err, in.Close()) }()
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, err //nolint:wrapcheck // test adds the flag to the error of the read
	}
	return selection.ParseDiff(string(data))
}
