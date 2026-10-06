// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package conformance holds the test that runs the corpus of the vendored
// mutate-spec definition through the engine. The package has no code: its
// test copies the Go fixture of each case into a temporary directory, runs
// the engine there, and compares the record with the case's case.json and
// expect.json.
//
// # Dependency position
//
// Its test imports context, encoding/json, fmt, io/fs, os, path/filepath,
// slices, sort, strconv, strings and testing from the standard library, and
// internal/record and internal/run from this module.
package conformance
