// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package selection reads the lines that a run selects: an entry of the
// command's -lines, the entries of the variable that mutate.Check reads,
// and the lines that a unified diff changes.
//
// A selection is a list of [Lines], each a range of lines of a file at an
// absolute path. [ParseEntry] reads one entry, file:first-last, whose path
// is relative to the working directory. [ParseList] reads entries separated
// by commas, as the variable [Var] states them. [ParseDiff] reads a unified
// diff, such as the output of git diff, and selects the lines that it adds
// and the lines on either side of the lines that it removes.
//
// # Errors
//
// Every error starts with the name of this package and states the entry or
// the line of the diff that does not parse, or the file that the diff does
// not match.
//
// # Dependency position
//
// Imports fmt, maps, os, path/filepath, slices, strconv and strings from
// the standard library. It imports no package from this module.
package selection
