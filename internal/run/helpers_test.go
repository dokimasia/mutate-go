// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/process"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// The variables of the environment that the tests set.
const (
	pathVar  = "PATH"
	procsVar = "GOMAXPROCS"
)

// tempVars are the variables that name the temporary directory of a
// process: TMPDIR on Unix, and TMP and TEMP on Windows.
var tempVars = []string{"TMPDIR", "TMP", "TEMP"}

// The variables of the fixtures' environment.
const (
	// goVar names the go command that the wrapper of goWrapper runs.
	goVar = "FIXTURE_GO"
	// goLogVar names the file to which the wrapper of goWrapper appends its
	// GOMAXPROCS.
	goLogVar = "FIXTURE_GO_LOG"
	// blockVar is the shell pattern of the argument that makes the wrapper of
	// goWrapper create the marker and wait in place of the go command.
	blockVar = "FIXTURE_BLOCK"
	// markerVar names the file that a fixture creates at the step of the run
	// at which the test acts.
	markerVar = "FIXTURE_MARKER"
	// phaseVar names the run in which blocker creates the marker and waits.
	phaseVar = "FIXTURE_PHASE"
	// expectedProcsVar is the GOMAXPROCS that procsTest expects.
	expectedProcsVar = "FIXTURE_PROCS"
	// alwaysVar makes killer end its binary whatever Add returns.
	alwaysVar = "FIXTURE_ALWAYS"
	// slowBuildVar is the shell pattern of the argument that makes the
	// wrapper of goWrapper wait slowBuild before it runs the go command.
	slowBuildVar = "FIXTURE_SLOW_BUILD"
	// childVar makes a fixture's test run as the copy of its binary that the
	// test starts.
	childVar = "FIXTURE_CHILD"
)

// slowBuild is how long the wrapper of goWrapper waits before a go command
// whose argument matches slowBuildVar's pattern, and ordinaryOutput is the
// pattern of the output of an ordinary control run's build.
const (
	slowBuild      = 20 * time.Second
	ordinaryOutput = "*/ordinary/*"
)

// The runs that phaseVar names.
const (
	openingPhase  = "opening"
	ordinaryPhase = "ordinary"
	mutantPhase   = "mutant"
	closingPhase  = "closing"
)

// The modes of the files that the tests write while a run runs, and of the
// go wrapper that TestMain writes before any test starts.
const (
	fileMode   = 0o644
	scriptMode = 0o755
)

// The time within which a fixture creates the marker of the step at which
// a test cancels the run, and the interval at which the test looks for it.
const (
	markerWait = 2 * time.Minute
	markerPoll = 10 * time.Millisecond
)

// goModFile is the name of a module's go.mod, and goMod the go.mod of every
// fixture that states none.
const (
	goModFile = "go.mod"
	goMod     = "module fixture\n\ngo 1.21\n"
)

// The names of the go wrapper. Its directory has the pattern
// wrapperPattern, and the wrapper takes on the name goCommand. Its file name
// is goName, because Windows runs a command only with the suffix .exe.
const (
	wrapperPattern = "gowrapper"
	goCommand      = "go"
	goName         = goCommand + process.ExeSuffix
)

// markerName is the name of the marker file of a test.
const markerName = "marker"

// definition is the vendored definition. protocol is its run protocol, whose
// variables the fixtures read, and annotation starts a line annotation of
// the Go overlay.
var (
	definition = spec.Load()
	protocol   = definition.Protocol
	annotation = definition.Overlay.Comment + definition.Catalogue.Annotation
)

// The pins of the reasons that the record states for a mutant without a
// run, which the cases of more than one step check.
const (
	cancelledReason = "the caller cancelled the run"
	tooLateReason   = "the caller's deadline leaves too little time for the mutant and the closing control run"
	openingReason   = "the caller's deadline leaves too little time for the opening control run"
)

// errorPrefix starts every error that Run returns.
const errorPrefix = "run: "

// missing is the name of a file or a directory that no fixture has.
const missing = "missing"

// The names of the fixtures' source files.
const (
	addFile       = "add.go"
	addTestFile   = "add_test.go"
	arithFile     = "arith.go"
	arithTestFile = "arith_test.go"
	countFile     = "count.go"
	countTestFile = "count_test.go"
	mainTestFile  = "main_test.go"
	brokenFile    = "broken.go"
)

// add is a package of one function.
const add = `package fixture

func Add(a, b int) int { return a + b }
`

// arith is a package with a constant, a function that its tests check, one
// that they call without checking, and one that they never call.
const arith = `package fixture

const Limit = 1 + 1

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

// arithTest checks Add and calls Sub.
const arithTest = `package fixture

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	Sub(1, 1)
}
`

// arithFiles is the module of arith and arithTest.
var arithFiles = map[string]string{arithFile: arith, arithTestFile: arithTest}

// arithVerdicts are the verdicts of arith's mutants under arithTest.
const arithVerdicts = `Add sbr-zero 0: killed [TestAdd]
Add aor 0: killed [TestAdd]
Sub sbr-zero 0: survived
Sub aor 0: survived
Unused sbr-zero 0: no-coverage
Unused aor 0: no-coverage
`

// arithNotRun are the verdicts of arith's mutants when no mutant runs.
const arithNotRun = `Add sbr-zero 0: not-run
Add aor 0: not-run
Sub sbr-zero 0: not-run
Sub aor 0: not-run
Unused sbr-zero 0: not-run
Unused aor 0: not-run
`

// addKilled are the verdicts of add's mutants under a test that checks Add.
const addKilled = "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n"

// addNotRun are the verdicts of add's mutants when no mutant runs.
const addNotRun = "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n"

// count is a package whose loop's increment mutant never ends the loop.
const count = `package fixture

func Count(n int) int {
	c := 0
	for i := 0; i < n; i++ {
		c++
	}
	return c
}
`

// countTest checks Count.
const countTest = `package fixture

import "testing"

func TestCount(t *testing.T) {
	if Count(3) != 3 {
		t.Error("Count(3) != 3")
	}
}
`

// countVerdicts are the verdicts of count's mutants under a test that
// checks Count.
const countVerdicts = `Count sbr-delete 0: killed [TestCount]
Count ror-boundary 0: killed [TestCount]
Count ror-false 0: killed [TestCount]
Count uoi-incdec 0: timed-out [TestCount]
Count uoi-incdec 1: killed [TestCount]
Count sbr-zero 0: killed [TestCount]
`

// brokenDecl is a declaration that does not compile, and broken a file of
// the package fixture that contains it.
const (
	brokenDecl = "var broken int = \"x\"\n"
	broken     = "package fixture\n\n" + brokenDecl
)

// slowOpening is a test of add whose opening control run sleeps half a
// second, which raises the deadline of a mutant's run above 7 seconds. The
// mutant runs do not sleep.
var slowOpening = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, render.TraceVar)

// procsTest is a test of add that fails unless its binary runs with
// GOMAXPROCS set to the value of expectedProcsVar.
var procsTest = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != os.Getenv(%q) {
		t.Fatal(os.Getenv(%[1]q))
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, procsVar, expectedProcsVar)

// ballastMiB is the memory that ballast touches in every run, in MiB.
const ballastMiB = 64

// ballast is a test of add that touches ballastMiB in every run, so the
// peak of its binary is above ballastMiB.
var ballast = fmt.Sprintf(`package fixture

import (
	"runtime"
	"testing"
)

func TestAdd(t *testing.T) {
	buf := make([]byte, %d<<20)
	for i := range buf {
		buf[i] = 1
	}
	runtime.KeepAlive(buf)
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, ballastMiB)

// tightMemory is a memory budget below the peak of each fixture's binary
// plus the protocol's constant, so each memory ceiling is that sum, and one
// worker fits.
const tightMemory = 100 << 20

// killer is a test of add that ends its own binary with SIGKILL when Add is
// wrong, or always when alwaysVar is set.
var killer = fmt.Sprintf(`package fixture

import (
	"os"
	"syscall"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 || os.Getenv(%q) != "" {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}
}
`, alwaysVar)

// blocker is a test of add that creates the file of markerVar and then
// waits a minute, in the run that phaseVar names.
var blocker = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	phase := %[1]q
	if os.Getenv(%[5]q) != "" {
		phase = %[2]q
	}
	if os.Getenv(%[6]q) != "0" {
		phase = %[3]q
	}
	if os.Getenv(%[7]q) == "" {
		phase = %[4]q
	}
	if phase == os.Getenv(%[8]q) {
		if err := os.WriteFile(os.Getenv(%[9]q), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, closingPhase, openingPhase, mutantPhase, ordinaryPhase,
	render.TraceVar, protocol.Variable, protocol.Instrumented, phaseVar, markerVar)

// scale is a module whose package's own test cannot tell a multiplication
// from a division, and whose package conformance tells them apart. The
// package other has tests that do not link the package.
var scale = map[string]string{
	"scale.go": "package fixture\n\nfunc Scale(x, factor int) int { return x * factor }\n",
	"scale_test.go": `package fixture

import "testing"

func TestScaleByOne(t *testing.T) {
	if Scale(2, 1) != 2 {
		t.Error("Scale(2, 1) != 2")
	}
}
`,
	conformanceFile: "package conformance\n",
	conformanceTestFile: `package conformance

import (
	"testing"

	"fixture"
)

func TestScale(t *testing.T) {
	if fixture.Scale(6, 3) != 18 {
		t.Error("Scale(6, 3) != 18")
	}
}
`,
	"other/other.go":      "package other\n",
	"other/other_test.go": "package other\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n",
}

// scaleTestFile is the name of scale's own test file.
const scaleTestFile = "scale_test.go"

// The import path of scale's package conformance, its pattern in the
// package's directory, and the names of its files.
const (
	conformance         = "fixture/conformance"
	conformancePattern  = "./conformance"
	conformanceFile     = "conformance/conformance.go"
	conformanceTestFile = "conformance/conformance_test.go"
	conformanceMainFile = "conformance/main_test.go"
)

// goWrapper is the directory of a go command that wraps the one that goVar
// names. The command is a copy of the test binary, which TestMain runs as
// the wrapper because its name is goName:
//   - It appends its GOMAXPROCS to the file of goLogVar where that variable
//     is set.
//   - For a command with an argument that matches the shell pattern of
//     blockVar, it creates the file of markerVar and waits a minute in place
//     of the go command.
//   - Before a command with an argument that matches the shell pattern of
//     slowBuildVar, it waits slowBuild.
//   - It runs the go command with its arguments and its standard streams,
//     and exits with the go command's status.
//
// TestMain writes it before any test starts, because a file that is open
// for writing while another goroutine forks a process fails exec with
// ETXTBSY.
var goWrapper string

// TestMain runs the go wrapper when the test binary's name is goName. It
// otherwise writes the wrapper, runs the tests and removes the wrapper.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == goName {
		os.Exit(wrapGo(os.Args[1:]))
	}
	dir, err := os.MkdirTemp("", wrapperPattern)
	if err == nil {
		goWrapper = dir
		err = copySelf(filepath.Join(dir, goName))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(goWrapper)
	os.Exit(code)
}

// copySelf copies the test binary to path, as a file that its owner may
// execute.
func copySelf(path string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, scriptMode)
}

// wrapGo runs as the go command of goWrapper with the arguments args, and
// returns its exit status.
func wrapGo(args []string) int {
	if log := os.Getenv(goLogVar); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
		if err == nil {
			_, err = fmt.Fprintln(f, os.Getenv(procsVar))
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if slices.ContainsFunc(args, func(arg string) bool { return matches(os.Getenv(blockVar), arg) }) {
		if err := os.WriteFile(os.Getenv(markerVar), nil, fileMode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		time.Sleep(time.Minute)
		return 0
	}
	if slices.ContainsFunc(args, func(arg string) bool { return matches(os.Getenv(slowBuildVar), arg) }) {
		time.Sleep(slowBuild)
	}
	//nolint:noctx // the go wrapper runs outside any test, and the run that started it ends it
	cmd := exec.Command(os.Getenv(goVar), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// matches reports whether arg matches the shell pattern pattern, whose *
// matches any text. It compares arg with slashes, so a pattern with slashes
// matches a path of Windows. An empty pattern matches no argument.
func matches(pattern, arg string) bool {
	if pattern == "" {
		return false
	}
	glob := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	return regexp.MustCompile(glob).MatchString(filepath.ToSlash(arg))
}

// useTempDir makes dir the temporary directory of the test process until t
// ends, through the variable of every platform.
func useTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range tempVars {
		t.Setenv(name, dir)
	}
}

// module writes texts, by slash-separated path, into a workspace of the
// test, with goMod as the go.mod unless texts has one, and returns the
// workspace's directory.
func module(t *testing.T, texts map[string]string) string {
	t.Helper()
	tree := files.Tree{goModFile: files.Text(goMod)}
	for name, text := range texts {
		tree[name] = files.Text(text)
	}
	return files.Workspace(t, tree)
}

// with returns a copy of texts with the files of more added or replaced.
func with(texts, more map[string]string) map[string]string {
	out := maps.Clone(texts)
	maps.Copy(out, more)
	return out
}

// runIn runs the engine on the package in dir with cfg, whose Dir it sets
// and whose Env it fills when it is nil, and returns the run's record.
func runIn(t *testing.T, dir string, cfg run.Config) *record.Record {
	t.Helper()
	return runWith(t, t.Context(), dir, cfg)
}

// runWith runs the engine as runIn does, with the context ctx.
func runWith(t *testing.T, ctx context.Context, dir string, cfg run.Config) *record.Record {
	t.Helper()
	cfg.Dir = dir
	if cfg.Env == nil {
		cfg.Env = os.Environ()
	}
	rec, err := run.Run(ctx, &cfg)
	assert.NoError(t, err, "Run returns a record")
	return rec
}

// cancelAt runs the engine on the package in dir with cfg, whose Env states
// marker as the file of markerVar or of the wrapper of goWrapper, and cancels
// the run once the file exists. It returns the run's record.
func cancelAt(t *testing.T, dir string, cfg run.Config, marker string) *record.Record {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		rec *record.Record
		err error
	}
	done := make(chan result, 1)
	cfg.Dir = dir
	go func() {
		rec, err := run.Run(ctx, &cfg)
		done <- result{rec, err}
	}()
	assert.Eventually(t, markerWait, markerPoll, func(tb assert.TB) {
		_, err := os.Stat(marker)
		assert.NoError(tb, err, "the marker exists")
	}, "the fixture creates the marker of the step at which the caller cancels")
	cancel()
	res := <-done
	assert.NoError(t, res.err, "Run returns a record")
	return res.rec
}

// blocking returns the environment of a run whose go command creates the
// file marker and then waits a minute, in place of each command with an
// argument that matches the shell pattern block, and returns marker.
func blocking(t *testing.T, block string) (env []string, marker string) {
	t.Helper()
	goCmd, err := exec.LookPath(goCommand)
	assert.NoError(t, err, "the go command is on the PATH")
	marker = filepath.Join(t.TempDir(), markerName)
	env = testbin.Setenv(
		os.Environ(),
		pathVar+"="+goWrapper+string(filepath.ListSeparator)+os.Getenv(pathVar),
		goVar+"="+goCmd,
		markerVar+"="+marker,
		blockVar+"="+block,
	)
	return env, marker
}

// slowBuilds returns the environment of a run whose go command waits
// slowBuild before each command with an argument that matches the shell
// pattern pattern.
func slowBuilds(t *testing.T, pattern string) []string {
	t.Helper()
	goCmd, err := exec.LookPath(goCommand)
	assert.NoError(t, err, "the go command is on the PATH")
	return testbin.Setenv(
		os.Environ(),
		pathVar+"="+goWrapper+string(filepath.ListSeparator)+os.Getenv(pathVar),
		goVar+"="+goCmd,
		slowBuildVar+"="+pattern,
	)
}

// verdicts writes one line per mutant: its scope, its kind, its position
// among the mutants of its scope and kind, its verdict, the tests that it
// names, and confirmed for a confirmed mutant.
func verdicts(rec *record.Record) string {
	var b strings.Builder
	seen := map[string]int{}
	for _, m := range rec.Mutants {
		id := m.Scope + " " + string(m.Kind)
		fmt.Fprintf(&b, "%s %d: %s", id, seen[id], m.Verdict)
		seen[id]++
		if len(m.Tests) > 0 {
			fmt.Fprintf(&b, " %v", m.Tests)
		}
		if m.Confirmed {
			b.WriteString(" confirmed")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// codes returns the codes of the record's run errors, in order.
func codes(rec *record.Record) []spec.ErrorCode {
	list := make([]spec.ErrorCode, 0, len(rec.Errors))
	for _, e := range rec.Errors {
		list = append(list, e.Code)
	}
	return list
}

// keys returns the keys of the record's mutants, in record order.
func keys(rec *record.Record) []string {
	list := make([]string, 0, len(rec.Mutants))
	for _, m := range rec.Mutants {
		list = append(list, m.Key)
	}
	return list
}

// cancelInPhase runs the engine on add with the test blocker and cfg, and
// cancels the run when blocker creates the marker in the run that phase
// names. It returns the run's record.
func cancelInPhase(t *testing.T, phase string, cfg run.Config) *record.Record {
	t.Helper()
	marker := filepath.Join(t.TempDir(), markerName)
	cfg.Env = append(os.Environ(), phaseVar+"="+phase, markerVar+"="+marker)
	return cancelAt(t, module(t, map[string]string{addFile: add, addTestFile: blocker}), cfg, marker)
}

// traceTruncator returns a test main of the package pkg that empties the
// trace file before the tests run, so the trace lacks the instrumentation's
// start mark. The instrumented package has the trace open while the tests
// run, and Windows does not remove an open file.
func traceTruncator(pkg string) string {
	return fmt.Sprintf(`package %s

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if trace := os.Getenv(%q); trace != "" {
		if err := os.Truncate(trace, 0); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
`, pkg, render.TraceVar)
}
