// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package run runs mutation testing on one Go package by the run protocol,
// and returns the run's record.
//
// Run loads the package, enumerates its mutants, instruments it and builds
// its test binary once, and the test binary of each other package of the
// suite that the caller names. It runs each binary once with no mutant
// active and a trace on, runs the binaries that executed a covered
// mutant's site with that mutant alone active, and runs each binary once
// more with no mutant active. Each run is a fresh process with a temporary
// directory of its own, bounded by its binary's deadline and, on Linux, its
// binary's memory ceiling. Run compares the data files of the suite's
// packages before and after the runs. A package without a mutant to run
// builds and runs nothing.
//
// Where a binary has fewer top-level tests than mutants to run, Run runs
// each of its tests alone with a trace on, and a mutant's run of the binary
// starts with the tests that executed the mutant's site. That part ends at
// a deadline from those tests' own times. The record lists those tests in
// each mutant's coveredBy.
//
// The record states the ranges of the caller's selection that lie in the
// package's files, and each generated file of the package that the run
// leaves out or, at the caller's request, includes. A caller's limit on the
// number of mutant runs ends the runs without failing the run, and the
// run's score is then the score of that sample.
//
// When the caller asks for confirmation, Run also builds the binaries from
// the package's unchanged source and runs them once, and then builds and
// runs each survivor, and each mutant without coverage, alone as ordinary
// source, without the instrumented variable, for the mutant's verdict.
//
// # Dependency position
//
// Imports context, crypto/sha256, encoding/hex, fmt, io/fs, os,
// path/filepath, runtime, runtime/debug, slices, strconv, strings, sync,
// syscall and time from the standard library, and internal/enumerate,
// internal/load, internal/memory, internal/process, internal/record,
// internal/render, internal/selection, internal/spec and internal/testbin
// from this module.
package run
