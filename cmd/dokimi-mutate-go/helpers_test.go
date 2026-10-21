// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert/files"
	"go.stealthscale.io/cli/clitest"

	"go.dokimi.dev/mutate/internal/process"
	"go.dokimi.dev/mutate/internal/run"
)

// mainVar is the variable of the environment that makes the test binary
// run main in place of its tests. go test merges the coverage of that
// process into the profile.
const mainVar = "MUTATE_TEST_MAIN"

// The variables of the fixtures' environment.
const (
	// fakeVar makes the fake go command of fakeGoDir fail when it is
	// fakeFail, and print text that is no JSON otherwise.
	fakeVar  = "FAKE_GO"
	fakeFail = "fail"
	// failVar makes arithTest's TestAdd fail.
	failVar = "FIXTURE_FAIL"
	// ordinaryVar makes arithTest's TestSub check Sub in an ordinary build.
	ordinaryVar = "FIXTURE_ORDINARY"
	// expectedProcsVar is the GOMAXPROCS that procsTest expects.
	expectedProcsVar = "FIXTURE_PROCS"
	// goVar names the go command that the fake go command of fakeGoDir runs,
	// and goLogVar the file to which the fake first appends its GOMAXPROCS.
	goVar    = "FIXTURE_GO"
	goLogVar = "FIXTURE_GO_LOG"
)

// The variables of the environment that the tests set or read.
const (
	pathVar     = "PATH"
	maxProcsVar = "GOMAXPROCS"
)

// optionVarPrefix starts the environment variable of each option of the
// command, which go.stealthscale.io/cli derives from the command's name.
const optionVarPrefix = "DOKIMI_MUTATE_GO_"

// tempVars are the variables that name the temporary directory of a
// process: TMPDIR on Unix, and TMP and TEMP on Windows.
var tempVars = []string{"TMPDIR", "TMP", "TEMP"}

// The modes of the fake go command, which TestMain writes before any test
// starts, and of the file to which the fake appends.
const (
	commandMode = 0o755
	fileMode    = 0o644
)

// The names of the fake go command. Its directory has the pattern
// fakePattern, and the fake takes on the name goCommand. Its file name is
// goName, because Windows runs a command only with the suffix .exe.
const (
	fakePattern = "fakego"
	goCommand   = "go"
	goName      = goCommand + process.ExeSuffix
)

// The names of the fixtures' files.
const (
	goModFile     = "go.mod"
	addFile       = "add.go"
	addTestFile   = "add_test.go"
	arithFile     = "arith.go"
	arithTestFile = "arith_test.go"
)

// goMod is the go.mod of every fixture.
const goMod = "module fixture\n\ngo 1.21\n"

// fixture is the import path of the fixtures' package.
const fixture = "fixture"

// fakeGoDir is the directory of a fake go command. The command is a copy of
// the test binary, which TestMain runs as the fake because its name is
// goName:
//   - Where goVar is set, it appends its GOMAXPROCS to the file of goLogVar,
//     runs the go command of goVar with its arguments and its standard
//     streams, and exits with the go command's status.
//   - Otherwise it writes "no list today" to its standard error and exits
//     with the status 1 when fakeVar is fakeFail, and prints text that is
//     no JSON when it is not.
//
// TestMain writes it before any test starts, because a file that is open
// for writing while another goroutine forks a process fails exec with
// ETXTBSY.
var fakeGoDir string

// TestMain runs the fake go command when the test binary's name is goName,
// and main when mainVar is set. It otherwise writes the fake go command, runs
// the tests and removes the fake.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == goName {
		os.Exit(runFakeGo(os.Args[1:]))
	}
	if os.Getenv(mainVar) != "" {
		main()
	}
	dir, err := os.MkdirTemp("", fakePattern)
	if err == nil {
		fakeGoDir = dir
		err = copySelf(filepath.Join(dir, goName))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(fakeGoDir)
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
	return os.WriteFile(path, data, commandMode)
}

// runFakeGo runs as the fake go command of fakeGoDir with the arguments args,
// and returns its exit status.
func runFakeGo(args []string) int {
	goCmd := os.Getenv(goVar)
	if goCmd == "" {
		if os.Getenv(fakeVar) == fakeFail {
			fmt.Fprintln(os.Stderr, "no list today")
			return 1
		}
		fmt.Fprintln(os.Stdout, "{not json")
		return 0
	}
	f, err := os.OpenFile(os.Getenv(goLogVar), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err == nil {
		_, err = fmt.Fprintln(f, os.Getenv(maxProcsVar))
		err = errors.Join(err, f.Close())
	}
	if err == nil {
		//nolint:noctx // the fake go command runs outside any test, and the run that started it ends it
		cmd := exec.Command(goCmd, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err = cmd.Run()
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// arith is a package with a function that its tests check, one that they
// call without checking, and one that they never call.
const arith = `package fixture

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

// arithTest checks Add, and fails when failVar is set. It calls Sub, and
// checks it in an ordinary build when ordinaryVar is set.
var arithTest = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%q) != "" {
		t.Fatal("the fixture fails")
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	if os.Getenv(%q) != "" && os.Getenv(%q) == "" && Sub(5, 3) != 2 {
		t.Error("Sub(5, 3) != 2 in the ordinary build")
	}
	Sub(1, 1)
}
`, failVar, ordinaryVar, definition.Protocol.Instrumented)

// arithFiles is the module of arith and arithTest.
var arithFiles = map[string]string{arithFile: arith, arithTestFile: arithTest}

// arithLines is the output of a run on arithFiles: the mutants without
// coverage, whose verdicts are final when the opening control run ends,
// then the survivors in the order of their keys, and the summary.
const arithLines = `arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)
arith.go:7:33: not covered: x * 2 became x / 2 (aor)
arith.go:5:26: survived: return a - b became return 0 (sbr-zero)
arith.go:5:33: survived: a - b became a + b (aor)
fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered
`

// add is a package of one function, and addTest its test.
const (
	add     = "package fixture\n\nfunc Add(a, b int) int { return a + b }\n"
	addTest = `package fixture

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`
)

// addFiles is the module of add and addTest.
var addFiles = map[string]string{addFile: add, addTestFile: addTest}

// addKilled is the summary of a run on addFiles.
const addKilled = "fixture: 2 of 2 mutants detected (100%): 2 killed\n"

// twoPackages is the module of add and of a copy of add in the package sub.
var twoPackages = map[string]string{
	addFile:           add,
	addTestFile:       addTest,
	"sub/add.go":      strings.Replace(add, fixture, "sub", 1),
	"sub/add_test.go": strings.Replace(addTest, fixture, "sub", 1),
}

// procsTest is a test of add that fails unless its binary runs with
// GOMAXPROCS set to the value of expectedProcsVar.
var procsTest = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%[1]q) != os.Getenv(%[2]q) {
		t.Fatal("the threads are " + os.Getenv(%[1]q))
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`, maxProcsVar, expectedProcsVar)

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

// enter writes the module of texts as module does. The module is the
// working directory of the test process until t ends.
func enter(t *testing.T, texts map[string]string) string {
	t.Helper()
	dir := module(t, texts)
	t.Chdir(dir)
	return dir
}

// useThreads sets the GOMAXPROCS of the test process to n until t ends. The
// shares that a case expects then do not depend on the machine.
func useThreads(t *testing.T, n int) {
	t.Helper()
	previous := runtime.GOMAXPROCS(n)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
}

// testSession returns a session whose runs have the environment env and
// write to stdout and stderr, with the records in records.
func testSession(env []string, stdout, stderr io.Writer, records string) *session {
	return &session{
		cfg:      run.Config{Env: env, Workers: 1},
		out:      &output{stdout: stdout, stderr: stderr},
		progress: &progress{stderr: io.Discard, every: time.Hour, now: time.Now},
		records:  records,
	}
}

// call runs the command line args with the environment that environ returns
// and the empty standard input, without a default memory budget, and
// returns the result of the run. It does not write a progress line.
func call(t *testing.T, args ...string) clitest.Result {
	t.Helper()
	c := &cli{every: time.Hour, now: time.Now}
	return clitest.Main(t.Context(), c.main, &clitest.Input{Args: args, Vars: environ()})
}

// environ returns the environment of the test process without the
// variables of the command's options, so a variable of the machine does not
// set an option of a run.
func environ() map[string]string {
	vars := map[string]string{}
	for _, kv := range os.Environ() {
		if key, value, ok := strings.Cut(kv, "="); ok && !strings.HasPrefix(key, optionVarPrefix) {
			vars[key] = value
		}
	}
	return vars
}
