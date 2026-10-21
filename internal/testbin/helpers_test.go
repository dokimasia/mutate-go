// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package testbin_test

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"go.dokimi.dev/assert"
)

// The variables of the environment of the fake test binary: the behaviour
// that the test binary takes on in place of its tests, the file into which
// the binary in the mode orphan writes its child's process ID, and the mark
// that the test process sets for every process that its tests start.
const (
	fakeVar    = "TESTBIN_FAKE"
	pidFileVar = "TESTBIN_PID_FILE"
	childVar   = "TESTBIN_CHILD"
)

// The behaviours of the fake test binary.
const (
	// passes writes the framed output of a test that passes and logs a line.
	passes = "passes"
	// exits3 exits with the status 3 and writes nothing.
	exits3 = "exits3"
	// failsAndHangs writes the framed output of a test that fails, and then
	// waits a minute.
	failsAndHangs = "fails-and-hangs"
	// hangs waits a minute.
	hangs = "hangs"
	// grows touches 256 MiB of memory, and then waits a minute.
	grows = "grows"
	// growsInChildren starts two copies of itself in the mode grows, and
	// waits for them.
	growsInChildren = "grows-in-children"
	// environment writes its working directory and the variables of its
	// temporary directory.
	environment = "environment"
	// orphan starts a copy of itself that hangs and keeps its output open,
	// writes the copy's process ID into the file of pidFileVar, and exits.
	orphan = "orphan"
)

// The lines that the fake test binary writes.
const (
	logLine  = "    a_test.go:5: a line that the test writes"
	passing  = "\x16=== RUN   TestA\n" + logLine + "\n\x16--- PASS: TestA (0.00s)\n\x16PASS\n"
	failing  = "\x16=== RUN   TestA\n\x16--- FAIL: TestA (0.00s)\n"
	grownMiB = 256
)

// TestMain runs the fake test binary when fakeVar names a behaviour. A copy
// of the test binary that a test starts without a behaviour has childVar
// set, and exits with the status 2 before any test runs. TestMain otherwise
// sets childVar for every process that the tests start, and runs the
// package's tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeVar); mode != "" {
		os.Exit(fake(mode))
	}
	if os.Getenv(childVar) != "" {
		fmt.Fprintln(os.Stderr, "a test started the test binary without a fake behaviour")
		os.Exit(2)
	}
	if err := os.Setenv(childVar, "1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// fake takes on the behaviour mode, and returns the exit status.
func fake(mode string) int {
	switch mode {
	case passes:
		fmt.Fprint(os.Stdout, passing)
	case exits3:
		return 3
	case failsAndHangs:
		fmt.Fprint(os.Stdout, failing)
		time.Sleep(time.Minute)
	case hangs:
		time.Sleep(time.Minute)
	case grows:
		grown := make([]byte, grownMiB<<20)
		for i := range grown {
			grown[i] = 1
		}
		time.Sleep(time.Minute)
	case growsInChildren:
		self, _ := os.Executable()
		var children []*exec.Cmd
		for range 2 {
			//nolint:noctx // the fake test binary runs outside any test, so no context ends its children
			child := exec.Command(self)
			child.Env = append(os.Environ(), fakeVar+"="+grows)
			if child.Start() != nil {
				return 1
			}
			children = append(children, child)
		}
		for _, child := range children {
			_ = child.Wait()
		}
	case environment:
		wd, _ := os.Getwd()
		fmt.Fprintf(os.Stdout, "%s\n%s\n%s\n%s\n", wd, os.Getenv("TMPDIR"), os.Getenv("TMP"), os.Getenv("TEMP"))
	case orphan:
		self, _ := os.Executable()
		//nolint:noctx // the fake test binary runs outside any test, so no context ends its child
		child := exec.Command(self)
		child.Env, child.Stdout = append(os.Environ(), fakeVar+"="+hangs), os.Stdout
		if child.Start() != nil {
			return 1
		}
		pid := []byte(strconv.Itoa(child.Process.Pid))
		if os.WriteFile(os.Getenv(pidFileVar), pid, fileMode) != nil {
			return 1
		}
	}
	return 0
}

// fileMode is the mode of the files that the tests write.
const fileMode = 0o644

// binary returns the path of the test binary, which TestMain makes the fake
// test binary.
func binary(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	assert.NoError(t, err, "the test binary's path reads")
	return self
}

// fakeEnv returns the environment of the test process with the fake test
// binary's behaviour mode, and the variables more.
func fakeEnv(mode string, more ...string) []string {
	return append(append(os.Environ(), fakeVar+"="+mode), more...)
}
