// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build unix

package run

import (
	"os"
	"os/exec"
	"syscall"
)

// exeSuffix is the suffix of an executable's file name.
const exeSuffix = ""

// isolate starts cmd in a process group of its own, so killTree reaches
// every process that the test binary starts.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree sends SIGKILL to the process group of p.
func killTree(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
}
