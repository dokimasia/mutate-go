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
// package, and Go runs any other go command the same way.
//
// # Dependency position
//
// Imports bytes, context, encoding/json, errors, fmt, go/ast, go/importer,
// go/parser, go/token, go/types, io, os, os/exec, path/filepath, runtime,
// slices, sort, strconv and strings from the standard library. It imports
// no package from this module.
package load
