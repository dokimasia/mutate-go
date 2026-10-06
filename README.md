# mutate

Mutation testing for Go, defined by a language-neutral standard. The
command `dokimi-mutate-go` and the function `mutate.Check` run your tests
against small changes to your package's code, and report each change that
your tests miss. The operator catalogue defines the changes.

```text
arith.go:7:33: not covered: x * 2 became x / 2 (aor)
arith.go:5:33: survived: a - b became a + b (aor)
fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered
```

## Install

The command runs on any module without a change to it:

```sh
go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest ./...
```

`go install go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest` installs it as
`dokimi-mutate-go`. Every language's engine of the standard is named
`dokimi-mutate` and the language.

To run it from `go test`, add the module:

```sh
go get go.dokimi.dev/mutate
```

The module requires Go 1.21 or later and imports only the standard library.
The engine reads the export data that its go command writes. It stops with
an error when `go env GOVERSION` in the package directory names another
toolchain than the one that built the engine. If your module selects a
newer toolchain, set `GOTOOLCHAIN` to it when you build the engine.

## Run it from the command line

```sh
go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest [flags] [packages]
```

`go run` with a version suffix ignores the `go.mod` of the current
directory, so the module under test keeps its files as they are. The
packages are patterns, as `go list` resolves them, and `.` by default.

| Flag | Meaning |
|---|---|
| `-p n` | Check n packages at once, 1 by default |
| `-workers n` | Run n mutants of one package at once, 1 by default |
| `-lines file:first-last` | Restrict the run to these lines of the file. The flag repeats |
| `-diff file` | Restrict the run to the lines that the unified diff in the file adds, and the lines on either side of the lines it removes. `-` reads standard input |
| `-suite pattern` | Count the tests of the packages that the pattern names too, resolved in the current directory. The flag repeats |
| `-record dir` | Write the record of each package to `dir` |
| `-json` | Write each package's record to standard output as one line of JSON, in place of the text |
| `-timeout d` | Start no package after `d`, and no mutant that would not end before then. 0, the default, sets no limit |
| `-sample n` | Start the runs of the first n mutants of each package in key order, and of no other. The summary states the sample's score, which the same code gives on every machine. 0, the default, runs every mutant |
| `-memory bytes` | Admit the runs of a package while the memory ceilings of every admitted package's runs fit in `bytes`, with `K`, `M`, `G` or `T` for a power of 1024. 0 sets no limit. By default three quarters of the memory that the process may use, by its cgroup limit or the machine's memory |
| `-confirm` | Run each survivor, and each mutant that is not covered, once more in an ordinary build of that mutant alone, and take the verdict of that run |

| Exit status | When |
|---|---|
| 0 | No package has an undetected mutant, and no run fails |
| 1 | A package has an undetected mutant |
| 2 | A run fails, a package does not start, a package of `-suite` does not resolve or load, the diff of `-diff` does not match the files, or the command line is wrong |

## Check the lines of a pull request

A pull request's check can run the mutants of the lines that the pull
request changes, from the top level of the checkout:

```sh
git diff origin/main...HEAD | go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest -diff - ./...
```

- The paths of the diff, after their `b/` prefix, are relative to the
  current directory. A diff that the files do not match stops the command.
- The run selects the lines that the diff adds, and the lines on either
  side of each run of lines that it removes. A package without such a line
  runs none of its mutants.
- A diff that changes only tests selects no mutant. A run of every line
  remains the gate.

## Run it from go test

Add one test file to the package, behind a build constraint that an
ordinary `go test` run does not satisfy:

```go
//go:build mutation

package btree_test

import (
	"testing"

	"go.dokimi.dev/mutate"
)

func TestMutation(t *testing.T) {
	mutate.Check(t)
}
```

Run it without the default deadline of `go test`. For more than one
package, add `-p 1`, because `go test` runs up to GOMAXPROCS test binaries
at once:

```sh
go test -p 1 -tags mutation -run '^TestMutation$' -timeout 0 ./...
```

`Check` fails the test with one line for each undetected mutant, and logs
the summary. With Go 1.25 or later, each line starts with the mutant's
position, so an editor opens the mutant. Earlier toolchains put the
position of the call to `Check` before the line.

| Variable | Meaning |
|---|---|
| `DOKIMI_MUTATE_RECORD_DIR` | `Check` writes the run's record to this directory |
| `DOKIMI_MUTATE_LINES` | `Check` restricts the run to these lines, as `file:first-last` entries separated by commas, each file relative to the package directory |

## Count the tests of other packages

A package's own tests are its suite by default. When the tests of other
packages exercise it, such as a conformance suite in another package, name
those packages:

```sh
go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest -suite ./... ./wire
```

```go
mutate.Check(t, mutate.Suite("../conformance"))
```

The engine builds the test binary of each such package whose tests link
the package, with the package's mutants in it, and leaves out the others. A
mutant runs each binary whose tests executed its code, the package's own
first, until one fails. A test of another package reads as that package's
import path, a colon and the test's name, and the record's `suite` lists the
other packages.

The command resolves the patterns of `-suite` in the current directory, as
it resolves the packages. A pattern that `go list` cannot resolve, such as
a directory that does not exist, or a package that does not load, stops the
command before any run starts.

## Tests that measure the build or store state

Every run of a test binary has `DOKIMI_MUTATE_MUTANT` set: to 0 in the
control runs, and to the active mutant's ordinal in a mutant's run and in
its confirmation run. Every run of the instrumented test binary also has
`DOKIMI_MUTATE_INSTRUMENTED` set to 1.

- The instrumented build inlines less than the ordinary one, so a value
  that the ordinary build keeps on the stack can move to the heap. Skip an
  assertion of an allocation count or a duration while
  `DOKIMI_MUTATE_INSTRUMENTED` is set.
- A run fails with the error `changed-files` when a test adds, changes or
  removes a file of the package, such as a failing input that it stores in
  `testdata` for later runs. Do not store such a file while
  `DOKIMI_MUTATE_MUTANT` is set to a value other than 0.

With `-confirm` or `mutate.Confirm()`, the engine runs each survivor, and
each mutant that is not covered, once more in an ordinary build of that
mutant alone, without `DOKIMI_MUTATE_INSTRUMENTED`. A skipped assertion of
an allocation count then runs against the mutant, and its failure kills
the mutant. A mutant that is not covered keeps that verdict when the run
passes. Before the mutants run, the engine builds and runs the tests from
the unchanged code once, and the run fails when a test fails there. Each
such mutant costs one more build: on go-humanize, the run took 18.5 s
instead of 13.5 s.

## Read the output

Each line reports one mutant: its position, its verdict, the original
code, the mutant's code and its kind. The line of a deleted statement reads
`removed` in place of `became`.

| Verdict | Meaning | What to do |
|---|---|---|
| `survived` | The tests ran the mutant and passed | Add an assertion that the mutant breaks, or annotate a mutant that no test can tell apart from the original |
| `not covered` | No test executes the mutant's code | Add a test that executes it |
| `error` | The run ended in a way that the engine cannot classify, such as a signal that it did not send | Read the reason at the end of the line |

The command writes a mutant's line when the mutant's verdict is final, so
the lines follow the order of the verdicts. A mutant that is not covered
gets its verdict when the opening control run ends, or with `-confirm` when
its confirmation ends, and any other mutant when its own run ends. `Check`
reports its lines in source order when the run ends.

The summary states how many of the mutants that the score counts the tests
detected. The score counts the killed, timed out, exhausted, survived and
not covered mutants. The percentage is rounded down, so 100% means that the
tests detected every one of them.

The command writes the run errors, the number of mutants that did not run,
and a progress line for each package every 10 seconds to standard error:

```text
dokimi-mutate-go: fixture: 120 of 410 mutants done, 7 undetected, 3.2 mutants a second, about 1m31s left, -timeout in 4m10s
```

## The record

`-record`, `-json` and `DOKIMI_MUTATE_RECORD_DIR` write the record that
mutate-spec defines. A record is one JSON document per package. It lists
every mutant with its verdict, the excluded mutants included, and states
the control runs, the limits and the score. A mutant's `key` identifies it
across runs. [`conformance/spec/record.schema.json`](conformance/spec/record.schema.json)
is the record's JSON Schema.

- `catalogue` and `overlay` are the versions of the operator catalogue and
  of its Go overlay. Two records' scores count the same mutants only when
  both versions are equal.
- A mutant's `coveredBy` lists the tests that executed its code when they
  ran alone, where the engine ran each test of the package alone. For a
  survivor, these are the tests that should have failed.
- `generated` lists each generated file that the run leaves out, with the
  number of mutants that it would have, and the summary states their total.
  A score of 100% then shows how much of the package it covers.

## Suppress a mutant

The engine does not mutate test files, generated files, files outside the
build or constants. A generator whose output your tests check makes its
files targets with a line before the package clause:

```go
// Code generated by codecgen. DO NOT EDIT.
//dokimi:mutate-include

package wire
```

The engine suppresses the mutants of five rule families that the catalogue
defines:

- calls that write log records
- calls that sleep, or set a deadline or a timeout, and the calls of the
  cancel function of such a context, such as `defer cancel()` after
  `context.WithTimeout`
- calls that register a flag
- arguments that only size an allocation
- calls that mark a function as a test helper, such as `t.Helper()`, also
  through a library's own interface with that method

To suppress the mutants of one line, annotate the line with their kinds
and the reason:

```go
//dokimi:mutate-skip sbr-delete: the lookup only saves time, and the value is the same without it
if v, ok := cache[key]; ok {
	return v
}
```

- On a line of its own, the annotation covers the next line. After code, it
  covers its own line.
- The list contains kinds, classes such as `ror`, or `all`.
- The reason after the colon is required. An annotation without a reason
  fails the run.
- An annotation that does not suppress a mutant fails the run, so you
  remove it when the code that it excuses changes.

No annotation covers a function or a file. The record lists every
suppressed mutant with its reason, and the score leaves it out.

## Parallelism and limits

- `-workers n` and `mutate.Workers(n)` run n mutants of one package at
  once, each in its own process. Tests that share a resource outside their
  temporary directory, such as a fixed port, then fail each other, and the
  failures count as kills.
- The packages together use GOMAXPROCS threads. Every go command of a
  package runs with GOMAXPROCS divided by `-p`, and every test binary with
  that number divided by the workers.
- A mutant's run of a test binary ends at 10 times that binary's time in
  the opening control run plus 2 seconds. On Linux, it also ends when its
  resident memory exceeds 4 times that binary's peak in the opening control
  run plus 512 MiB.
- When a test binary has fewer tests than the package has mutants to run,
  the engine runs each of the binary's top-level tests alone once, with no
  mutant active, and records the code that each test executes and its time.
  A mutant's run then starts with the tests that execute its code, and runs
  the whole suite only when they pass. The first part ends at 10 times
  those tests' times plus 2 seconds, so a mutant that hangs in a fast test
  does not wait for the binary's slowest tests.
- `-memory` admits the runs of a package only while the memory ceilings of
  every admitted package's runs fit in the budget. The engine ends each run
  that crosses its ceiling, so the runs together use about the budget at
  most, by default three quarters of a CI container's memory limit.
- The mutants run in the order of their keys, which is a pseudo-random
  order that every run of the same code repeats. When `-timeout`, the
  test's deadline or an interrupt ends a run early, the mutants that ran are
  a uniform sample of the package's mutants. The run fails, and its summary
  and the record's `sample` state the score of that sample:
  `fixture: the run failed, 40 of a sample of 52 mutants detected (76%): ...`

## Develop

| Command | What it does |
|---|---|
| `make check` | Runs the gate that CI runs: the linters, every test on the latest Go release and on Go 1.21.0 with every statement covered, and the race detector |
| `make fmt` | Formats the Go sources |
| `make spec-sync` | Copies mutate-spec's definition into `conformance/spec`, checks it against its manifest, and copies the files that the engine embeds into `internal/spec` |
| `make spec-check` | Checks the vendored definition against its manifest, and reports whether it is behind mutate-spec |

`conformance/spec` contains the vendored copy of mutate-spec's definition
and the Go fixtures of its corpus. The test of `conformance` runs every case
through the engine and compares the record with the case.
[RFC-0001](docs/rfc/0001-the-go-mutation-engine.md) describes the engine.

## License

MIT. See [LICENSE](LICENSE).
