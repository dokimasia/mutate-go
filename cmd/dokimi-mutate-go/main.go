// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"go.dokimi.dev/mutate/internal/memory"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// name is the command's name, dokimi-mutate and the language, which starts
// every line that the command writes to stderr.
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
	c := &cli{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		every:  progressEvery,
		now:    time.Now,
		budget: limit / quarters * budgetShare,
		limit:  limit,
	}
	return c.main(ctx, os.Args[1:])
}

// cli is one run of the command line. It has the standard streams of the
// run, its clock, the interval of its progress lines, the default memory
// budget of the process and the memory that the process may use.
type cli struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	now            func() time.Time
	every          time.Duration
	budget         int64
	limit          int64
}

// main runs the command line args and returns the exit status. It writes
// the help to stdout for -h and -help, and the versions for -version. A
// command line that is not valid gets one line that states the error, and
// a line on the help. Otherwise main changes to the directory of -C,
// resolves the selection, the suite, the packages of -skip and the
// packages, and tests or lists each package. To test more than one package
// at once, main first counts each package's mutants, and orders the
// packages and sizes their shares of the threads by the counts. The memory
// budget bounds the memory ceilings and the workers of each package's run,
// and without a budget the memory that the process may use bounds them.
// When a write to stdout fails, main states the first error on stderr and
// returns at least exitFailed, because the command did not write its
// results.
func (c *cli) main(ctx context.Context, args []string) (status int) {
	stdout := &lockedWriter{w: c.stdout}
	defer func() {
		if err := stdout.Err(); err != nil {
			fmt.Fprintf(c.stderr, "%s: standard output: %v\n", name, err)
			status = max(status, exitFailed)
		}
	}()
	o, err := parse(args, c.budget)
	if errors.Is(err, flag.ErrHelp) {
		help(stdout, map[string]string{flagBudget: budgetDefault(c.budget)})
		return exitDetected
	}
	if err != nil {
		fmt.Fprintf(c.stderr, "%s: %v\nRun '%s -%s' for usage.\n", name, err, name, flagHelp)
		return exitInvalid
	}
	if o.version {
		version(stdout)
		return exitDetected
	}
	out := &output{stdout: stdout, stderr: &lockedWriter{w: c.stderr}, json: o.json}
	if o.dir != "" {
		if err := os.Chdir(o.dir); err != nil {
			out.errorf("-%s: %v", flagDir, err)
			return exitInvalid
		}
	}
	var lines []selection.Lines
	for _, l := range o.lines {
		lines = append(lines, l.Abs())
	}
	if o.diff != "" {
		changed, err := c.readDiff(o.diff)
		if err != nil {
			out.errorf("-%s: %v", flagDiff, err)
			return exitInvalid
		}
		lines = append(append([]selection.Lines{}, lines...), changed...)
	}
	suitePaths, ok := importPaths(ctx, flagSuite, o.suite, out)
	if !ok {
		return exitInvalid
	}
	skipPaths, ok := importPaths(ctx, flagSkip, o.skip, out)
	if !ok {
		return exitInvalid
	}
	pkgs, ok := packages(ctx, o.patterns, out)
	if !ok {
		status = exitInvalid
	}
	var deadline time.Time
	if o.timeout > 0 {
		deadline = c.now().Add(o.timeout)
	}
	s := &session{
		cfg: run.Config{
			Env:              os.Environ(),
			Lines:            lines,
			Suite:            suitePaths,
			Workers:          o.workers,
			Deadline:         deadline,
			Sample:           o.sample,
			IncludeGenerated: o.includeGenerated,
			Confirm:          o.confirm,
			Memory:           c.limit,
		},
		out:      out,
		progress: &progress{stderr: out.stderr, every: c.every, now: c.now, deadline: deadline},
		records:  o.records,
		skip:     skipPaths,
	}
	threads := runtime.GOMAXPROCS(0)
	if o.list {
		return max(status, schedule(ctx, pkgs, nil, threads, o.parallel, deadline, out, s.list))
	}
	if o.budget > 0 {
		s.cfg.Memory = int64(o.budget)
		s.cfg.Admit = newBudget(int64(o.budget)).admit
	}
	stop := s.progress.watch()
	defer stop()
	// The counts order the packages only when two or more of them may run at
	// once.
	var weights []int
	if min(o.parallel, len(pkgs)) > 1 {
		weights = s.weigh(ctx, pkgs, threads, o.parallel)
	}
	return max(status, schedule(ctx, pkgs, weights, threads, o.parallel, deadline, out, s.check))
}

// readDiff returns the selection of the unified diff in the file path, or
// on stdin for stdinPath, as selection.ParseDiff states it. A diff that
// selects no line returns an empty selection, which selects none.
func (c *cli) readDiff(path string) ([]selection.Lines, error) {
	var data []byte
	var err error
	if path == stdinPath {
		data, err = io.ReadAll(c.stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // main adds the flag to the error of the read
	}
	return selection.ParseDiff(string(data))
}
