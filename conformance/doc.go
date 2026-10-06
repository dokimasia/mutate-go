// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package conformance runs the corpus of the vendored mutate-spec definition
// through the engine. The package contains only this documentation and its
// test. The test copies the Go fixture of each case into a temporary
// directory, runs the engine there with the options of the case, and
// compares the record with the case's case.json and expect.json.
//
// # Dependency position
//
// Its test imports bytes, context, encoding/json, fmt, os, path,
// path/filepath, slices and testing from the standard library,
// go.dokimi.dev/assert and go.dokimi.dev/assert/expect, and internal/record,
// internal/run, internal/selection and internal/spec from this module.
package conformance
