// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package mutate measures a Go package's tests by mutation. It runs the
// tests against each change to the package's code that the operator
// catalogue defines, and reports every change that the tests do not
// detect.
//
// A package opts in with a test file whose build constraint an ordinary
// go test run does not satisfy:
//
//	//go:build mutation
//
//	package btree_test
//
//	import (
//		"testing"
//
//		"go.dokimi.dev/mutate"
//	)
//
//	func TestMutation(t *testing.T) {
//		mutate.Check(t, mutate.Workers(4))
//	}
//
// The run takes as long as the package's tests, once per mutant. Run it
// without the default deadline of go test:
//
//	go test -tags mutation -run '^TestMutation$' -timeout 0 .
//
// go test runs the test binaries of up to GOMAXPROCS packages at once. Run
// the mutation tests of a module's packages with -p 1:
//
//	go test -p 1 -tags mutation -run '^TestMutation$' -timeout 0 ./...
//
// [Check] fails the test with one line for each undetected mutant, in
// source order. It logs the run's summary last:
//
//	arith.go:5:33: survived: a - b became a + b (aor)
//	arith.go:7:33: not covered: x * 2 became x / 2 (aor)
//	mutate: fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered
//
// Every run of the instrumented test binary sets DOKIMI_MUTATE_INSTRUMENTED,
// and a test that asserts an allocation count skips while it is set. With
// [Confirm], Check runs each survivor, and each mutant whose site the tests
// never execute, once more in an ordinary build of that mutant alone, where
// such a test runs and can kill the mutant.
//
// # Environment
//
//   - DOKIMI_MUTATE_RECORD_DIR names the directory that [Check] writes the
//     run's record to, as the import path escaped as a path segment and
//     .mutate.json.
//   - DOKIMI_MUTATE_LINES restricts the run to lines of the package's
//     files, as file:first-last entries separated by commas, each file
//     relative to the package directory.
//
// The command dokimi-mutate-go, go.dokimi.dev/mutate/cmd/dokimi-mutate-go,
// runs the same check on any module without a change to it.
//
// # Dependency position
//
// Imports context, fmt, os, strings, testing and time from the standard
// library, and internal/record, internal/run and internal/spec from this
// module.
package mutate
