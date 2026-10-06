// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package process

import (
	"io"
	"os"
	"os/exec"
	"time"
)

// Group is a command that runs in a process group of its own, as [Start]
// started it. The caller calls Wait once, and then Drain once.
//
// # Concurrency
//
// Kill is safe for concurrent use, also while Wait or Drain runs. Wait and
// Drain run on one goroutine, in that order.
//
// # Allocation contract
//
// A group allocates its pipes, one goroutine per pipe that copies the
// output, and the channel that the goroutines signal on.
type Group struct {
	cmd *exec.Cmd
	// reads contains the read end of each pipe of the output, and copied
	// receives one value per pipe when the copy of its output ends.
	reads  []*os.File
	copied chan struct{}
}

// Start starts cmd in a process group of its own, and copies its standard
// output to stdout and its standard error to stderr through pipes of the
// package's own. When stdout and stderr are one writer, one pipe carries
// both, in the order of the command's writes. Start sets cmd.Stdout,
// cmd.Stderr and cmd.SysProcAttr, and compares stdout with stderr, so their
// dynamic types are comparable, as pointers are.
//
// The command's process gets a copy of each pipe's write end, and every
// process that it starts inherits the copy. A pipe's output ends when the
// last of these processes exits.
//
// Start returns the error of a pipe that does not open, or of a command
// that does not start, as exec.Cmd.Start returns it. It then closes every
// pipe that it opened.
func Start(cmd *exec.Cmd, stdout, stderr io.Writer) (*Group, error) {
	g := &Group{cmd: cmd}
	writers := []io.Writer{stdout}
	if stderr != stdout {
		writers = append(writers, stderr)
	}
	var writes []*os.File
	var err error
	for i := 0; i < len(writers) && err == nil; i++ {
		var r, w *os.File
		if r, w, err = os.Pipe(); err == nil {
			g.reads, writes = append(g.reads, r), append(writes, w)
		}
	}
	if err == nil {
		cmd.Stdout, cmd.Stderr = writes[0], writes[len(writes)-1]
		isolate(cmd)
		err = cmd.Start()
	}
	for _, w := range writes {
		_ = w.Close()
	}
	if err != nil {
		for _, r := range g.reads {
			_ = r.Close()
		}
		return nil, err
	}
	g.copied = make(chan struct{}, len(writers))
	for i, w := range writers {
		go func() {
			// A copy ends at the end of the output, or when Drain closes the
			// read end.
			_, _ = io.Copy(w, g.reads[i])
			g.copied <- struct{}{}
		}()
	}
	return g, nil
}

// Wait waits for the command's process to exit, and then ends the
// processes that remain in its group, which keep the output open. It
// returns when the process exits, whatever the output does, with the error
// of exec.Cmd.Wait: nil for a command that exits with the status 0, and an
// *exec.ExitError for any other exit, a kill included. The command's
// ProcessState is set when Wait returns.
func (g *Group) Wait() error {
	err := g.cmd.Wait()
	g.Kill()
	return err
}

// Drain waits for the end of the output after Wait, at most for delay, and
// closes the pipes. A process that left the group can keep the output open
// past the delay. Drain then closes the pipes' read ends, which ends the
// copies, and the output that the process writes later is lost. When Drain
// returns, every copy to stdout and stderr has ended.
func (g *Group) Drain(delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for copied := 0; copied < len(g.reads); {
		select {
		case <-g.copied:
			copied++
		case <-timer.C:
			for _, r := range g.reads {
				_ = r.Close()
			}
		}
	}
	for _, r := range g.reads {
		_ = r.Close()
	}
}

// Kill ends the group: the command's process and each process that the
// command started and that remains in the group. Kill of a group that
// ended does nothing.
func (g *Group) Kill() {
	kill(g.cmd.Process)
}
