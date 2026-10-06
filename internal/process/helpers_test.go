// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package process_test

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.dokimi.dev/assert"
)

// fakeVar is the variable of the environment that names the behaviour that
// the test binary takes on in place of its tests.
const fakeVar = "PROCESS_FAKE"

// The behaviours of the fake command.
const (
	// writes prints outLine to its standard output and then errLine to its
	// standard error.
	writes = "writes"
	// exits3 exits with the status 3.
	exits3 = "exits3"
	// sleeps waits a minute.
	sleeps = "sleeps"
	// leaves starts a copy of itself that sleeps in its process group,
	// writes the copy's process ID, and exits.
	leaves = "leaves"
	// waits starts a copy of itself that sleeps in its process group,
	// writes the copy's process ID, and sleeps.
	waits = "waits"
)

// The lines that the fake command writes.
const (
	outLine = "out\n"
	errLine = "err\n"
)

// The limits of the tests: a delay that a drain which ends on its own
// never reaches, and the time within which a group that Kill or Wait ends
// stops.
const (
	longDelay = time.Minute
	ended     = 10 * time.Second
)

// TestMain runs the fake command when fakeVar names a behaviour, and the
// package's tests otherwise.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeVar); mode != "" {
		os.Exit(fake(mode))
	}
	os.Exit(m.Run())
}

// fake takes on the behaviour mode, or a behaviour of platformFakes, and
// returns the exit status: 2 for a behaviour that neither names.
func fake(mode string) int {
	switch mode {
	case writes:
		fmt.Print(outLine)
		fmt.Fprint(os.Stderr, errLine)
	case exits3:
		return 3
	case sleeps:
		time.Sleep(time.Minute)
	case leaves:
		return startChild(nil)
	case waits:
		if startChild(nil) != 0 {
			return 1
		}
		time.Sleep(time.Minute)
	default:
		if f, ok := platformFakes[mode]; ok {
			return f()
		}
		return 2
	}
	return 0
}

// startChild starts a copy of the fake command that sleeps, with the
// process attributes attr and the fake command's standard output, writes
// the copy's process ID, and returns 0, or 1 when the copy does not start.
func startChild(attr *syscall.SysProcAttr) int {
	child := exec.Command(os.Args[0])
	child.Env, child.Stdout, child.SysProcAttr = append(os.Environ(), fakeVar+"="+sleeps), os.Stdout, attr
	if child.Start() != nil {
		return 1
	}
	fmt.Println(child.Process.Pid)
	return 0
}

// command returns the command that runs the test binary as the fake
// command with the behaviour mode.
func command(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeVar+"="+mode)
	return cmd
}

// childPID returns the process ID that a fake command wrote on the first
// line of out.
func childPID(t *testing.T, out string) int {
	t.Helper()
	line, _, _ := strings.Cut(out, "\n")
	pid, err := strconv.Atoi(line)
	assert.NoError(t, err, "the fake command writes its child's process ID: "+out)
	return pid
}
