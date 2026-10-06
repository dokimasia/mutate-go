// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package load reads one Go package the way the go command builds it, and
// type-checks it with the standard library.
//
// Load runs go list in the package directory with the caller's
// environment, so GOFLAGS, GOTOOLCHAIN and go.work select what an ordinary
// go test there selects. It type-checks the files that go list reports in
// CompiledGoFiles, and reads every dependency from the export data that go
// list writes. Linking lists the other packages whose test binaries link a
// package, List lists the packages that patterns name, and Go runs any
// other go command the same way.
//
// # go list
//
// Every call of go list runs with -e, and asks for the JSON fields that
// the caller's entry type declares, so the fields that go list writes are
// the fields that the caller reads. go list states the error of a package
// that does not list in the package's entry, and [Listed.Problem] applies
// one rule to it: the package's own error first, and then the errors of its
// dependencies.
//
// # Dependency position
//
// Imports bytes, cmp, context, encoding/json, errors, fmt, go/ast,
// go/importer, go/parser, go/token, go/types, go/version, io, os, os/exec,
// path/filepath, reflect, runtime, slices, strconv, strings and time from
// the standard library, and internal/process from this module.
package load
