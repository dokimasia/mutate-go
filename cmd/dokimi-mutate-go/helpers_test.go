// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"
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
)

// The variables of the environment that the tests set or read.
const (
	pathVar     = "PATH"
	tmpVar      = "TMPDIR"
	maxProcsVar = "GOMAXPROCS"
)

// The modes of the fixtures' directories and files.
const (
	dirMode    = 0o755
	fileMode   = 0o644
	scriptMode = 0o755
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

// fakeGoDir contains a go command that fails when fakeVar is fakeFail, and
// prints text that is no JSON otherwise. TestMain writes it before any test
// starts, because a file open for writing while another goroutine forks a
// process fails exec with ETXTBSY.
var fakeGoDir string

// TestMain runs main when mainVar is set, and otherwise writes the fake go
// command, runs the tests and removes the fake go command.
func TestMain(m *testing.M) {
	if os.Getenv(mainVar) != "" {
		main()
	}
	dir, err := os.MkdirTemp("", "fakego")
	if err == nil {
		fakeGoDir = dir
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$%s\" = %s ]; then echo \"no list today\" >&2; exit 1; fi\n"+
			"echo \"{not json\"\n", fakeVar, fakeFail)
		err = os.WriteFile(filepath.Join(dir, "go"), []byte(script), scriptMode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
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

// module writes files, by slash-separated path, and goMod into a new
// directory, and returns the directory.
func module(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		assert.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode), "the fixture's directory is made")
		assert.NoError(t, os.WriteFile(path, []byte(text), fileMode), "the fixture's file is written")
	}
	assert.NoError(t, os.WriteFile(filepath.Join(dir, goModFile), []byte(goMod), fileMode), "the go.mod is written")
	return dir
}

// with returns a copy of files with the files of more added or replaced.
func with(files, more map[string]string) map[string]string {
	out := maps.Clone(files)
	maps.Copy(out, more)
	return out
}

// enter writes the module of files, as module does, and makes it the
// working directory of the test process until t ends.
func enter(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := module(t, files)
	t.Chdir(dir)
	return dir
}

// call runs the command line args with the empty standard input and without
// a default memory budget, and returns its exit status, its standard output
// and its standard error. It writes no progress line.
func call(args ...string) (status int, stdout, stderr string) {
	var out, errs bytes.Buffer
	c := &cli{stdin: strings.NewReader(""), stdout: &out, stderr: &errs, every: time.Hour, now: time.Now}
	status = c.main(context.Background(), args)
	return status, out.String(), errs.String()
}
