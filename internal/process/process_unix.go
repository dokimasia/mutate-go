// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build unix

package process

import (
	"os"
	"os/exec"
	"syscall"
)

// ExeSuffix is the suffix of an executable's file name.
const ExeSuffix = ""

// isolate makes cmd start in a process group of its own, whose ID is the
// process's ID. It replaces cmd.SysProcAttr.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// kill sends SIGKILL to the process group of p. A group without a process
// is no error.
func kill(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
}
