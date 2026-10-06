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
// # Dependency position
//
// Imports crypto/sha256, encoding/hex, fmt, go/ast, go/constant,
// go/scanner, go/token, go/types, slices, sort, strconv and strings from
// the standard library, and internal/load and internal/spec from this
// module.
package enumerate
