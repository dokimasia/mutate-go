// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

func main() {
	os.Exit(command())
}

// name is the command's name, dokimi-mutate and the language, which starts
// every line that the command writes to stderr.
const name = "dokimi-mutate-go"

// progressEvery is the interval between two progress lines of a package
// that runs.
const progressEvery = 10 * time.Second

// command runs the command line of the process and returns its exit
// status. SIGINT and SIGTERM cancel the runs. main exits with the status
// after the deferred calls of command have returned.
func command() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	p := &printer{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, every: progressEvery, now: time.Now}
	return p.mutate(ctx, os.Args[1:])
}

// suiteFlag collects the patterns of the flag -suite, which repeats.
type suiteFlag []string

func (*suiteFlag) String() string { return "" }

func (f *suiteFlag) Set(pattern string) error {
	*f = append(*f, pattern)
	return nil
}

// linesFlag collects the entries of the flag -lines, which repeats.
type linesFlag []run.Lines

func (*linesFlag) String() string { return "" }

func (f *linesFlag) Set(entry string) error {
	l, err := run.ParseLines(entry)
	if err != nil {
		return err
	}
	*f = append(*f, l)
	return nil
}

// listed is the part of a go list entry that the command reads. Error is
// the error of the entry's package or pattern, and DepsErrors lists the
// errors of the package's dependencies.
type listed struct {
	Dir        string
	ImportPath string
	Error      *struct{ Err string }
	DepsErrors []*struct{ Err string }
}

// options are the settings of every package's check that the command line
// states.
type options struct {
	cfg     run.Config
	records string
	json    bool
}

// printer writes the output of the runs and keeps the highest exit status.
// With the text output, it writes the line of each undetected mutant, and
// of each mutant whose run ended in an error, when the mutant's verdict is
// final, and the summary of a package when the package's run ends. It
// writes a progress line for each package that runs at every interval
// every, measured by now. stdin is the input of -diff -.
type printer struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	every          time.Duration
	now            func() time.Time
	mu             sync.Mutex
	status         int
	// deadline is when -timeout ends the runs, or zero without -timeout.
	deadline time.Time
	// running maps the import path of each package that runs to its
	// progress.
	running map[string]*progress
}

// progress counts the mutants of a package's run: those with a final
// verdict other than not-run, the undetected ones among them, the ones
// whose run ended, the ones that will not run, and all, which are 0 until
// the first verdict. started is when the run of the first mutant that ended
// started.
type progress struct {
	done, undetected, ran, stopped, total int
	started                               time.Time
}

func (p *printer) print(w io.Writer, format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(w, format+"\n", args...)
}

func (p *printer) raise(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = max(p.status, status)
}

// mutate runs the command line args and returns the exit status. It writes
// the lines of the undetected mutants and the summary of each package, or
// each package's record under -json, to stdout. It writes the errors and
// the progress lines to stderr.
func (p *printer) mutate(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(p.stderr)
	parallel := flags.Int("p", 1, "check `n` packages at once")
	workers := flags.Int("workers", 1, "run `n` mutants of one package at once")
	records := flags.String("record", "", "write the record of each package to `dir`")
	asJSON := flags.Bool(
		"json",
		false,
		"write each package's record to stdout as one line of JSON, in place of the text",
	)
	timeout := flags.Duration(
		"timeout",
		0,
		"start no package after `d`, and no mutant that would not end before then; 0 for no limit",
	)
	sample := flags.Int(
		"sample",
		0,
		"start the runs of the first `n` mutants of each package in key order, and of no other; 0 for every mutant",
	)
	memory := memoryFlag(memoryLimit("/proc", "/sys") / 4 * 3)
	flags.Var(
		&memory,
		"memory",
		"admit the runs of a package after its opening control run while the memory ceilings of every package's runs "+
			"fit in `bytes`, with K, M, G or T for a power of 1024; 0 for no limit, by default three quarters of the "+
			"memory that the process may use",
	)
	confirm := flags.Bool(
		"confirm",
		false,
		"run each survivor, and each mutant that is not covered, once more in an ordinary build of that mutant alone, "+
			"and take the verdict of that run",
	)
	var lines linesFlag
	flags.Var(&lines, "lines", "restrict the run to the lines `file:first-last`; repeats")
	diff := flags.String(
		"diff",
		"",
		"restrict the run to the lines that the unified diff in `file` adds, and the lines beside the lines it removes, "+
			"with the paths after b/ relative to the working directory; - reads standard input",
	)
	var suite suiteFlag
	flags.Var(
		&suite,
		"suite",
		"count the tests of the packages that `pattern` names, where they link a package; repeats",
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *parallel < 1 || *workers < 1 {
		fmt.Fprintln(p.stderr, name+": -p and -workers take a number of at least 1")
		return 2
	}
	if *timeout < 0 {
		fmt.Fprintln(p.stderr, name+": -timeout takes a duration of at least 0")
		return 2
	}
	if *sample < 0 {
		fmt.Fprintln(p.stderr, name+": -sample takes a number of at least 0")
		return 2
	}
	if *diff != "" {
		changed, err := p.readDiff(*diff)
		if err != nil {
			fmt.Fprintf(p.stderr, name+": -diff: %v\n", err)
			return 2
		}
		lines = append(append(linesFlag{}, lines...), changed...)
	}
	var deadline time.Time
	if *timeout > 0 {
		deadline = p.now().Add(*timeout)
	}
	patterns := flags.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	suitePaths, ok := p.resolveSuite(ctx, suite)
	if !ok {
		return 2
	}
	pkgs, err := list(ctx, patterns)
	if err != nil {
		fmt.Fprintf(p.stderr, name+": %v\n", err)
		return 2
	}
	opts := options{
		cfg: run.Config{
			Env:      os.Environ(),
			Lines:    lines,
			Suite:    suitePaths,
			Workers:  *workers,
			Procs:    max(1, runtime.GOMAXPROCS(0) / *parallel),
			Deadline: deadline,
			Sample:   *sample,
			Confirm:  *confirm,
		},
		records: *records,
		json:    *asJSON,
	}
	if memory > 0 {
		opts.cfg.Admit = newBudget(int64(memory)).admit
	}
	p.running = map[string]*progress{}
	p.deadline = deadline
	stop := p.watch()
	slots := make(chan struct{}, *parallel)
	var wg sync.WaitGroup
	for _, pkg := range pkgs {
		if pkg.Error != nil {
			p.print(p.stderr, name+": %s: %s", pkg.ImportPath, pkg.Error.Err)
			p.raise(2)
			continue
		}
		slots <- struct{}{}
		why := ""
		if ctx.Err() != nil {
			why = "the command was interrupted"
		} else if !deadline.IsZero() && time.Now().After(deadline) {
			why = "-timeout passed before the package started"
		}
		if why != "" {
			<-slots
			p.print(p.stderr, name+": %s: not checked, because %s", pkg.ImportPath, why)
			p.raise(2)
			continue
		}
		pkg := pkg
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.raise(p.check(ctx, pkg, opts))
			<-slots
		}()
	}
	wg.Wait()
	stop()
	return p.status
}

// readDiff returns the selection of the unified diff in the file path, or
// on stdin for the path -, as run.ParseDiff states it. A diff that selects
// no line returns an empty selection, which selects none.
func (p *printer) readDiff(path string) ([]run.Lines, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(p.stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return run.ParseDiff(string(data))
}

// resolveSuite resolves the patterns of -suite in the command's working
// directory, as the command resolves the packages, and returns the import
// paths of the packages that they name. Each run resolves its suite in its
// package's directory, where an import path names the same package as in
// the working directory, whatever symbolic links lead to either. A pattern
// that does not resolve, and a package that go list states an error of,
// fail the command: resolveSuite writes each such error and reports false.
func (p *printer) resolveSuite(ctx context.Context, patterns []string) ([]string, bool) {
	if len(patterns) == 0 {
		return nil, true
	}
	entries, err := list(ctx, patterns)
	if err != nil {
		p.print(p.stderr, name+": -suite: %v", err)
		return nil, false
	}
	var paths []string
	for _, e := range entries {
		problem := e.Error
		if problem == nil && len(e.DepsErrors) > 0 {
			problem = e.DepsErrors[0]
		}
		if problem != nil {
			p.print(p.stderr, name+": -suite: %s: %s", e.ImportPath, strings.TrimSpace(problem.Err))
			continue
		}
		paths = append(paths, e.ImportPath)
	}
	return paths, len(paths) == len(entries)
}

// list returns the packages that the patterns name, as go list reports
// them.
func list(ctx context.Context, patterns []string) ([]listed, error) {
	out, err := load.Go(
		ctx,
		".",
		os.Environ(),
		append([]string{"list", "-e", "-json=Dir,ImportPath,Error,DepsErrors"}, patterns...)...)
	if err != nil {
		return nil, err
	}
	var pkgs []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg listed
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			return pkgs, nil
		} else if err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		pkgs = append(pkgs, pkg)
	}
}

// check runs mutation testing on pkg and returns the exit status that its
// run gives: 2 for a run that fails or whose record does not write, 1 for
// a run with an undetected mutant, and 0 otherwise.
func (p *printer) check(ctx context.Context, pkg listed, opts options) int {
	cfg := opts.cfg
	cfg.Dir = pkg.Dir
	cfg.Verdict = func(r *record.Record, m record.Mutant) { p.verdict(pkg.ImportPath, r, m, !opts.json) }
	p.begin(pkg.ImportPath)
	rec, err := run.Run(ctx, cfg)
	p.end(pkg.ImportPath)
	if err != nil {
		p.print(p.stderr, name+": %s: %v", pkg.ImportPath, err)
		return 2
	}
	p.report(rec, opts.json)
	status := 0
	for _, m := range rec.Mutants {
		if m.Verdict == record.Survived || m.Verdict == record.NoCoverage {
			status = 1
		}
	}
	if opts.records != "" {
		if _, err := rec.Write(opts.records); err != nil {
			p.print(p.stderr, name+": %v", err)
			return 2
		}
	}
	if rec.Failed() {
		return 2
	}
	return status
}

// report writes the output of one run that ended. To stderr it writes the
// run errors and the number of mutants that did not run. To stdout it
// writes the record as one line of JSON when asJSON is set, and otherwise
// the summary, after the lines that verdict wrote.
func (p *printer) report(rec *record.Record, asJSON bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range rec.Errors {
		fmt.Fprintf(p.stderr, name+": %s: %s: %s\n", rec.Target.Name, e.Code, e.Message)
	}
	notRun, reason := 0, ""
	for _, m := range rec.Mutants {
		if m.Verdict == record.NotRun {
			notRun, reason = notRun+1, m.Reason
		}
	}
	if notRun > 0 {
		fmt.Fprintf(p.stderr, name+": %s: %d mutants did not run: %s\n", rec.Target.Name, notRun, reason)
	}
	if asJSON {
		// A record contains strings, integers and finite numbers, which
		// encoding/json encodes without an error.
		data, _ := json.Marshal(rec)
		fmt.Fprintf(p.stdout, "%s\n", data)
		return
	}
	fmt.Fprintln(p.stdout, rec.Summary(spec.Load().Protocol))
}

// begin, verdict and end keep the progress of the package importPath:
// begin when its run starts, verdict at each final verdict, and end when
// its run ends.
func (p *printer) begin(importPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running[importPath] = &progress{}
}

// verdict counts m, a mutant of rec whose verdict is final, in the
// progress of the package importPath: as done, or as stopped when m is
// not-run. When lines is set and m is undetected, or its run ended in an
// error, verdict writes m's line to stdout at once, so the lines follow the
// order of the verdicts. A mutant with Seconds ran, and the first such
// mutant marks when the runs started.
func (p *printer) verdict(importPath string, rec *record.Record, m record.Mutant, lines bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.running[importPath]
	if m.Verdict == record.NotRun {
		s.stopped++
	} else {
		s.done++
	}
	s.total = len(rec.Mutants)
	if m.Seconds != nil {
		if s.ran == 0 {
			s.started = p.now().Add(-time.Duration(*m.Seconds * float64(time.Second)))
		}
		s.ran++
	}
	undetected := m.Verdict == record.Survived || m.Verdict == record.NoCoverage
	if undetected {
		s.undetected++
	}
	if lines && (undetected || m.Verdict == record.Error) {
		fmt.Fprintln(p.stdout, m.Line(rec.Path(m.File, ".")))
	}
}

func (p *printer) end(importPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, importPath)
}

// watch writes the progress of every package that runs at each interval
// p.every, until the function that it returns is called.
func (p *printer) watch() (stop func()) {
	ticker := time.NewTicker(p.every)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				p.tick()
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		<-stopped
	}
}

// tick writes one progress line for each package that runs, in import path
// order. The line states that the package runs while none of its mutants
// has a verdict. Then it states how many are done, and how many of those
// are undetected. Once a mutant's run ended, it states the rate of the runs
// and the time left at that rate. Once a mutant is not-run, no further
// mutant starts, and the line states how many will not run and that the
// run waits for the mutants that still run and the closing control run.
// Under -timeout, the line ends with the time until the timeout.
func (p *printer) tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	pkgs := make([]string, 0, len(p.running))
	for pkg := range p.running {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		s := p.running[pkg]
		line := "running"
		if s.total > 0 {
			line = fmt.Sprintf("%d of %d mutants done, %d undetected", s.done, s.total, s.undetected)
		}
		switch running := s.total - s.done - s.stopped; {
		case s.stopped > 0:
			line += fmt.Sprintf(", %d not run, waiting for ", s.stopped)
			if running == 1 {
				line += "1 mutant run and "
			} else if running > 1 {
				line += fmt.Sprintf("%d mutant runs and ", running)
			}
			line += "the closing control run"
		case s.ran > 0:
			rate := float64(s.ran) / now.Sub(s.started).Seconds()
			left := time.Duration(float64(s.total-s.done) / rate * float64(time.Second))
			line += fmt.Sprintf(", %.1f mutants a second, about %s left", rate, left.Round(time.Second))
		}
		if left := p.deadline.Sub(now); !p.deadline.IsZero() && left > 0 {
			line += fmt.Sprintf(", -timeout in %s", left.Round(time.Second))
		} else if !p.deadline.IsZero() {
			line += ", -timeout passed"
		}
		fmt.Fprintf(p.stderr, name+": %s: %s\n", pkg, line)
	}
}
