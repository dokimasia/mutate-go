// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package report writes the text of a mutation run's record: the line of a
// mutant, the notes on a run, and the summary of a run, and the lines of a
// listing, which states the mutants that a run would test. The command and
// Check write the same text from it.
//
// # Line format
//
// A mutant's line starts with its position, as a compiler's message does,
// so an editor links it to the source:
//
//	wire/codec.go:41:9: survived: n + 1 became n - 1 (aor)
//	wire/codec.go:41:9: to test: n + 1 becomes n - 1 (aor)
//
// # Dependency position
//
// Imports fmt, strconv and strings from the standard library, and
// internal/record and internal/spec from this module.
package report
