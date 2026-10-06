// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package spec reads the part of the vendored mutate-spec definition that
// the engine needs when it runs: the operator catalogue, the run protocol
// and the Go overlay.
//
// The vendored definition is in conformance/spec. make spec-sync copies
// the files that the engine reads into this package, which embeds them, so
// a run reads no file of the repository or of mutate-spec. The package
// states the files as Go values and adds no rule of its own.
//
// # Dependency position
//
// Imports embed, encoding/json and strings from the standard library. It
// imports no package from this module.
package spec
