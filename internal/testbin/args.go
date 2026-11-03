// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package testbin

import (
	"regexp"
	"strings"
	"time"
)

// The flags of a test binary that the package passes.
const (
	// flagJSON frames the start, the pause, the continuation and the end of
	// each test in the output, which Scan reads.
	flagJSON = "-test.v=test2json"
	// flagPanicOnExit0 makes a test that calls os.Exit(0) fail, as go test
	// makes it fail.
	flagPanicOnExit0 = "-test.paniconexit0"
	// flagTimeout starts the deadline after which the testing package's
	// alarm ends the binary and names the running tests.
	flagTimeout = "-test.timeout="
	// flagFailFast starts no test after the first failure.
	flagFailFast = "-test.failfast"
	// flagRun starts the pattern of the tests to run.
	flagRun = "-test.run="
)

// Flags returns the flags of a run with the deadline timeout: the framed
// progress that Scan reads, a failure for a test that calls os.Exit(0), and
// the deadline. failfast adds -test.failfast.
//
// # Allocation contract
//
// Flags allocates the list and the text of the deadline.
func Flags(timeout time.Duration, failfast bool) []string {
	flags := []string{flagJSON, flagPanicOnExit0, flagTimeout + timeout.String()}
	if failfast {
		flags = append(flags, flagFailFast)
	}
	return flags
}

// The separators of a pattern of -test.run: the separator of the elements of
// a test's name, which the testing package matches one level at a time, and
// the separator of two alternative patterns of whole names.
const (
	levelSeparator = "/"
	alternatives   = "|"
)

// Only returns the flag that runs only the tests that names lists, each
// matched in full and literally. A subtest's name gets one anchored pattern
// for each element of its name, such as ^TestPair$/^max$, and the testing
// package reads a | between two such patterns as alternatives, so the flag
// runs the subtests of the names and no other subtest of their tests.
func Only(names ...string) string {
	var b strings.Builder
	b.WriteString(flagRun)
	for i, name := range names {
		if i > 0 {
			b.WriteString(alternatives)
		}
		for j, element := range strings.Split(name, levelSeparator) {
			if j > 0 {
				b.WriteString(levelSeparator)
			}
			b.WriteString("^")
			b.WriteString(regexp.QuoteMeta(element))
			b.WriteString("$")
		}
	}
	return b.String()
}

// Setenv returns env with each of kv in place of every entry of its key: a
// KEY=value entry sets the key, and a KEY entry without = removes it. A Go
// program reads the first entry of a key, so a later entry alone would not
// take effect. Setenv leaves env unchanged.
func Setenv(env []string, kv ...string) []string {
	keys := map[string]bool{}
	for _, e := range kv {
		key, _, _ := strings.Cut(e, "=")
		keys[key] = true
	}
	out := make([]string, 0, len(env)+len(kv))
	for _, e := range env {
		if key, _, _ := strings.Cut(e, "="); !keys[key] {
			out = append(out, e)
		}
	}
	for _, e := range kv {
		if strings.Contains(e, "=") {
			out = append(out, e)
		}
	}
	return out
}
