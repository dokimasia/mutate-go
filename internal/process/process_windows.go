// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package process

import (
	"os"
	"os/exec"
)

// ExeSuffix is the suffix of an executable's file name.
const ExeSuffix = ".exe"

// isolate leaves cmd as it is. The standard library has no call that ends
// a process tree on Windows.
func isolate(*exec.Cmd) {}

// kill kills p alone. A process that ended is no error.
func kill(p *os.Process) {
	_ = p.Kill()
}
