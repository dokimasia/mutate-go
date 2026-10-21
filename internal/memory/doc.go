// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

// Package memory reads the memory that the process may use, the resident
// memory of the processes of a process group, and the peak resident memory
// of a process that exited.
//
// [Limit] and [Group.Resident] read Linux's procfs and sysfs under the file
// system root that the caller passes, os.DirFS("/") for the machine's own,
// so a test can pass files of its own. On a platform without procfs, each
// returns 0. [Peak] reads the resource usage that wait4 reports on Linux,
// and returns nil elsewhere.
//
// # Concurrency
//
// [Limit] and [Peak] are safe for concurrent use. Each reads the files of
// the root at the time of the call. A [Group] is not safe for concurrent
// use.
//
// # Dependency position
//
// Imports io/fs, os, path, strconv, strings and syscall from the standard
// library. It imports no package from this module.
package memory
