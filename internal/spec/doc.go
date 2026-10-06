// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package spec reads the part of the vendored mutate-spec definition that
// the engine needs when it runs, the operator catalogue, the run protocol
// and the Go overlay, and declares their vocabularies as Go types.
//
// The vendored definition is in conformance/spec. make spec-sync copies the
// files that the engine reads into this package, which embeds them, so a
// run reads no file of the repository or of mutate-spec. [Load] decodes them
// once per process. The package states the files as Go values and adds no
// rule of its own.
//
// # Vocabularies
//
// Each value that the definition names and the engine stores has a type:
// [Kind], [Class] and [Family] from the catalogue, [Verdict], [ScoreClass]
// and [ErrorCode] from the protocol, and [SkipReason] from the overlay. Each
// encodes as its string, so a record's JSON spells the definition's values.
//
//   - Kind, Verdict, ScoreClass, ErrorCode and SkipReason declare a constant
//     for every value of the definition, because the engine writes or
//     branches on single values. The tests of this package fail when the
//     constants and the definition differ in either direction.
//   - Class and Family declare no constant. The engine reads their values
//     from the catalogue and branches on none of them.
//
// # Concurrency
//
// [Load] is safe for concurrent use. The [Definition] that it returns shares
// its maps and slices with every caller, so a caller reads it and does not
// change it.
//
// # Dependency position
//
// Imports embed, encoding/json, strings and sync from the standard library.
// It imports no package from this module.
package spec
