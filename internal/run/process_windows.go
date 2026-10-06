// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"os/exec"
)

// exeSuffix is the suffix of an executable's file name.
const exeSuffix = ".exe"

// isolate leaves cmd as it is. The standard library has no call that ends
// a process tree on Windows.
func isolate(*exec.Cmd) {}

// killTree kills p alone.
func killTree(p *os.Process) {
	_ = p.Kill()
}
