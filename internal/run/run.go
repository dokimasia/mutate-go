// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// Engine is the record's name of this engine.
const Engine = "mutate-go"

// cancelled is the reason of every mutant that does not run because the
// caller's context is done.
const cancelled = "the caller cancelled the run"

// workPattern is the pattern of the name of a run's work directory, which
// contains the instrumented source, the test binaries and the traces.
const workPattern = "mutate-"

// workingDir is the process's working directory, the directory against
// which cfg.Verdict receives the path of each mutant's file.
const workingDir = "."

// Config states one run of one package.
type Config struct {
	// Dir is the package directory.
	Dir string
	// Env is the environment of every go command and every run of a test
	// binary, as os.Environ returns it.
	Env []string
	// Lines is the run's selection. A nil selection selects every line, and
	// an empty one selects none. The record states the ranges in the
	// package's files.
	Lines []selection.Lines
	// Suite lists patterns, as go list resolves them in Dir, of the other
	// packages whose tests count for the package's mutants. Run builds the
	// test binary of each such package whose tests link the package, with
	// the package instrumented, and leaves out the others and the package
	// itself.
	Suite []string
	// Workers is the number of mutants that run at once. Below 2, one
	// mutant runs at a time.
	Workers int
	// Procs is the number of threads that the run may use, or 0 for the
	// engine's GOMAXPROCS. Every go command runs with GOMAXPROCS set to
	// Procs, and every run of a test binary with Procs divided by Workers,
	// and at least 1.
	Procs int
	// Spare, when it is not nil, returns the threads that the run's go
	// commands may use beside Procs, such as threads that the caller's other
	// runs leave free. A go command reads it when it starts, and adds the
	// threads to Procs before the division among the workers. A run of a
	// test binary does not read it, so no verdict depends on it.
	Spare func() int
	// Deadline is when the caller's time ends, or zero for a caller
	// without a deadline. The opening control run of each test binary gets
	// at most half of the time left. Run does not start a mutant when the
	// time left is shorter than the deadlines of the mutant's run and of
	// the closing control run together. Under Confirm, a survivor's
	// confirmation starts only while the time left covers its deadline, the
	// time that the ordinary control run's builds took and the closing
	// control run's deadline, and a survivor whose confirmation does not fit
	// is not-run. A mutant without coverage, which runs in its confirmation
	// alone, starts only while the time left covers that confirmation. A run
	// of one test alone starts only while the time left also covers its test
	// binary's deadline.
	Deadline time.Time
	// Sample is the number of mutants whose runs start, in the order of
	// their keys, or 0 for every mutant. Every later mutant is not-run, so
	// the record's sample states the score of the same mutants for the same
	// code on every machine. A run that this limit alone ends does not fail,
	// and its score is the score of its sample.
	Sample int
	// IncludeGenerated makes every generated file of the package a target,
	// as if the comments before its package clause contained the include
	// directive. The record lists each such file as included.
	IncludeGenerated bool
	// List makes Run stop after the instrumentation, which type-checks the
	// instrumented package: the record lists every mutant with the verdict
	// that the enumeration decides, and each mutant that a run would test
	// without a verdict. Run then builds and runs nothing, and an
	// instrumentation that the type checker rejects is the run error build.
	List bool
	// Confirm makes the run confirm each mutant that survives its run, and
	// each mutant whose site the opening control run never executed. Run
	// writes the mutant alone into the package's source, without a switch,
	// builds the suite from that source, and runs it once more without the
	// instrumented variable. The verdict of that run is the mutant's
	// verdict, and a mutant without coverage keeps no-coverage when the run
	// passes. Before the mutant runs, the ordinary control run builds and
	// runs the suite from the unchanged source.
	Confirm bool
	// Admit, when it is not nil, admits the runs that follow the opening
	// control run. Run calls it with the memory that those runs may use at
	// once: the workers times the largest memory ceiling of the suite's test
	// binaries, 0 where the engine applies no ceiling. Admit waits until the
	// caller admits the runs, and returns the function that Run calls when
	// its closing control run has ended. It returns false when ctx ended
	// first, and Run then stops.
	Admit func(ctx context.Context, bytes int64) (release func(), ok bool)
	// Verdict receives each mutant when its verdict is final, from one
	// goroutine at a time: the mutant, the number of the run's mutants, and
	// the path of the mutant's file relative to the working directory, as
	// [record.Record.Path] states it. Verdict may be nil.
	Verdict func(m record.Mutant, mutants int, path string)
}

// runner is the state of one run.
type runner struct {
	cfg Config
	// procs is the number of threads that the run may use.
	procs int
	def   spec.Definition
	rec   *record.Record
	work  string
	// wd is the process's working directory as record.Resolved returns it,
	// against which report states each mutant's path.
	wd   string
	pkg  *load.Package
	prog *render.Program
	// programs lists the test binaries of the suite: the package's own
	// first, when it has a test file, and then the others by import path.
	programs []*program
	// snapshots lists the files of the package, and then of each other
	// package of the suite, before any test binary runs.
	snapshots []snapshot
	// ran reports whether a test binary has started.
	ran bool
	// order lists the mutant of each entry of the record.
	order []*enumerate.Mutant
	// total is the sum of the programs' deadlines: the longest that one
	// mutant's run of every program, or the closing control run, can take.
	total time.Duration
	// ordinaryBuild is how long the builds of the ordinary control run
	// took, the time that a confirmation's builds take again.
	ordinaryBuild time.Duration
	// limited reports whether cfg.Sample alone ended the mutant runs: the
	// limit stopped the starts, and every mutant that started ran.
	limited bool
	mu      sync.Mutex
}

// outcome is a mutant's verdict and what its runs state: the tests that
// failed or were running, the runs' wall time when the engine measured it,
// the reason, and whether a confirmation run decided the verdict.
type outcome struct {
	verdict   spec.Verdict
	tests     []string
	seconds   *float64
	reason    string
	confirmed bool
}

// Run runs mutation testing on the package in cfg.Dir and returns the run's
// record.
//
// The record states every failure that the protocol defines: a run error,
// and the verdict of each mutant. A run without a mutant to run, because
// each is not-selected, suppressed or not-viable, builds and runs nothing,
// and its record states no control run. A suite without a test binary,
// because the package has no test file and cfg.Suite names no package
// whose tests link it, runs nothing, and every runnable mutant is
// no-coverage. When ctx is done, Run stops the runs, and every mutant
// without a verdict is not-run.
//
// Run starts the mutants' runs in the order of their keys. When ctx, the
// caller's deadline or cfg.Sample ends the runs, the record's sample states
// the score of the mutants whose keys sort before the first key that did not
// run.
//
// Each test binary has its own limits, from its opening control run. After
// the control runs before the mutants, Run runs each top-level test of a
// binary alone, where the binary has fewer tests than mutants to run, and
// records the sites that each test executes. A mutant's run of such a
// binary starts with the tests that executed the mutant's site, and runs
// the binary's whole suite after them when they pass.
//
// Under cfg.Confirm, the verdict of a survivor, and of a mutant whose site
// no test binary executed, is final only after its confirmation run, so
// its verdict, tests and seconds are those of its ordinary build, and the
// mutant is confirmed.
//
// Run reads the files of the package, and of each other package of the
// suite, before any test binary runs. The inputs digest states them as they
// are then, and the run error changed-files names each file that differs
// after the runs.
//
// # Errors
//
// Run returns an error when the package directory or one of its data files
// does not read, and when it cannot make its work directory.
func Run(ctx context.Context, cfg Config) (*record.Record, error) {
	def := spec.Load()
	dir, _ := filepath.Abs(cfg.Dir)
	resolved, err := filepath.EvalSymlinks(dir)
	var files map[string]string
	if err == nil {
		files, err = readSnapshot(resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	work, err := os.MkdirTemp("", workPattern)
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	defer os.RemoveAll(work)
	info, _ := debug.ReadBuildInfo()
	r := &runner{
		cfg: cfg, procs: threads(cfg), def: def, work: work, wd: record.Resolved(workingDir),
		snapshots: []snapshot{{dir: resolved, files: files}},
		rec: &record.Record{
			Record:    def.Protocol.Record.Name,
			Version:   def.Protocol.Record.Version,
			Catalogue: def.Version,
			Overlay:   def.Overlay.Version,
			Engine:    record.Engine{Name: Engine, Version: record.EngineVersion(info)},
			Toolchain: runtime.Version(),
			Target:    record.Target{Language: def.Overlay.Language, Name: dir},
			Root:      dir,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
			Limits:    []record.Limits{},
			Errors:    []record.RunError{},
			Mutants:   []record.Mutant{},
			Skipped:   []record.Skip{},
			Generated: []record.Generated{},
		},
	}
	r.run(ctx)
	r.compare()
	r.rec.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	limit := 0
	if r.limited {
		limit = cfg.Sample
	}
	r.rec.SetScore(def.Protocol, limit)
	r.rec.Inputs = r.inputs(ctx, resolved)
	return r.rec, nil
}

// threads returns the number of threads that a run of cfg may use:
// cfg.Procs, or the engine's GOMAXPROCS where cfg.Procs is below 1.
func threads(cfg Config) int {
	if cfg.Procs < 1 {
		return runtime.GOMAXPROCS(0)
	}
	return cfg.Procs
}

// goEnv returns the environment of a go command of the run: cfg.Env with
// GOMAXPROCS set to the run's threads and the threads that cfg.Spare
// returns, divided by divisor, and at least 1. A confirmation's build runs
// beside the other workers, so its divisor is the workers.
func (r *runner) goEnv(divisor int) []string {
	procs := r.procs
	if r.cfg.Spare != nil {
		procs += r.cfg.Spare()
	}
	return testbin.Setenv(r.cfg.Env, procsVar+"="+strconv.Itoa(max(1, procs/max(1, divisor))))
}

// inputs returns the record's inputs digest of the run of the package in
// dir. The build ID of the engine's executable identifies a development
// build, and when it does not read, the digest states the engine's version
// alone.
func (r *runner) inputs(ctx context.Context, dir string) string {
	exe, _ := os.Executable()
	engine, _ := buildID(context.WithoutCancel(ctx), dir, r.goEnv(1), exe)
	var ids, digests []string
	for _, p := range r.programs {
		ids = append(ids, p.target+" "+p.buildID)
	}
	for _, s := range r.snapshots {
		digests = append(digests, s.importPath+" "+record.Files(s.files))
	}
	return record.Inputs(
		record.Identity(r.rec.Engine.Version, engine),
		r.rec.Toolchain,
		strings.Join(ids, "\n"),
		strings.Join(digests, "\n"),
	)
}

// run runs the protocol's steps up to the first that stops the run.
func (r *runner) run(ctx context.Context) {
	result, err := r.loadMutants(ctx)
	if err != nil {
		r.fail(spec.ErrorLoad, err.Error())
		return
	}
	for _, s := range result.Skipped {
		r.rec.Skipped = append(
			r.rec.Skipped,
			record.Skip{File: s.File, Start: position(s.Start), End: position(s.End), Reason: s.Reason},
		)
	}
	for _, g := range result.Generated {
		r.rec.Generated = append(
			r.rec.Generated,
			record.Generated{File: g.File, Mutants: g.Mutants, Included: g.Included},
		)
	}
	for _, problem := range result.Problems {
		r.fail(problem.Code, problem.Message)
	}
	if len(result.Problems) > 0 {
		r.list(result)
		r.stop("the run stopped at an annotation error")
		return
	}
	prog, err := render.Render(r.pkg, result, r.def.Protocol.Variable)
	r.list(result)
	r.prog = prog
	if r.cfg.List {
		if err != nil {
			r.fail(spec.ErrorBuild, err.Error())
		}
		return
	}
	if len(r.pending()) == 0 {
		// No verdict depends on the tests.
		return
	}
	if err == nil {
		if err = r.suite(ctx); err != nil {
			r.fail(spec.ErrorLoad, err.Error())
			r.stop("the packages of the suite do not load")
			return
		}
		if len(r.programs) == 0 {
			r.uncovered(nil)
			return
		}
	}
	reason := r.build(ctx, err)
	if reason == "" {
		reason = r.opening(ctx)
	}
	if reason == "" {
		var release func()
		release, reason = r.admit(ctx)
		defer release()
	}
	if reason == "" && r.cfg.Confirm {
		reason = r.ordinary(ctx)
	}
	if reason != "" {
		r.stop(reason)
		return
	}
	r.alone(ctx)
	r.mutants(ctx)
	r.closing(ctx)
}

// loadMutants loads the run's package and enumerates its mutants under the
// run's selection. A package that go list lists has its import path as the
// target's name, also when it does not load. It returns the error of the
// load when the package does not load.
func (r *runner) loadMutants(ctx context.Context) (*enumerate.Result, error) {
	include := r.def.Overlay.Comment + r.def.Catalogue.Include
	p, err := load.Load(ctx, load.Config{Dir: r.cfg.Dir, Env: r.goEnv(1), Imports: render.Imports(), Include: include})
	if p != nil {
		r.rec.Target.Name, r.rec.Root, r.rec.Toolchain = p.ImportPath, p.Root, p.Toolchain
	}
	if err != nil {
		return nil, err
	}
	r.pkg = p
	return enumerate.Enumerate(p, r.def, enumerate.Options{
		Lines:            r.selection(),
		IncludeGenerated: r.cfg.IncludeGenerated,
	}), nil
}

// admit waits until cfg.Admit admits the runs that follow the opening
// control run, and returns the function that releases them. It returns the
// reason that the run stops when ctx ended first.
func (r *runner) admit(ctx context.Context) (release func(), reason string) {
	if r.cfg.Admit == nil {
		return func() {}, ""
	}
	var ceiling int64
	for _, p := range r.programs {
		ceiling = max(ceiling, p.ceiling)
	}
	release, ok := r.cfg.Admit(ctx, int64(max(1, r.cfg.Workers))*ceiling)
	if !ok {
		return func() {}, cancelled
	}
	return release, ""
}

// selection returns the ranges of the run's selection in the package's
// files, by file relative to the module root, and states them in the
// record. It returns nil for a run of every line, and an empty selection
// when no range lies in a file of the package.
func (r *runner) selection() []enumerate.Range {
	if r.cfg.Lines == nil {
		return nil
	}
	ranges := []enumerate.Range{}
	r.rec.Selection = []record.Range{}
	for _, l := range r.cfg.Lines {
		path := l.Path
		if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
			path = filepath.Join(dir, filepath.Base(path))
		}
		if filepath.Dir(path) != r.pkg.Dir {
			continue
		}
		rel, _ := filepath.Rel(r.pkg.Root, path)
		file := filepath.ToSlash(rel)
		ranges = append(ranges, enumerate.Range{File: file, First: l.First, Last: l.Last})
		r.rec.Selection = append(r.rec.Selection, record.Range{File: file, First: l.First, Last: l.Last})
	}
	return ranges
}

// position returns the record's position of a position of the enumeration.
func position(p enumerate.Position) record.Position {
	return record.Position{Line: p.Line, Column: p.Column}
}

// suite adds the test binaries of the run to the programs: the package's
// own, when it has a test file, and the binary of each other package that
// cfg.Suite names and whose tests link the package, in the order of their
// import paths. It reads the data files of each such other package, and
// states the packages in the record.
func (r *runner) suite(ctx context.Context) error {
	if r.pkg.Tests {
		r.programs = append(r.programs, &program{dir: r.pkg.Dir})
	}
	if len(r.cfg.Suite) == 0 {
		return nil
	}
	linked, err := load.Linking(ctx, load.Config{Dir: r.pkg.Dir, Env: r.goEnv(1)}, r.pkg.ImportPath, r.cfg.Suite)
	if err != nil {
		return err
	}
	for _, l := range linked {
		files, err := readSnapshot(l.Dir)
		if err != nil {
			return err
		}
		r.programs = append(r.programs, &program{target: l.ImportPath, dir: l.Dir})
		r.snapshots = append(r.snapshots, snapshot{importPath: l.ImportPath, dir: l.Dir, files: files})
		r.rec.Suite = append(r.rec.Suite, l.ImportPath)
	}
	return nil
}

// fail adds a run error to the record.
func (r *runner) fail(code spec.ErrorCode, message string) {
	r.rec.Errors = append(r.rec.Errors, record.RunError{Code: code, Message: message})
}

// list adds every mutant of result to the record, with the verdict that the
// enumeration and the instrumentation decide. It then reports each mutant
// that has a verdict. A runnable mutant gets its verdict from the runs.
func (r *runner) list(result *enumerate.Result) {
	verdicts := map[enumerate.Status]spec.Verdict{
		enumerate.Suppressed:  spec.Suppressed,
		enumerate.NotViable:   spec.NotViable,
		enumerate.NotSelected: spec.NotSelected,
	}
	for _, m := range result.Mutants {
		r.order = append(r.order, m)
		r.rec.Mutants = append(r.rec.Mutants, record.Mutant{
			Key:         m.Key,
			Kind:        m.Kind,
			File:        m.Site.File.Name,
			Scope:       m.Site.Scope,
			Start:       position(m.Site.StartPos),
			End:         position(m.Site.EndPos),
			Original:    m.Original,
			Replacement: m.Replacement,
			Verdict:     verdicts[m.Status],
			Rule:        m.Rule,
			Reason:      m.Reason,
		})
	}
	for i, m := range result.Mutants {
		if m.Status != enumerate.Runnable {
			r.report(i)
		}
	}
}

// pending returns the record's index of each mutant without a verdict, in
// record order.
func (r *runner) pending() []int {
	var indexes []int
	for i := range r.rec.Mutants {
		if r.rec.Mutants[i].Verdict == "" {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// stop gives every mutant without a verdict the verdict not-run, with the
// reason.
func (r *runner) stop(reason string) {
	for i := range r.rec.Mutants {
		if r.rec.Mutants[i].Verdict == "" {
			r.set(i, outcome{verdict: spec.NotRun, reason: reason})
		}
	}
}

// uncovered gives every mutant without a verdict whose site did not
// execute the verdict no-coverage. executed contains the first ordinal of each
// site that executed, which is the ordinal of the site's first mutant.
func (r *runner) uncovered(executed map[int]bool) {
	for _, i := range r.pending() {
		if !executed[r.first(i)] {
			r.set(i, outcome{verdict: spec.NoCoverage})
		}
	}
}

// first returns the first ordinal of the site of the mutant at index i.
func (r *runner) first(i int) int { return r.prog.Ordinals[r.order[i].Site.Mutants[0]] }

// set gives the mutant at index i the outcome o and the tests that executed
// its site alone, and reports it.
func (r *runner) set(i int, o outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := &r.rec.Mutants[i]
	m.Verdict, m.Tests, m.Seconds, m.Confirmed = o.verdict, o.tests, o.seconds, o.confirmed
	m.CoveredBy = r.coveredBy(i)
	if o.reason != "" {
		m.Reason = o.reason
	}
	r.report(i)
}

// coveredBy returns the tests whose run alone executed the site of the
// mutant at index i, as a record names them: the tests of each program
// whose opening control run executed the site, in the order of the
// programs and of each program's tests. It returns nil when no test
// executed the site alone, and when a program that executed the site has
// no record of each test's sites.
func (r *runner) coveredBy(i int) []string {
	var tests []string
	for _, p := range r.programs {
		if !p.executed[r.first(i)] {
			continue
		}
		if p.sites == nil {
			return nil
		}
		var covering []string
		for _, test := range p.tests {
			if p.sites[test][r.first(i)] {
				covering = append(covering, test)
			}
		}
		tests = append(tests, named(covering, p.target)...)
	}
	return tests
}

// report passes the mutant at index i to cfg.Verdict, with the number of
// the run's mutants and the path of the mutant's file relative to the
// working directory.
func (r *runner) report(i int) {
	if r.cfg.Verdict != nil {
		m := r.rec.Mutants[i]
		r.cfg.Verdict(m, len(r.rec.Mutants), r.rec.Path(m.File, r.wd))
	}
}
