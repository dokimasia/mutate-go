// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package testbin runs a test binary once, under a deadline and a memory
// ceiling, and reads the progress of its tests from its output.
//
// [Run] starts the binary in a process group of its own, as
// internal/process runs a command, with a temporary directory of its own,
// and ends the group at the backup deadline, at the memory ceiling, when
// the caller's context ends, or at the first failed test. A run ends when
// the binary exits, and the processes that the binary left in its group end
// with it. [Scan] reads the output that the testing package writes under
// -test.v=test2json: the tests that started, failed, were running and timed
// out. [Flags], [Only] and [Setenv] build the flags and the environment of a
// run.
//
// # Platforms
//
// On Linux, Run reads a run's resident memory from procfs, and a run with a
// ceiling ends when it crosses it. Elsewhere the ceiling never ends a run.
// On Windows, Run ends the binary alone, because the standard library has
// no call that ends a process tree there.
//
// # Dependency position
//
// Imports bufio, bytes, context, io, os, os/exec, regexp, slices, strings,
// sync and time from the standard library, and internal/memory and
// internal/process from this module.
package testbin
