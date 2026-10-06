// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Command dokimi-mutate-go runs mutation testing on Go packages without a
// change to their module. Its name follows the rule for every language's
// engine: dokimi-mutate and the language.
//
//	go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest [flags] [packages]
//
// go run with a version suffix ignores the go.mod of the working
// directory, so the module under test keeps its go.mod and its files as
// they are. The packages are patterns, as go list resolves them, and . by
// default.
//
//   - -p n checks n packages at once, 1 by default.
//   - -workers n runs n mutants of one package at once, 1 by default. Tests
//     that share a resource outside their temporary directory, such as a
//     fixed port, then fail each other, and the failures count as kills.
//   - -lines file:first-last restricts the run to these lines of the file,
//     relative to the working directory. The flag repeats.
//   - -diff file restricts the run to the lines that the unified diff in
//     file adds, and to the lines on either side of each run of lines that
//     it removes, as git diff base...HEAD writes them for a pull request.
//     The path of each new version, after its b/ prefix, is relative to the
//     working directory, and - reads the diff from standard input. A diff
//     that the files do not match fails the command, and a diff that changes
//     no line of a package selects none of its mutants. The lines of -diff
//     and of -lines together are the selection.
//   - -suite pattern adds the tests of the packages that pattern names to
//     the tests that count for each package's mutants, where their test
//     binaries link the package. The command resolves the pattern in the
//     working directory, as it resolves the packages. A pattern that go list
//     cannot resolve, such as a directory that does not exist, or a package
//     that does not load, fails the command. A test of such a package reads
//     as its import path, a colon and the test's name. The flag repeats.
//   - -record dir writes the record of each package to dir.
//   - -json writes the record of each package to standard output as one
//     line of JSON, in place of the text.
//   - -timeout d starts no package after d, and no mutant whose run and the
//     closing control run would not end before then. 0, the default, sets
//     no limit.
//   - -sample n starts the runs of the first n mutants of each package in
//     the order of their keys, and of no other. Each later mutant does not
//     run, so the package's run fails as a run that -timeout ends does, and
//     its summary states the score of the sample. The same code gives the
//     same sample on every machine. 0, the default, runs every mutant.
//   - -memory bytes admits the runs of a package after its opening control
//     run while the runs of every admitted package fit in bytes, each
//     package's workers times the largest memory ceiling of its test
//     binaries. A package whose runs need more on their own starts when no
//     other package runs. K, M, G and T after the number state powers of
//     1024, and 0 sets no limit. The default is three quarters of the memory
//     that the process may use: the machine's memory, or the least
//     memory.max of the process's cgroup v2 and its ancestors when that is
//     less. The engine applies memory ceilings on Linux only.
//   - -confirm runs each survivor, and each mutant that is not covered, once
//     more in an ordinary build of that mutant alone, without
//     DOKIMI_MUTATE_INSTRUMENTED, so a test that skips a check of the build
//     in the instrumented binary, such as an allocation count, runs against
//     it. The verdict of that run is the mutant's verdict, and a mutant that
//     is not covered keeps that verdict when the run passes.
//
// The packages together use GOMAXPROCS threads. Every go command of a
// package runs with GOMAXPROCS divided by -p, and every run of a test
// binary with that number divided by -workers, and at least 1.
//
// The command prints the line of each undetected mutant, and of each mutant
// whose run ended in an error, when the mutant's verdict is final. A mutant
// that is not covered gets its verdict when the opening control run ends,
// or under -confirm when its confirmation ends, and any other mutant when
// its own run ends. When the run of a package ends, the command prints the
// package's summary:
//
//	arith.go:7:33: not covered: x * 2 became x / 2 (aor)
//	arith.go:5:33: survived: a - b became a + b (aor)
//	fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered
//
// The tests ran a survivor and passed. No test executes the code of a
// mutant that is not covered. The line of a deleted statement reads
// removed in place of became. When the run leaves out generated files, the
// summary ends with their number and their mutants, such as 3 generated
// files with 120 mutants left out.
//
// The mutants run in the order of their keys, a pseudo-random order that
// every run of the same code repeats. When -timeout or a signal ends a run
// early, the mutants that ran are a uniform sample of the package's
// mutants, and the summary of the failed run states the sample's score:
//
//	fixture: the run failed, 40 of a sample of 52 mutants detected (76%): ...
//
// The command writes the run errors, the number of mutants that did not
// run, and a progress line for each package that runs every 10 seconds to
// standard error, each after the command's name:
//
//	dokimi-mutate-go: fixture: 120 of 410 mutants done, 7 undetected, 3.2 mutants a second, about 1m31s left
//
// A mutant is done when it has a verdict other than not-run. Once -timeout
// keeps the remaining mutants from starting, the line states how many will
// not run, and that the run waits for the mutants that still run and the
// closing control run:
//
//	dokimi-mutate-go: fixture: 121 of 410 mutants done, 7 undetected, 288 not run, waiting for 1 mutant run and the closing control run
//
// # Signals
//
// SIGINT and SIGTERM stop the runs. Every mutant without a verdict is then
// not-run, the runs fail, and no further package starts.
//
// # Exit status
//
//   - 0 when no package has an undetected mutant and no run fails
//   - 1 when a package has an undetected mutant
//   - 2 when a run fails, a package does not list or does not start, a
//     package of -suite does not resolve or load, the diff of -diff does
//     not read or does not match the files, or the command line is wrong
package main
