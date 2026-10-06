// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Why the engine ended a run.
const (
	endedMemory   = "memory"
	endedDeadline = "deadline"
	endedCancel   = "cancel"
	endedFailure  = "failure"
)

// backupDelay is how long after a run's deadline the engine ends the run's
// process tree, at most. The testing package's alarm cannot end a hang
// during package initialization, before the alarm starts.
const backupDelay = 5 * time.Second

// pollInterval is how often the engine reads a run's resident memory.
const pollInterval = 25 * time.Millisecond

// readDelay is how long the engine waits for a run's output to end after
// the test binary exited. A process that the binary started can hold the
// output open.
const readDelay = 5 * time.Second

// execution is one run of a test binary.
type execution struct {
	// err is why the run did not start, or nil for a run that started.
	err   error
	state *os.ProcessState
	// ended is why the engine ended the run, or empty when it did not.
	ended   string
	seconds float64
	output  Output
}

// execute runs bin in dir with env and args, in a temporary directory of
// its own under work that TMPDIR, TMP and TEMP name and that execute
// removes afterwards. It ends the run's process tree backup after timeout,
// when the run's resident memory crosses ceiling, where ceiling is
// positive, when ctx is done, and, when atFailure is true, when the output
// states the first failed test. -test.failfast starts no test after a
// failure, but lets the running tests and the paused parallel tests run to
// their end.
func execute(
	ctx context.Context,
	bin, dir, work string,
	env, args []string,
	timeout, backup time.Duration,
	ceiling int64,
	atFailure bool,
) *execution {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.WaitDelay = readDelay
	isolate(cmd)
	tmp, err := os.MkdirTemp(work, "tmp-")
	if err == nil {
		defer os.RemoveAll(tmp)
		cmd.Env = setenv(env, "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
		err = cmd.Start()
	}
	if err != nil {
		return &execution{err: err}
	}
	start := time.Now()
	ex := &execution{}
	var mu sync.Mutex
	exited := false
	end := func(why string) {
		mu.Lock()
		defer mu.Unlock()
		if !exited && ex.ended == "" {
			ex.ended = why
			killTree(cmd.Process)
		}
	}
	var failed func()
	if atFailure {
		failed = func() { end(endedFailure) }
	}
	scanned := make(chan Output, 1)
	go func() { scanned <- Scan(reader, failed) }()
	kill := time.AfterFunc(timeout+backup, func() { end(endedDeadline) })
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
				end(endedCancel)
				return
			case <-ticker.C:
				if ceiling > 0 && resident(cmd.Process.Pid) > ceiling {
					end(endedMemory)
					return
				}
			}
		}
	}()
	_ = cmd.Wait()
	ex.seconds = time.Since(start).Seconds()
	ex.state = cmd.ProcessState
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
	ex.output = <-scanned
	return ex
}

// setenv returns env with each of kv in place of every entry of its key: a
// KEY=value entry sets the key, and a KEY entry without = removes it. A Go
// program reads the first entry of a key, so a later entry alone would not
// take effect.
func setenv(env []string, kv ...string) []string {
	keys := map[string]bool{}
	for _, e := range kv {
		key, _, _ := strings.Cut(e, "=")
		keys[key] = true
	}
	out := make([]string, 0, len(env)+len(kv))
	for _, e := range env {
		if key, _, _ := strings.Cut(e, "="); !keys[key] {
			out = append(out, e)
		}
	}
	for _, e := range kv {
		if strings.Contains(e, "=") {
			out = append(out, e)
		}
	}
	return out
}
