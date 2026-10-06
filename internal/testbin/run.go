// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package testbin

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"go.dokimi.dev/mutate/internal/memory"
)

// BackupDelay is how long after a run's deadline Run ends the run's process
// group, at most. The testing package's alarm cannot end a hang during the
// package's initialization, before the alarm starts.
const BackupDelay = 5 * time.Second

// pollInterval is how often Run reads a run's resident memory.
const pollInterval = 25 * time.Millisecond

// readDelay is how long Run waits for a run's output to end after the test
// binary exited. A process that the binary started can hold the output open.
const readDelay = 5 * time.Second

// The variables that name a run's temporary directory, for Unix and for
// Windows.
const (
	tmpdirVar = "TMPDIR"
	tmpVar    = "TMP"
	tempVar   = "TEMP"
)

// tmpPattern is the pattern of the name of a run's temporary directory.
const tmpPattern = "tmp-"

// system is the machine's file system, whose procfs Run reads a run's
// resident memory from.
var system = os.DirFS("/")

// Ending is why Run ended a run.
type Ending uint8

const (
	// Exited is a run that ended on its own.
	Exited Ending = iota
	// Memory is a run whose resident memory crossed its ceiling.
	Memory
	// Deadline is a run that lasted its deadline and the backup delay.
	Deadline
	// Cancelled is a run whose caller's context ended.
	Cancelled
	// Failed is a run whose output stated its first failed test.
	Failed
)

// Config states one run of a test binary.
type Config struct {
	// Binary is the path of the test binary.
	Binary string
	// Dir is the run's working directory: the directory of the binary's
	// package.
	Dir string
	// Work is the directory in which Run makes the run's temporary
	// directory.
	Work string
	// Env is the run's environment, and Args the binary's arguments.
	Env  []string
	Args []string
	// Timeout is the run's deadline, which the binary's -test.timeout
	// states. Backup after it, Run ends the run's process group.
	Timeout, Backup time.Duration
	// Ceiling is the run's memory ceiling in bytes, or 0 for none.
	Ceiling int64
	// StopAtFailure makes Run end the run at the first line of its output
	// that states a failed test. -test.failfast starts no test after a
	// failure, but lets the running tests and the paused parallel tests run
	// to their end.
	StopAtFailure bool
}

// Result is what one run of a test binary did.
type Result struct {
	// Err is why the run did not start, or nil for a run that started.
	Err error
	// State is the binary's exit state, or nil for a run that did not start.
	State *os.ProcessState
	// Ended is why Run ended the run.
	Ended Ending
	// Seconds is the run's wall time.
	Seconds float64
	// Output is what the run's output states about its tests.
	Output Output
}

// Run runs the test binary that cfg states, in a process group of its own,
// with a temporary directory of its own under cfg.Work that TMPDIR, TMP and
// TEMP name and that Run removes afterwards. It ends the process group
// cfg.Backup after cfg.Timeout, when the run's resident memory crosses
// cfg.Ceiling where that is positive, when ctx is done, and under
// cfg.StopAtFailure at the first failed test. It ends the processes that
// the binary started and left when the binary exits.
//
// A run that does not start, because its temporary directory or its
// process does not start, states the error in the result and nothing else.
//
// # Concurrency
//
// Run is safe for concurrent use. Each call runs its own process.
func Run(ctx context.Context, cfg Config) *Result {
	cmd := exec.Command(cfg.Binary, cfg.Args...)
	cmd.Dir = cfg.Dir
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.WaitDelay = readDelay
	isolate(cmd)
	tmp, err := os.MkdirTemp(cfg.Work, tmpPattern)
	if err == nil {
		defer os.RemoveAll(tmp)
		cmd.Env = Setenv(cfg.Env, tmpdirVar+"="+tmp, tmpVar+"="+tmp, tempVar+"="+tmp)
		err = cmd.Start()
	}
	if err != nil {
		return &Result{Err: err}
	}
	start := time.Now()
	res := &Result{}
	var mu sync.Mutex
	exited := false
	end := func(why Ending) {
		mu.Lock()
		defer mu.Unlock()
		if !exited && res.Ended == Exited {
			res.Ended = why
			killTree(cmd.Process)
		}
	}
	var failed func()
	if cfg.StopAtFailure {
		failed = func() { end(Failed) }
	}
	scanned := make(chan Output, 1)
	go func() { scanned <- Scan(reader, failed) }()
	kill := time.AfterFunc(cfg.Timeout+cfg.Backup, func() { end(Deadline) })
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				end(Cancelled)
				return
			case <-ticker.C:
				if cfg.Ceiling > 0 && memory.Resident(system, cmd.Process.Pid) > cfg.Ceiling {
					end(Memory)
					return
				}
			}
		}
	}()
	_ = cmd.Wait()
	res.Seconds = time.Since(start).Seconds()
	res.State = cmd.ProcessState
	kill.Stop()
	close(stop)
	<-watched
	mu.Lock()
	exited = true
	// The processes that the test binary started and left belong to the
	// run.
	killTree(cmd.Process)
	mu.Unlock()
	_ = writer.Close()
	res.Output = <-scanned
	return res
}
