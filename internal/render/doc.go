// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package render writes a package's source with every runnable mutant
// behind a runtime switch, and the overlay that hands the source to the go
// command.
//
// Render replaces each site that has a runnable mutant with a form that
// switches on the active mutant, and adds a helper file that declares the
// switch. The test binary reads the active mutant's ordinal from the
// protocol's variable, DOKIMI_MUTATE_MUTANT, when the package initializes.
// When DOKIMI_MUTATE_TRACE names a file, the binary appends the line start
// to it when the package initializes, and the first ordinal of each site
// the first time that the site executes.
//
// A form writes each operand once, evaluates it as the active mutant's
// expression does, and adds no line, so every line of an instrumented file
// keeps its number, except when an increment's operand spans lines. No form
// passes a function value, so no form makes an operand escape. Render
// type-checks the instrumented package and gives up a site whose form the
// type checker rejects.
//
// Plain writes one mutant into its file without a switch, for the mutant's
// ordinary build. The mutant's source keeps the code that the mutant leaves
// out behind a constant that skips it, so a name that only that code uses
// remains in use. A test of a property of the build, such as an
// allocation count, runs against that build as against the package's own.
//
// # Dependency position
//
// Imports bytes, encoding/json, errors, fmt, go/ast, go/build/constraint,
// go/parser, go/scanner, go/token, os, path/filepath, sort, strconv and
// strings from the standard library, and internal/enumerate, internal/load
// and internal/spec from this module.
package render
