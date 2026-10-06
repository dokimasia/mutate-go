// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
)

// The run errors that Run finds, as the protocol spells them.
const (
	ErrorLoad            = "load"
	ErrorBuild           = "build"
	ErrorControl         = "control-failed"
	ErrorNotInstrumented = "not-instrumented"
	ErrorOrdinary        = "ordinary-control-failed"
	ErrorClosing         = "closing-control-failed"
	ErrorChangedFiles    = "changed-files"
)

// Engine is the record's name of this engine.
const Engine = "mutate-go"

// openingLimit bounds the opening control run of each test binary of a
// caller without a deadline, as go test bounds a test binary by default.
const openingLimit = 10 * time.Minute

// cancelled is the reason of every mutant that does not run because the
// caller's context is done.
const cancelled = "the caller cancelled the run"

// failures states, by the verdict that classify gives a failed control run,
// what the tests that the run names did, or the test binary when it names
// none.
var failures = map[string]string{
	record.Killed:    "failed",
	record.TimedOut:  "ran until the deadline",
	record.Exhausted: "exceeded the memory ceiling",
}

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
	Lines []Lines
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
	// Deadline is when the caller's time ends, or zero for a caller
	// without a deadline. The opening control run of each test binary gets
	// at most half of the time left. Run does not start a mutant when the
	// time left is shorter than the deadlines of the mutant's run and of
	// the closing control run together. Under Confirm, the time left must
	// also cover the deadline of a confirmation run and the time that the
	// ordinary control run's builds took. A run of one test alone starts
	// only while the time left also covers its test binary's deadline.
	Deadline time.Time
	// Sample is the number of mutants whose runs start, in the order of
	// their keys, or 0 for every mutant. Every later mutant is not-run, so
	// the record's sample states the score of the same mutants for the same
	// code on every machine.
	Sample int
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
	// goroutine at a time, with the run's record. The record's Root, its
	// Target and the number of its Mutants are final when Verdict first
	// runs, and its other fields change until Run returns. Verdict may be
	// nil.
	Verdict func(rec *record.Record, m record.Mutant)
}

// Lines selects the lines First to Last of the file at Path, an absolute
// path.
type Lines struct {
	Path        string
	First, Last int
}

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
// package directory: . for the run's own package, and the import path for
// another.
func (p *program) pattern() string {
	if p.target == "" {
		return "."
	}
	return p.target
}

// snapshot is the digest of each data file of one package of the suite,
// as Run reads them before any test binary runs.
type snapshot struct {
	// importPath is empty for the run's own package.
	importPath string
	dir        string
	files      map[string]string
}

// runner is the state of one run.
type runner struct {
	cfg Config
	// procs is the number of threads that the run may use, and goEnv the
	// environment of every go command, which sets GOMAXPROCS to procs. A
	// confirmation's build runs beside the other workers, so confirmEnv
	// sets GOMAXPROCS to procs divided by the workers.
	procs      int
	goEnv      []string
	confirmEnv []string
	def        spec.Definition
	rec        *record.Record
	work       string
	pkg        *load.Package
	prog       *render.Program
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
	mu            sync.Mutex
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
		files, err = record.Snapshot(resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	work, err := os.MkdirTemp("", "mutate-")
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	defer os.RemoveAll(work)
	info, _ := debug.ReadBuildInfo()
	procs := cfg.Procs
	if procs < 1 {
		procs = runtime.GOMAXPROCS(0)
	}
	goEnv := setenv(cfg.Env, "GOMAXPROCS="+strconv.Itoa(procs))
	confirmEnv := setenv(cfg.Env, "GOMAXPROCS="+strconv.Itoa(max(1, procs/max(1, cfg.Workers))))
	r := &runner{
		cfg: cfg, procs: procs, goEnv: goEnv, confirmEnv: confirmEnv, def: def, work: work,
		snapshots: []snapshot{{dir: resolved, files: files}},
		rec: &record.Record{
			Record:    def.Protocol.Record.Name,
			Version:   def.Protocol.Record.Version,
			Catalogue: def.Version,
			Overlay:   def.Overlay.Version,
			Engine:    record.Engine{Name: Engine, Version: record.EngineVersion(info)},
			Toolchain: runtime.Version(),
			Target:    record.Target{Language: "go", Name: dir},
			Root:      dir,
			StartedAt: timestamp(),
			Limits:    []record.Limits{},
			Errors:    []record.RunError{},
			Mutants:   []record.Mutant{},
			Skipped:   []record.Skip{},
			Generated: []record.Generated{},
		},
	}
	r.run(ctx)
	r.compare()
	r.rec.FinishedAt = timestamp()
	r.rec.SetScore(def.Protocol)
	// The build ID of the engine's executable identifies a development
	// build. When it does not read, the digest states the version alone.
	exe, _ := os.Executable()
	engine, _ := load.Go(context.WithoutCancel(ctx), resolved, r.goEnv, "tool", "buildid", exe)
	identity := record.Identity(r.rec.Engine.Version, strings.TrimSpace(string(engine)))
	var ids, digests []string
	for _, p := range r.programs {
		ids = append(ids, p.target+" "+p.buildID)
	}
	for _, s := range r.snapshots {
		digests = append(digests, s.importPath+" "+record.Files(s.files))
	}
	r.rec.Inputs = record.Inputs(identity, r.rec.Toolchain, strings.Join(ids, "\n"), strings.Join(digests, "\n"))
	return r.rec, nil
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }

// run runs the protocol's steps up to the first that stops the run. A
// package that go list lists has its import path as the target's name,
// also when it does not load.
func (r *runner) run(ctx context.Context) {
	include := r.def.Overlay.Comment + r.def.Catalogue.Include
	p, err := load.Load(ctx, load.Config{Dir: r.cfg.Dir, Env: r.goEnv, Imports: render.Imports(), Include: include})
	if p != nil {
		r.rec.Target.Name, r.rec.Root, r.rec.Toolchain = p.ImportPath, p.Root, p.Toolchain
	}
	if err != nil {
		r.fail(ErrorLoad, err.Error())
		return
	}
	r.pkg = p
	result := enumerate.Enumerate(p, r.def, r.selection())
	for _, s := range result.Skipped {
		r.rec.Skipped = append(
			r.rec.Skipped,
			record.Skip{File: s.File, Start: position(s.Start), End: position(s.End), Reason: s.Reason},
		)
	}
	for _, g := range result.Generated {
		r.rec.Generated = append(r.rec.Generated, record.Generated{File: g.File, Mutants: g.Mutants})
	}
	for _, problem := range result.Problems {
		r.fail(problem.Code, problem.Message)
	}
	if len(result.Problems) > 0 {
		r.list(result)
		r.stop("the run stopped at an annotation error")
		return
	}
	prog, err := render.Render(p, result)
	r.list(result)
	r.prog = prog
	if len(r.pending()) == 0 {
		// No verdict depends on the tests.
		return
	}
	if err == nil {
		if err = r.suite(ctx); err != nil {
			r.fail(ErrorLoad, err.Error())
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

func position(p enumerate.Position) record.Position {
	return record.Position{Line: p.Line, Column: p.Column}
}

// fail adds a run error to the record.
func (r *runner) fail(code, message string) {
	r.rec.Errors = append(r.rec.Errors, record.RunError{Code: code, Message: message})
}

// list adds every mutant of result to the record, with the verdict that the
// enumeration and the instrumentation decide. It then reports each mutant
// that has a verdict. A runnable mutant gets its verdict from the runs.
func (r *runner) list(result *enumerate.Result) {
	verdicts := map[enumerate.Status]string{
		enumerate.Suppressed:  record.Suppressed,
		enumerate.NotViable:   record.NotViable,
		enumerate.NotSelected: record.NotSelected,
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
			r.set(i, outcome{verdict: record.NotRun, reason: reason})
		}
	}
}

// uncovered gives every mutant without a verdict whose site did not
// execute the verdict no-coverage. executed contains the first ordinal of each
// site that executed, which is the ordinal of the site's first mutant.
func (r *runner) uncovered(executed map[int]bool) {
	for _, i := range r.pending() {
		if !executed[r.first(i)] {
			r.set(i, outcome{verdict: record.NoCoverage})
		}
	}
}

// first returns the first ordinal of the site of the mutant at index i.
func (r *runner) first(i int) int { return r.prog.Ordinals[r.order[i].Site.Mutants[0]] }

// outcome is a mutant's verdict and what its runs state: the tests that
// failed or were running, the runs' wall time when the engine measured it,
// the reason, and whether a confirmation run decided the verdict.
type outcome struct {
	verdict   string
	tests     []string
	seconds   *float64
	reason    string
	confirmed bool
}

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
	if r.cfg.Verdict != nil {
		r.cfg.Verdict(r.rec, *m)
	}
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

// report passes the mutant at index i to the caller.
func (r *runner) report(i int) {
	if r.cfg.Verdict != nil {
		r.cfg.Verdict(r.rec, r.rec.Mutants[i])
	}
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
	linked, err := load.Linking(ctx, load.Config{Dir: r.pkg.Dir, Env: r.goEnv}, r.pkg.ImportPath, r.cfg.Suite)
	if err != nil {
		return err
	}
	for _, l := range linked {
		files, err := record.Snapshot(l.Dir)
		if err != nil {
			return err
		}
		r.programs = append(r.programs, &program{target: l.ImportPath, dir: l.Dir})
		r.snapshots = append(r.snapshots, snapshot{importPath: l.ImportPath, dir: l.Dir, files: files})
		r.rec.Suite = append(r.rec.Suite, l.ImportPath)
	}
	return nil
}

// build builds the instrumented test binary of each program and reads its
// build ID. It returns the reason that the run stops before the opening
// control run, or "" when every program built. err is the error of the
// instrumentation, which fails the build too.
//
// The build runs without vet, because the vet of Go 1.21 to 1.23 opens a
// file that only the overlay adds on disk, where it does not exist.
func (r *runner) build(ctx context.Context, err error) string {
	var overlay string
	if err == nil {
		overlay, err = r.prog.Write(filepath.Join(r.work, "src"))
	}
	for i := 0; err == nil && i < len(r.programs); i++ {
		p := r.programs[i]
		name := "pkg.test"
		if p.target != "" {
			name = "suite" + strconv.Itoa(i) + ".test"
		}
		p.bin = filepath.Join(r.work, name+exeSuffix)
		_, err = load.Go(
			ctx,
			r.pkg.Dir,
			r.goEnv,
			"test",
			"-c",
			"-vet=off",
			"-o",
			p.bin,
			"-overlay",
			overlay,
			p.pattern(),
		)
		if err != nil {
			return r.buildFailed(ctx, p, err)
		}
		var id []byte
		id, err = load.Go(ctx, r.pkg.Dir, r.goEnv, "tool", "buildid", p.bin)
		p.buildID = strings.TrimSpace(string(id))
	}
	if err != nil {
		r.fail(ErrorBuild, err.Error())
		return "the instrumented build failed"
	}
	return ""
}

// buildFailed states the failure err of the instrumented build of p, and
// returns the reason that the run stops. It builds p from the unchanged
// source too, so the run error states whether the tests build without the
// engine. A build that ctx ended states no run error.
func (r *runner) buildFailed(ctx context.Context, p *program, err error) string {
	_, stop := r.ordinaryBinary(ctx, filepath.Join(r.work, "unchanged"), p, "")
	switch {
	case ctx.Err() != nil:
		return cancelled
	case stop == nil:
		r.fail(ErrorBuild, "the instrumented build fails, and the unchanged source builds: "+err.Error())
		return "the instrumented build failed"
	}
	r.fail(ErrorBuild, "the tests"+of(p.target)+" do not build from the unchanged source: "+stop.reason)
	return "the tests do not build"
}

// env returns the environment of a run of the instrumented test binary with
// the mutant ordinal active, 0 for none, and the trace file trace, empty for
// none. The run's threads are divided among the workers.
func (r *runner) env(ordinal int, trace string) []string {
	return setenv(
		r.cfg.Env,
		r.def.Protocol.Variable+"="+strconv.Itoa(ordinal),
		r.def.Protocol.Instrumented+"=1",
		"DOKIMI_MUTATE_TRACE="+trace,
		"GOMAXPROCS="+strconv.Itoa(max(1, r.procs/max(1, r.cfg.Workers))),
	)
}

// ordinaryEnv returns the environment of a run of an ordinary build, the
// ordinary control run for the ordinal 0 and a confirmation run for a
// mutant's ordinal: the environment of env without the instrumented
// variable and the trace.
func (r *runner) ordinaryEnv(ordinal int) []string {
	return setenv(r.env(ordinal, ""), r.def.Protocol.Instrumented, "DOKIMI_MUTATE_TRACE")
}

// flags returns the test binary's flags for a run with the deadline
// timeout. Every run states the progress of its tests in the framed lines
// that Scan reads, and a mutant's run stops at its first failing test.
func flags(timeout time.Duration, failfast bool) []string {
	f := []string{"-test.v=test2json", "-test.paniconexit0", "-test.timeout=" + timeout.String()}
	if failfast {
		f = append(f, "-test.failfast")
	}
	return f
}

// openingLimit returns the limit of one test binary's opening control run,
// and whether the caller's deadline sets it: at most half of the time left.
func (r *runner) openingLimit() (time.Duration, bool) {
	if !r.cfg.Deadline.IsZero() {
		if half := time.Until(r.cfg.Deadline) / 2; half < openingLimit {
			return half, true
		}
	}
	return openingLimit, false
}

// opening runs the opening control run of each program, and sets each
// program's limits and its top-level tests from its run. Without
// cfg.Confirm, it gives the verdict no-coverage to each mutant whose site
// the programs never executed. It returns the reason that the run stops
// before the mutant runs, or "" when they start. A run that the caller's
// deadline ends is not a failure of the tests.
func (r *runner) opening(ctx context.Context) string {
	tooLate := "the caller's deadline leaves too little time for the opening control run"
	limits := r.def.Protocol.Limits
	var seconds float64
	var peak *int64
	executed := map[int]bool{}
	for i, p := range r.programs {
		limit, late := r.openingLimit()
		if limit <= 0 {
			return tooLate
		}
		r.ran = true
		trace := filepath.Join(r.work, "trace"+strconv.Itoa(i))
		ex := execute(ctx, p.bin, p.dir, r.work, r.env(0, trace), flags(limit, false), limit, backupDelay, 0, false)
		verdict, _, _ := classify(ex)
		switch {
		case ex.ended == endedCancel:
			return cancelled
		case late && verdict == record.TimedOut:
			return tooLate
		case failed(ex):
			r.fail(ErrorControl, describe("the tests fail with no mutant active", ex, p.target))
			return "the opening control run failed"
		}
		data, _ := os.ReadFile(trace)
		var started bool
		p.executed, started = parseTrace(string(data))
		if !started {
			r.fail(
				ErrorNotInstrumented,
				"the opening control run's trace"+of(p.target)+
					" has no start mark, so the test binary did not run the instrumented package",
			)
			return "the opening control run failed"
		}
		for o := range p.executed {
			executed[o] = true
		}
		p.tests = ex.output.Tests
		p.deadline = time.Duration((limits.Deadline.Factor*ex.seconds + limits.Deadline.Seconds) * float64(time.Second))
		seconds += ex.seconds
		if b := peakBytes(ex.state); b != nil {
			p.ceiling = int64(limits.Memory.Factor*float64(*b)) + limits.Memory.Bytes
			if peak == nil || *b > *peak {
				peak = b
			}
		}
	}
	r.rec.Control = &record.Control{
		Opening: record.Opening{Seconds: seconds, PeakBytes: peak, Sites: len(r.prog.Sites)},
	}
	for _, s := range r.prog.Sites {
		if executed[r.prog.Ordinals[s.Mutants[0]]] {
			r.rec.Control.Opening.SitesExecuted++
		}
	}
	for _, p := range r.programs {
		l := record.Limits{Target: r.rec.Target.Name, DeadlineSeconds: p.deadline.Seconds()}
		if p.target != "" {
			l.Target = p.target
		}
		if p.ceiling > 0 {
			ceiling := p.ceiling
			l.MemoryCeilingBytes = &ceiling
		}
		r.rec.Limits = append(r.rec.Limits, l)
		r.total += p.deadline
	}
	if !r.cfg.Confirm {
		r.uncovered(executed)
	}
	return ""
}

// ordinary runs the ordinary control run. It builds each program from the
// package's unchanged source, as the toolchain builds it without the
// engine, and runs it with no mutant active and without the instrumented
// variable, under the program's limits. It states the run's time in the
// record, and returns the reason that the run stops before the mutants
// run, or "" when every program passed.
func (r *runner) ordinary(ctx context.Context) string {
	if !r.cfg.Deadline.IsZero() && time.Until(r.cfg.Deadline) < 2*r.total {
		return "the caller's deadline leaves too little time for the ordinary control run"
	}
	dir := filepath.Join(r.work, "ordinary")
	defer os.RemoveAll(dir)
	var seconds float64
	for _, p := range r.programs {
		start := time.Now()
		bin, stop := r.ordinaryBinary(ctx, dir, p, "")
		r.ordinaryBuild += time.Since(start)
		if stop != nil {
			reason := stop.reason
			if stop.verdict == record.NotViable {
				r.fail(ErrorBuild, "the ordinary build of the unchanged source fails: "+stop.reason)
				reason = "the ordinary build failed"
			}
			return reason
		}
		ex := execute(
			ctx,
			bin,
			p.dir,
			r.work,
			r.ordinaryEnv(0),
			flags(p.deadline, false),
			p.deadline,
			backupDelay,
			p.ceiling,
			false,
		)
		if ex.ended == endedCancel {
			return cancelled
		}
		if failed(ex) {
			r.fail(ErrorOrdinary, describe("the tests fail in an ordinary build with no mutant active", ex, p.target))
			return "the ordinary control run failed"
		}
		seconds += ex.seconds
	}
	r.rec.Control.Ordinary = &record.Ordinary{Seconds: seconds}
	return ""
}

// of returns the words that name the package target in a message: empty
// for the run's own package, and " of" and the import path otherwise.
func of(target string) string {
	if target == "" {
		return ""
	}
	return " of " + target
}

// parseTrace returns the first ordinals in a trace, and whether the trace
// has the start mark.
func parseTrace(trace string) (executed map[int]bool, started bool) {
	executed = map[int]bool{}
	for _, line := range strings.Split(trace, "\n") {
		if line == "start" {
			started = true
		} else if o, err := strconv.Atoi(line); err == nil {
			executed[o] = true
		}
	}
	return executed, started
}

// failed reports whether a control run failed: it did not start, a test
// failed, the binary exited with a failure status, or the engine ended it.
func failed(ex *execution) bool {
	return ex.err != nil || ex.ended != "" || ex.state.ExitCode() != 0
}

// describe states a control run's failure after prefix: the tests that the
// run names and what they did, or the reason of a run that ended in an
// error, and then the end of the run's output. target is the package whose
// tests the binary runs, or empty for the run's own package.
func describe(prefix string, ex *execution, target string) string {
	verdict, tests, reason := classify(ex)
	cause := reason
	if did, ok := failures[verdict]; ok {
		subject := strings.Join(named(tests, target), ", ")
		if subject == "" {
			subject = "the test binary"
		}
		cause = subject + " " + did
	}
	tail := ex.output.Tail
	if len(tail) > 8<<10 {
		tail = tail[len(tail)-8<<10:]
	}
	return prefix + ": " + cause + "\n" + tail
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

// reserve returns the time that one more mutant needs before the caller's
// deadline: the deadlines of its run and of the closing control run, and
// under cfg.Confirm also the deadline of its confirmation run and the time
// of the ordinary control run's builds.
func (r *runner) reserve() time.Duration {
	if r.cfg.Confirm {
		return 3*r.total + r.ordinaryBuild
	}
	return 2 * r.total
}

// alone runs each top-level test of a program alone, with no mutant active
// and a trace on, on cfg.Workers workers and under the program's limits,
// and records the sites that each test executed. It does so for each
// program that has more than one test and fewer tests than the mutants
// without a verdict whose sites the program executed, because each test
// costs a run.
//
// alone drops a program's record when one of its tests fails alone, or
// when ctx or the caller's deadline ends the runs before each of its tests
// ran. A test's run starts only while the time left covers the program's
// deadline and the reserve of a mutant.
func (r *runner) alone(ctx context.Context) {
	type job struct {
		p           *program
		test, trace string
	}
	var jobs []job
	pending := r.pending()
	for k, p := range r.programs {
		covered := 0
		for _, i := range pending {
			if p.executed[r.first(i)] {
				covered++
			}
		}
		if len(p.tests) < 2 || len(p.tests) >= covered {
			continue
		}
		p.sites, p.seconds = map[string]map[int]bool{}, map[string]float64{}
		for n, test := range p.tests {
			trace := filepath.Join(r.work, "trace"+strconv.Itoa(k)+"-"+strconv.Itoa(n))
			jobs = append(jobs, job{p: p, test: test, trace: trace})
		}
	}
	var mu sync.Mutex
	broken := map[*program]bool{}
	queue := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < max(1, r.cfg.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				p, env := j.p, r.env(0, j.trace)
				args := append(flags(p.deadline, true), "-test.run=^"+regexp.QuoteMeta(j.test)+"$")
				ex := execute(ctx, p.bin, p.dir, r.work, env, args, p.deadline, backupDelay, p.ceiling, true)
				data, _ := os.ReadFile(j.trace)
				sites, _ := parseTrace(string(data))
				mu.Lock()
				broken[j.p] = broken[j.p] || failed(ex)
				j.p.sites[j.test], j.p.seconds[j.test] = sites, ex.seconds
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil || !r.cfg.Deadline.IsZero() && time.Until(r.cfg.Deadline) < r.reserve()+j.p.deadline {
			break
		}
		queue <- j
	}
	close(queue)
	wg.Wait()
	for _, p := range r.programs {
		if broken[p] || len(p.sites) < len(p.tests) {
			p.sites, p.seconds = nil, nil
		}
	}
}

// mutants runs each mutant without a verdict alone, on cfg.Workers
// workers. It starts the runs in the order of the mutants' keys, and of the
// record for two equal keys, so a run that the caller's deadline ends has
// run a uniform sample of the mutants. Under the caller's deadline, a
// mutant starts only while the time left covers the reserve of a mutant, and
// a timer ends the wait for a free worker once the time left no longer
// covers it.
//
// When ctx or the caller's deadline ends the starts, or cfg.Sample mutants
// have started, each mutant that no worker took is not-run at once, while
// the mutants that the workers took still run, so the caller sees that no
// further mutant starts.
func (r *runner) mutants(ctx context.Context) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(1, r.cfg.Workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				r.mutant(ctx, i, r.prog.Ordinals[r.order[i]])
			}
		}()
	}
	pending := r.pending()
	sort.SliceStable(
		pending,
		func(a, b int) bool { return r.rec.Mutants[pending[a]].Key < r.rec.Mutants[pending[b]].Key },
	)
	tooLate := "the caller's deadline leaves too little time for the mutant and the closing control run"
	if r.cfg.Confirm {
		tooLate = "the caller's deadline leaves too little time for the mutant, its confirmation and the closing control run"
	}
	// expired fires when the time left no longer covers the reserve of a
	// mutant. It is nil, and never fires, without a deadline.
	var expired <-chan time.Time
	if !r.cfg.Deadline.IsZero() {
		timer := time.NewTimer(time.Until(r.cfg.Deadline.Add(-r.reserve())))
		defer timer.Stop()
		expired = timer.C
	}
	next, reason := 0, ""
	for next < len(pending) && reason == "" {
		if next == r.cfg.Sample && next > 0 {
			reason = "the caller limits the run to " + strconv.Itoa(next) + " mutants"
			continue
		}
		// The timer's channel can be empty for a moment after its time, so
		// the time left is checked before each wait too.
		if !r.cfg.Deadline.IsZero() && time.Until(r.cfg.Deadline) < r.reserve() {
			reason = tooLate
			continue
		}
		select {
		case jobs <- pending[next]:
			next++
		case <-ctx.Done():
			reason = cancelled
		case <-expired:
			reason = tooLate
		}
	}
	for _, i := range pending[next:] {
		r.set(i, outcome{verdict: record.NotRun, reason: reason})
	}
	close(jobs)
	wg.Wait()
}

// mutant runs the mutant at index i, whose ordinal is ordinal, and gives the
// mutant the outcome. It runs the instrumented test binaries whose opening
// control run executed the mutant's site, with the mutant active, as
// runPrograms states, and under cfg.Confirm confirms a survivor in those
// binaries. A mutant without coverage runs only under cfg.Confirm, in its
// confirmation in every binary, and keeps no-coverage when every binary
// passes. When ctx is done, the run ends at once and gives the mutant
// not-run.
func (r *runner) mutant(ctx context.Context, i, ordinal int) {
	var covering []*program
	for _, p := range r.programs {
		if p.executed[r.first(i)] {
			covering = append(covering, p)
		}
	}
	if len(covering) == 0 {
		o := r.confirm(ctx, i, ordinal, r.programs)
		if o.verdict == record.Survived {
			o.verdict = record.NoCoverage
		}
		r.set(i, o)
		return
	}
	o := r.runPrograms(ctx, i, covering, r.env(ordinal, ""), true, func(p *program) (string, *outcome) {
		return p.bin, nil
	})
	if o.verdict == record.Survived && r.cfg.Confirm {
		o = r.confirm(ctx, i, ordinal, covering)
	}
	r.set(i, o)
}

// runPrograms runs each of programs in order, with the environment env,
// until one does not pass, and returns the outcome of the run that did not
// pass, or survived. binary returns a program's test binary, or the outcome
// of a build that gave none, which ends the runs. Each program's run ends
// at the program's deadline and memory ceiling. When first is true, the
// run of a program starts with the part that runs states for the mutant at
// index i, which ends at its own deadline, and the program's deadline
// applies to the parts together.
func (r *runner) runPrograms(
	ctx context.Context,
	i int,
	programs []*program,
	env []string,
	first bool,
	binary func(*program) (string, *outcome),
) outcome {
	var seconds float64
	for _, p := range programs {
		bin, stop := binary(p)
		if stop != nil {
			return *stop
		}
		parts := []part{{}}
		if first {
			parts = r.runs(p, i)
		}
		var used time.Duration
		for _, pt := range parts {
			// The runs before used the deadline, which leaves this run at
			// least a millisecond, so that its alarm is set.
			left := max(p.deadline-used, time.Millisecond)
			if pt.deadline > 0 {
				left = min(left, pt.deadline)
			}
			// A run ends at its deadline, unless it hangs before the testing
			// package's alarm starts. The engine then ends it, at the latest
			// when the time left falls to the closing control run's deadline.
			backup := backupDelay
			if !r.cfg.Deadline.IsZero() {
				backup = min(backup, max(0, time.Until(r.cfg.Deadline)-2*r.total))
			}
			args := append(flags(left, true), pt.filter...)
			ex := execute(ctx, bin, p.dir, r.work, env, args, left, backup, p.ceiling, true)
			used += time.Duration(ex.seconds * float64(time.Second))
			seconds += ex.seconds
			if v, ts, why := classify(ex); v != record.Survived {
				o := outcome{verdict: v, tests: named(ts, p.target), reason: why}
				if ex.err == nil && v != record.NotRun {
					o.seconds = &seconds
				}
				return o
			}
		}
	}
	return outcome{verdict: record.Survived, seconds: &seconds}
}

// runs returns the parts of the runs of program p for the mutant at index
// i: the tests that executed the mutant's site when they ran alone, and then
// p's whole suite. The first part ends at the protocol's factor times the
// tests' wall times in their runs alone, plus its constant. When p's tests
// did not run alone, or no test or every test executed the site, the whole
// suite is the only part.
func (r *runner) runs(p *program, i int) []part {
	var tests []string
	var seconds float64
	for _, test := range p.tests {
		if p.sites[test][r.first(i)] {
			tests = append(tests, regexp.QuoteMeta(test))
			seconds += p.seconds[test]
		}
	}
	if len(tests) == 0 || len(tests) == len(p.tests) {
		return []part{{}}
	}
	limit := r.def.Protocol.Limits.Deadline
	return []part{
		{
			filter:   []string{"-test.run=^(" + strings.Join(tests, "|") + ")$"},
			deadline: time.Duration((limit.Factor*seconds + limit.Seconds) * float64(time.Second)),
		},
		{},
	}
}

// confirm runs the ordinary build of the mutant at index i, whose ordinal
// is ordinal, in programs, and returns the outcome of that build's runs. It
// writes the mutant into the package's source as render.Plain writes it,
// builds each of programs from that source, and runs its whole suite with
// the mutant's ordinal and without the instrumented variable, as
// runPrograms states.
//
// The outcome is confirmed unless ctx ended the confirmation, which makes
// the mutant not-run. A build that the toolchain rejects makes the mutant
// not-viable, with the toolchain's message as its reason.
func (r *runner) confirm(ctx context.Context, i, ordinal int, programs []*program) outcome {
	dir := filepath.Join(r.work, "confirm"+strconv.Itoa(i))
	defer os.RemoveAll(dir)
	overlay, err := render.Plain(r.pkg, r.order[i]).Write(filepath.Join(dir, "src"))
	if err != nil {
		return outcome{
			verdict:   record.Error,
			reason:    "the ordinary build was not written: " + err.Error(),
			confirmed: true,
		}
	}
	o := r.runPrograms(ctx, i, programs, r.ordinaryEnv(ordinal), false, func(p *program) (string, *outcome) {
		return r.ordinaryBinary(ctx, dir, p, overlay)
	})
	o.confirmed = o.verdict != record.NotRun
	return o
}

// ordinaryBinary builds the test binary of p into dir, from the package's
// source with the files of overlay in place of its own, and returns its
// path. Without an overlay, it builds the unchanged source. A build that
// does not give a binary returns its outcome instead: not-run when ctx
// ended it, and not-viable with the toolchain's message otherwise.
func (r *runner) ordinaryBinary(ctx context.Context, dir string, p *program, overlay string) (string, *outcome) {
	bin := filepath.Join(dir, filepath.Base(p.bin))
	args := []string{"test", "-c", "-vet=off", "-o", bin}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	if _, err := load.Go(ctx, r.pkg.Dir, r.confirmEnv, append(args, p.pattern())...); err != nil {
		if ctx.Err() != nil {
			return "", &outcome{verdict: record.NotRun, reason: cancelled}
		}
		return "", &outcome{verdict: record.NotViable, reason: compilerMessage(err)}
	}
	return bin, nil
}

// compilerMessage returns the toolchain's message of a failed build: the
// lines of the error after the go command's own, without the lines that
// name a package.
func compilerMessage(err error) string {
	var lines []string
	for _, line := range strings.Split(err.Error(), "\n")[1:] {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// classify gives a run its verdict by the first rule that matches: the run
// did not start; the engine ended it for memory, at the deadline, for the
// caller, or at the first failed test; a signal that the engine did not
// send ended it; it passed; the testing package's alarm ended it; and
// otherwise a test failed.
//
// The tests of a run that ended before its tests did are the tests that
// were running. The same applies to a failed run whose output names no
// failed test, such as a test that calls os.Exit(1).
func classify(ex *execution) (verdict string, tests []string, reason string) {
	if ex.err != nil {
		return record.Error, nil, "the run did not start: " + ex.err.Error()
	}
	status, _ := ex.state.Sys().(syscall.WaitStatus)
	switch {
	case ex.ended == endedMemory:
		return record.Exhausted, ex.output.Running, ""
	case ex.ended == endedDeadline:
		return record.TimedOut, ex.output.Running, ""
	case ex.ended == endedCancel:
		return record.NotRun, nil, cancelled
	case ex.ended == endedFailure:
		return record.Killed, ex.output.Failed, ""
	case status.Signaled():
		reason = fmt.Sprintf("the run ended on the signal %s, which the engine did not send", status.Signal())
		return record.Error, nil, reason
	case ex.state.ExitCode() == 0:
		return record.Survived, nil, ""
	case ex.output.TimedOut:
		return record.TimedOut, ex.output.Running, ""
	case len(ex.output.Failed) == 0:
		return record.Killed, ex.output.Running, ""
	}
	return record.Killed, ex.output.Failed, ""
}

// closing runs the closing control run of each program, each under the
// program's limits, unless the caller cancelled the run.
func (r *runner) closing(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	var seconds float64
	for _, p := range r.programs {
		ex := execute(
			ctx,
			p.bin,
			p.dir,
			r.work,
			r.env(0, ""),
			flags(p.deadline, false),
			p.deadline,
			backupDelay,
			p.ceiling,
			false,
		)
		if ex.ended == endedCancel {
			return
		}
		if failed(ex) {
			r.fail(ErrorClosing, describe("the tests fail with no mutant active after the mutant runs", ex, p.target))
			return
		}
		seconds += ex.seconds
	}
	r.rec.Control.Closing = &record.Closing{Seconds: seconds}
}

// compare states the run error changed-files when a data file of a package
// of the suite differs after the runs from the file before them, or was
// added or removed. It compares nothing when no test binary started.
func (r *runner) compare() {
	if !r.ran {
		return
	}
	var changed []string
	for _, s := range r.snapshots {
		after, err := record.Snapshot(s.dir)
		if err != nil {
			r.fail(ErrorChangedFiles, "the files do not read after the runs: "+err.Error())
			return
		}
		rel, _ := filepath.Rel(r.rec.Root, s.dir)
		for _, path := range record.Changed(s.files, after) {
			changed = append(changed, filepath.ToSlash(filepath.Join(rel, path)))
		}
	}
	if len(changed) > 0 {
		r.fail(ErrorChangedFiles, "the runs added, changed or removed these files: "+strings.Join(changed, ", "))
	}
}
