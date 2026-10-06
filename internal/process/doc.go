// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package process runs a command in a process group of its own, so that
// ending the command ends every process that it started.
//
// [Start] starts a command in a group of its own, and copies the command's
// standard output and standard error through pipes of the package's own to
// the caller's writers. [Group.Kill] ends the group. [Group.Wait] returns
// when the command's process exits, whatever its output does, and ends the
// processes that the command left in its group. [Group.Drain] then waits a
// bounded time for the output to end, because a process that left the
// group, such as one that started a session of its own, can keep the
// output open.
//
// The test binaries of a run and the go command both run this way, so a
// test that leaves a process running, or a go command that runs through a
// wrapper script, ends with its group, and neither keeps its caller
// waiting.
//
// # Platforms
//
// On Unix, a group is a process group, and Kill sends SIGKILL to every
// process in it. On Windows, Kill ends the command's process alone, because
// the standard library has no call that ends a process tree there, and
// Drain waits its bound for the output of a process that the command left.
//
// # Dependency position
//
// Imports io, os, os/exec, syscall and time from the standard library. It
// imports no package from this module.
package process
