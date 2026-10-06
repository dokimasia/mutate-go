// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package enumerate lists a loaded package's mutants under the operator
// catalogue and the Go overlay.
//
// Enumerate finds every site of a catalogue class in the package's own
// code, and in its generated files when the caller includes them, makes the
// mutants of each kind that applies there, and computes each mutant's key.
// It lists the sites that the overlay's rules skip. It marks the mutants
// that an annotation suppresses, and those that a rule family suppresses by
// its APIs, its method rules, its result rules and its argument rules. It
// also marks the mutants that the compiler rejects because they divide an
// integer by a constant 0, and those outside the run's selection. It states,
// for each site, the facts that an instrumented form of the site needs.
//
// # Cost
//
// The walk visits each node of a file once. A site finds the rule family
// that suppresses it in an index of the file's suppressions, with a binary
// search and a walk up the suppressions that nest around it, and its line
// in an index of the selection with a binary search. On 16,000 logging calls
// in one function, the listing of the package took 0.19 s.
//
// # Dependency position
//
// Imports cmp, crypto/sha256, encoding/hex, fmt, go/ast, go/constant,
// go/scanner, go/token, go/types, math, slices, sort, strconv and strings
// from the standard library, and internal/load and internal/spec from this
// module.
package enumerate
