---
rfc: 0001
title: The Go mutation engine
author: Roy Klopper <roy.klopper@stealthscale.io>
status: Draft
created: 2026-10-02
updated: 2026-10-06
discussion: none
supersedes: none
superseded-by: none
produces-adr: tbd
---

# RFC-0001: The Go mutation engine

## Summary

The Go engine is the module `go.dokimi.dev/mutate`, in the mutate-go
repository. `go test` runs it through `mutate.Check(t)`, and `go run` runs
it as the `dokimi-mutate-go` command, which leaves the module under test
unchanged.
Both type-check the package with the standard library, write every mutant
into the package's source behind a runtime switch, hand the rewritten files
to `go test -c` through `-overlay`, and run the one test binary in a fresh
process per mutant. On request, they also build each survivor, and each
mutant whose site the tests never execute, alone as ordinary source and run
its tests once more. The engine applies the
operator catalogue and follows the run protocol that mutate-spec defines.
This RFC covers what is specific to Go.

## Motivation

One build per package is faster than a build per mutant on every package
measured, and far faster where the suite is short. With the same mutants in
both modes, one mutant at a time:

| Package | Mutants | Suite | One build | A build per mutant | Ratio |
|---|---|---|---|---|---|
| go-humanize v1.1.0 | 655 | 0.006 s | 1.9 s | 104.2 s | 55× |
| google/btree v1.1.3 | 605 | 0.2 s | 49.0 s | 150.8 s | 3.1× |
| shopspring/decimal v1.4.0, every 36th mutant | 58 | 4 s | 135.0 s | 145.4 s | 1.1× |

A build per mutant also grew the build cache by 1.3 to 2.4 MB per mutant,
and one build by 4.0 to 8.7 MB per package. On btree, one build ran
gremlins' own operators 5.0 times faster than gremlins.

A prototype with the instrumented forms of this design measured these
figures, and the figures under Bounds. It did not mutate compound
assignments or unary minus, it loaded through golang.org/x/tools, and it
checked its instrumentation with a forced-failure run instead of a trace.
It had no closing control run and no memory ceiling.

No published Go tool combines the two properties. ooze runs from a test and
builds once per mutant. mutest and kanly build once and are separate
binaries. On btree, each of the four tools showed a defect or a limit:

- gremlins v0.6.0 did not test a single mutant, and exited 0, when `go.mod`
  opened with a comment.
- ooze v0.2.0 grew the build cache by 477.5 MB, because it copies the module
  to a new path for every mutant.
- kanly v0.1.0 counted mutants whose test binaries the kernel ended for
  memory as killed.
- mutest v0.6.2 refuses a module whose go line is below 1.20.

A library that `go test` drives needs only the toolchain, and a survivor
appears where a test failure appears. A command that `go run` fetches
leaves the module under test unchanged.

## Detailed design

### Components

| Component | Responsibility | Package |
|---|---|---|
| Entry for tests | `Check` and its options | `mutate` |
| Entry for the command line | `dokimi-mutate-go`, run by `go run` | `cmd/dokimi-mutate-go` |
| Loader | `go list -export`, then `go/types` with export data | `internal/load` |
| Enumerator | Sites, mutants, keys, exclusions, suppression, selection | `internal/enumerate` |
| Renderer | The instrumented files, the helper file and the overlay | `internal/render` |
| Runner | The build, the control runs, the mutant runs, the limits and the verdicts | `internal/run` |
| Recorder | The record and its `inputs` digest | `internal/record` |
| Definition | The vendored catalogue, run protocol and Go overlay from mutate-spec, decoded once, with their vocabularies as Go types | `internal/spec` |

The module's go line is 1.27.0. A module that adds the engine as a
dependency gets its go line raised to at least 1.27.0. The module's
packages import only the standard library, and its tests also import
`go.dokimi.dev/assert`. CI builds and tests it with the latest release.

### The test entry point

```go
// Package mutate measures a Go package's tests by mutation. It runs the
// tests against each change to the package's code that the operator
// catalogue defines, and reports every change that the tests do not detect.
package mutate

// Check runs mutation testing on the package in the test's working
// directory, and fails tb with one line for each undetected mutant: a
// survivor, or a mutant whose site the tests never execute. Each line
// starts with the mutant's position, which go test does not precede with
// the position of the call to Check. When DOKIMI_MUTATE_RECORD_DIR is set,
// Check writes the run's record there.
//
// Check builds the package's test binary once, with every mutant behind a
// runtime switch, and runs it in a fresh process per mutant. That binary
// contains every test of the package, so the test that calls Check belongs
// in a file whose build constraint an ordinary go test run does not
// satisfy, such as //go:build mutation. Check skips tb when it runs inside a
// binary that Check started.
//
// Check fails tb with the run error when the package does not load, an
// annotation lacks a reason or suppresses nothing, the instrumented build
// fails, a test fails with no mutant active, or a run changes a file of the
// package. When the test's deadline leaves too little time for the next
// mutant and the closing control run, Check marks the remaining mutants
// not-run and fails tb. Run it with -timeout 0, or with a timeout longer
// than the run.
func Check(tb testing.TB, opts ...Option)

// Option configures one call of Check. The zero Option changes nothing.
type Option struct {
	set func(config) config
}

// Workers sets the number of mutants that run concurrently, 1 by default.
//
// Each mutant runs in a process of its own, so n above 1 runs n copies of
// the package's tests concurrently. Tests that share a resource outside
// their temporary directory, such as a fixed port, then fail each other,
// and the failures count as kills. Every run, the control runs included,
// gets GOMAXPROCS divided by n, and at least 1. Workers panics for n below
// 1.
func Workers(n int) Option

// Suite adds the tests of other packages to the tests that count for the
// package's mutants. The patterns name the packages as go list resolves
// them in the package directory, such as ../conformance or ./... . Check
// builds the test binary of each such package whose tests link the
// package, with the package instrumented, and leaves out the others. A
// mutant runs each binary whose tests executed its site, the package's own
// first, until one fails. The record's suite names the other packages, and
// a test of one of them reads as the package's import path, a colon and the
// test's name.
func Suite(patterns ...string) Option

// Confirm makes Check confirm each survivor, and each mutant whose site the
// tests never execute, in the mutant's ordinary build. Check writes the
// mutant alone into the package's source, without a switch, builds the test
// binary from that source, and runs it once more without
// DOKIMI_MUTATE_INSTRUMENTED. A test that checks a property of the build,
// such as an allocation count, and skips while that variable is set, then
// runs against the mutant. The verdict of that run is the mutant's verdict,
// and a mutant whose site the tests never execute keeps the verdict not
// covered when the run passes.
func Confirm() Option

// IncludeGenerated makes Check mutate every generated file of the package,
// as if the comments before its package clause contained the line
// //dokimi:mutate-include. The record lists each such file as included, and
// the score counts its mutants.
func IncludeGenerated() Option
```

A package opts in with one file:

```go
//go:build mutation

package btree_test

import (
	"testing"

	"go.dokimi.dev/mutate"
)

func TestMutation(t *testing.T) {
	mutate.Check(t, mutate.Workers(4))
}
```

```sh
go test -tags mutation -run '^TestMutation$' -timeout 0 .
```

Check reports each undetected mutant, and each mutant whose run ended in an
`error`, at its position and in source order. An editor's test runner shows
the report like any other test failure:

```text
<file>:<line>:<column>: survived: <original> became <replacement> (<kind>)
<file>:<line>:<column>: not covered: <original> became <replacement> (<kind>)
<file>:<line>:<column>: error: <original> became <replacement> (<kind>): <reason>
```

A deletion reads `<original> removed` in place of `<original> became
<replacement>`. Check then fails the test once for each run error, and once
for the mutants that did not run, and logs the run's summary.

### The command

```sh
go run go.dokimi.dev/mutate/cmd/dokimi-mutate-go@latest [flags] [packages]
```

`go run` with a version suffix ignores the `go.mod` of the current
directory. The module under test keeps its `go.mod` and its files as they
are. The command's name is `dokimi-mutate` and the language, the name of
every language's engine, so `go install` installs it as `dokimi-mutate-go`
beside the engines of other languages.

The command's help describes every flag, grouped by task: the selection,
the tests, the execution and the output. One table of flags in the
command's source defines the flags and writes the help, and a test writes
the command's package documentation and the README's flag reference from
the help, so no second description of a flag exists. The sections below
state the design of the mechanisms behind the flags.

- `-C dir` changes the working directory before the command resolves the
  packages, the suite's patterns and every path of `-lines`, `-diff` and
  `-record`, as the go command's `-C` does.
- `-list` loads and enumerates each package, and type-checks its
  instrumented source, and stops before the build. It prints one line per
  mutant, with `to test` or the verdict that the enumeration decides, and
  a count per package. Its `not-viable` mutants are a run's, except those
  that only a confirmation's build rejects.
- `-version` prints the versions of the engine, of the catalogue, of the
  Go overlay and of the toolchain that built the engine.

The command writes the line of each undetected mutant, and of each mutant
whose run ended in an `error`, to standard output in Check's format. It
writes the line when the mutant's verdict is final. A mutant without
coverage gets its verdict when the opening control run ends, or under
`-confirm` when its confirmation ends, and any other mutant when its run
ends, so the lines follow the order of the verdicts.
When a package's run ends, the command writes the package's summary:

```text
<import path>: <detected> of <counted> mutants detected (<percent>%): <count> <verdict>, ...
<import path>: <detected> of a sample of <counted> mutants detected (<percent>%): <count> <verdict>, ...
<import path>: the run failed: <count> <verdict>, ...
<import path>: the run failed, <detected> of a sample of <counted> mutants detected (<percent>%): <count> <verdict>, ...
```

The percentage is rounded down. The second form states the record's
`sample` of a run that `-sample` alone ended. The fourth form states the
`sample` of a run that a deadline or an interrupt ended.

To standard error, the command writes each run error, the number of
mutants that did not run with the reason, and a progress line for each
package that runs, every 10 seconds:

```text
dokimi-mutate-go: <import path>: <done> of <total> mutants done, <undetected> undetected, <rate> mutants a second, about <duration> left, -timeout in <duration>
```

The line reads `running` until the package's first verdict. A mutant is
done when it has a verdict other than `not-run`. The rate and the time left
appear once a mutant's run has ended. The time until the timeout appears
under `-timeout`, and `-timeout passed` replaces it once the timeout has
passed. SIGINT and SIGTERM stop the runs, and no package starts after them.

Once the caller's deadline keeps the remaining mutants from starting, the
engine gives each of them `not-run` at once, while the mutants that the
workers took still run. The line then states them and what the run waits
for, in place of the rate and the time left:

```text
dokimi-mutate-go: <import path>: <done> of <total> mutants done, <undetected> undetected, <count> not run, waiting for <count> mutant runs and the closing control run, -timeout in <duration>
```

| Exit status | When |
|---|---|
| 0 | Every counted mutant was detected, and every run completed |
| 1 | A package has a mutant that survived or that no test covers |
| 2 | The command line or an input is invalid: an unknown flag, a value out of range, a directory of `-C` that does not exist, a diff that does not read or does not match the files, or a pattern of packages or of `-suite` that `go list` cannot resolve |
| 3 | A run failed: a package does not load or build, its tests fail without a mutant, a run changed a file of the package, a record does not write, `-timeout` or a signal ended the run, or the command cannot write to standard output |

When more than one status applies, the command exits with the highest.
A run that `-sample` alone ends exits with the status of its sample, 0
or 1.

`-diff` reads a unified diff, such as the output of
`git diff origin/main...HEAD`, and selects lines of the new version of each
file as cargo-mutants' `--in-diff` selects them:

- Each line that the diff adds is selected, and so are the lines on either
  side of each run of lines that it removes, where the new version has
  them. A hunk need not show those lines as context, so a diff without
  context, such as the output of `git diff -U0`, selects the same lines. A
  file that the diff deletes selects no line.
- The path of each new version, after its `b/` prefix, is relative to the
  working directory, as a path of `-lines` is.
- The engine reads each file that the diff states. A line that the diff
  adds or keeps, and that the file states otherwise, stops the command,
  because the diff is older or newer than the checkout. So does a hunk with
  more lines of a version than its header counts, and a header whose range
  ends past the largest `int`.
- A hunk ends after the lines that its header counts, and after the note
  of a missing final line break that can follow them. The next line starts
  a header or the metadata of a file. These lines stop the command there,
  as lines past the hunk's counts:
  - a line that starts with a space, `+` or `-` and does not start a header
  - an empty line, which a tool writes for a line that both versions have
  - a second note
- A package whose files the selection does not touch runs none of its
  mutants. Its record's `selection` is an empty list, and each of its
  mutants is `not-selected`.

Under `-p` above 1, the command counts the mutants that each package's run
would test before the first run starts. It loads and enumerates each
package for its count, and schedules the packages by the counts:

- The packages start in the order of their counts, the most first, and in
  the order of `go list` among equal counts.
- A package that starts gets a share of the threads that the running
  packages leave free, in proportion to its count against the counts of
  the packages that may start beside it: the free slots of `-p`, or the
  packages left when they are fewer. The share is rounded, at least 1 and
  at most the free threads, and equal where those packages count 0.
- While GOMAXPROCS is at least `-p`, a package starts only when a thread
  is free, so a package with many mutants runs on more threads beside fewer
  packages. Where GOMAXPROCS is below `-p`, each package gets one thread.
- A package's share does not change during its run, and the package
  returns it when the run ends. A share that grew during a run would change
  how many tests a binary runs at once, so the mutants before and after the
  change would run under different conditions, and the limits of the
  opening control run would no longer describe the later runs.

`-memory-budget` keeps the packages that run at once within a budget of
memory:

- After its opening control run, a package's runs may use its workers
  times the largest memory ceiling of its test binaries. The package waits
  until that fits in the budget beside the runs of the packages that the
  command admitted. A package that needs more on its own starts when no
  other package runs, so every package starts.
- The default budget is three quarters of the memory that the process may
  use: the machine's `MemTotal`, or the least `memory.max` of the
  process's cgroup v2 and its ancestors when that is less. A systemd scope
  or a CI container states such a limit.
- The engine ends each run that crosses its ceiling, so the runs together
  use about the budget at most. The kernel's out-of-memory killer then
  does not end a test binary, which would give its mutant `error` and fail
  the package's run. The ceilings exist on Linux only, so the budget
  admits every package elsewhere.

A run resolves its suite in its package's directory, the path that `go
list` states with every symbolic link resolved. A relative pattern made
absolute from the working directory keeps the links of the working
directory's path, and `go list` then places the pattern outside the
module. An import path refers to the same package from every directory of
the module. A pattern of `-suite` that `go list` cannot resolve, and a
package of `-suite` that it states an error of, stop the command before
any run starts.

### Environment

| Variable | Set by | Meaning |
|---|---|---|
| `DOKIMI_MUTATE_RECORD_DIR` | The caller | Check writes the record to this directory |
| `DOKIMI_MUTATE_LINES` | The caller | Check's selection, as `file:first-last` entries separated by commas, files relative to the package directory |
| `DOKIMI_MUTATE_MUTANT` | The engine | The protocol's variable: the ordinal of the active mutant in a mutant's run and in its confirmation run, and 0 in the control runs. Its presence makes Check skip. A test library that stores state for later runs stores none while it is not 0 |
| `DOKIMI_MUTATE_INSTRUMENTED` | The engine | `1` in every run of the instrumented test binary, the control runs included, and unset in the ordinary control run and in each confirmation run. A test that asserts an allocation count or a duration skips while it is set |
| `DOKIMI_MUTATE_TRACE` | The engine | The path of the opening control run's trace |
| `TMPDIR` | The engine | A fresh directory per run, removed when the run ends |
| `GOMAXPROCS` | The engine | Every run of a test binary gets the package's share of the engine's GOMAXPROCS divided by the workers, and at least 1. Every go command of a package's run gets the package's share and the threads that the running packages leave free, divided among them, because a build's threads do not change a verdict. A confirmation's build divides them by the workers too. Under the command, the rules of `-p` decide each package's share |

The record's file name is the package's import path, escaped with
`url.PathEscape`, followed by `.mutate.json`.

### Loading

1. The engine runs `go list -json -export -deps -compiled -e .` in the
   package directory, with the caller's environment, so `GOFLAGS`,
   `GOTOOLCHAIN` and `go.work` select what an ordinary `go test` there
   selects.
2. It type-checks the package's `CompiledGoFiles` with `go/types`, through
   `go/importer.ForCompiler(fset, "gc", lookup)`. `lookup` opens the export
   data that `go list` reported for each dependency.
3. It mutates the files that `go list` reports in `GoFiles`, a list that
   leaves out test files, files outside the build and files that import
   `C`. It also leaves out the files that `go/ast.IsGenerated` reports,
   unless a comment before the file's package clause is the line
   `//dokimi:mutate-include`, or the run includes generated files under
   `-include-generated` or `mutate.IncludeGenerated`. The record's
   `generated` lists each such file, with its mutants and whether the run
   included it.

Measured with this loader alone, on Go 1.27.1, google/btree v1.1.3,
go-humanize v1.1.0, shopspring/decimal v1.4.0, bits-and-blooms/bitset
v1.25.0 and go-cmp v0.7.0's `cmp` type-checked with no error, each in under
0.2 s, `go list` included. On btree it compiled 1 of 3 implementation
files, the one Go 1.27's build constraints select.

These rules guard the load:

- The engine compares `go env GOVERSION`, run in the package directory,
  with `runtime.Version()`, and fails on a difference, because export data
  written by one toolchain does not read in another.
- The engine resolves symbolic links in the package's path before it writes
  the overlay. The go command matches an overlay against the paths it
  computes from its working directory, and through a link the overlay
  matched no file: the original package ran for every mutant.
- A package beneath `GOMODCACHE` fails to load, because the go command does
  not replace files there.
- A flag on the outer `go test` command line, such as `-tags`, is not passed
  to the engine's build. `GOFLAGS` is.

### Sites and their instrumented forms

Each instrumented file replaces each site with a form that switches on the
active mutant. Sites nest strictly inside one another, and each form writes
the forms of the sites inside it once, so a file's size grows linearly with
the depth of nesting.

| Kind | Site | Instrumented form |
|---|---|---|
| `aor`, and the `ror-*` kinds of `<`, `<=`, `>` and `>=` | `x op y` | `_mutate_s17(x, y)`, a generic function per site. It names its type argument, as in `_mutate_s17[uint](x, y)`, when the operand's type has a name in the package |
| `ror-true`, `ror-false` of `==` and `!=` | `x == y` | `_mutateEq(17, x == y)`, which returns `true` while the site's `ror-true` is active, `false` while its `ror-false` is, and the comparison otherwise |
| `aor` | `v op= y`, where `v` is an identifier or a chain of selectors | `v = _mutate_s17(v, y)` |
| `lcr-*` | `a && b`, `a \|\| b` | `(!_mutateCT(17, 19) && (_mutateActive == 18 \|\| (a)) && (_mutateActive == 17 \|\| (b)))` for `&&`, and `(_mutateCT(17, 19) \|\| (_mutateActive != 18 && (a)) \|\| (_mutateActive != 17 && (b)))` for `\|\|`. 17 is the site's `lcr-left`, 18 its `lcr-right`, and 19 its `lcr-false` or `lcr-true`. `_mutateCT` writes the site's trace and reports whether the mutant 19 is active. A mutant that leaves out an operand never evaluates it |
| `uoi-incdec` | `x++` as a statement | `if _mutateIs(17) { x-- } else { x++ }` |
| `uoi-incdec` | `x++` as a `for` loop's post statement | `x = _mutate_s17(x)` |
| `uoi-not` | a boolean operand `x` | `(x != _mutateIs(17))` |
| `uoi-minus` | `-x`, where `x` is not a constant | `_mutate_s17(x)`, a generic function per site that returns `-x`, or `x` when the mutant is active |
| `sbr-delete` | a statement | `if !_mutateIs(17) { stmt }` |
| `sbr-zero` | `return e1, e2` | `if _mutateIs(17) { return _mutateZero0, _mutateZero1 }`, before the original return. The variables contain the zero values of the function's results, as the following list states |

No declaration of the package changes what a form computes. A local
variable named `nil`, or one named after a result's type, would change a
zero value that the form writes as code, so a return of zero values
returns variables of the instrumentation's own:

- A function whose results have no names gets the names, in parentheses
  where the result list has none, as `func() T` becomes
  `func() (_mutateZero0 T)`. Only a return assigns such a variable, and a
  return ends the body.
- A result named `_` gets the name in its place.
- The function copies each other named result into its variable before its
  first statement, where the result is still the zero value.

Every name that the instrumentation adds starts with `_mutate`: the
declarations and imports of the helper file, and the variables of the
results. When an identifier of the package's files or of its test files
starts with `_mutate`, the names start with the first of `_mutate1`,
`_mutate2` and so on with which no identifier starts. No declaration of the
package or of its tests then hides one of the names or takes its place. A
package-level variable `_mutateZero0` that the function reads, a result
named `_mutateZero0` and a local variable `_mutateActive` keep their
meaning, and the forms keep theirs.

The helper file writes the constants `true` and `false` as `0 == 0` and
`0 != 0`, which no declaration of the package can hide. Each of these edits
is on the line that it changes. The mutant's copy of an increment's operand
is on one line, with a raw string that spans lines written as an
interpreted string, so every line of the file keeps its number, and
`runtime.Caller` reports the line of the source.

A package-level declaration of a predeclared type, of `nil` or of `panic`,
in the package or in its tests, hides that name in the helper file as well,
because the helper file belongs to the package and uses those names. The
instrumented build then fails with the run error `build`.

No form passes a function value, so no form makes an operand escape to
the heap.

The overlay adds one file to the package, `zz_mutate.go`. It reads
`DOKIMI_MUTATE_MUTANT` once, during package initialization, and declares
the switch functions and the per-site generic functions. When
`DOKIMI_MUTATE_TRACE` is set, it writes `start` to the trace when the
package initializes, and each site's first mutant ordinal the first time
the site executes.

In a module whose go line is below 1.18, every instrumented file and the
helper file get `go1.18` added to their build constraint, because the
helper functions are generic. A module at 1.22 or above never gets the
line. It would lower the file's language version and change the semantics
of its loop variables.

Go limits what one build can instrument. The engine lists these sites in
the record's `skipped` with the reason:

| Site | Why the engine cannot instrument it |
|---|---|
| A constant expression, or an array length | It cannot contain a runtime switch |
| An operand of a type-parameter type | A per-site function needs one type argument for every instantiation |
| An untyped constant in a non-constant shift, where the type has no name in the package | The constant takes its type from the surrounding expression, which a function argument does not supply |
| A comparison or connector whose result has a named boolean type | The switch functions return `bool` |
| `v op= y` or a `for` post statement whose `v` has side effects | `v = f(v, y)` would evaluate `v` twice |
| A site in a file that imports `C` | The overlay's support for cgo is limited |

A mutant's source keeps the code that the mutant leaves out behind a
constant that skips it, in the forms that the confirmation writes. A
variable, an imported package or a label that only that code uses remains
in use, so the compiler accepts the source.

The compiler rejects one kind of source that the instrumented build
accepts: an `aor` mutant that divides an integer by a constant 0, such as
`x / 0` for `x * 0` or `x /= 0` for `x *= 0`. The instrumented form divides
at run time, so the build alone does not reject the mutant. The engine
marks the mutant `not-viable`, with the compiler's message `invalid
operation: division by zero` as its reason, and does not run it. A
floating-point division by 0 compiles, and its mutant runs.

### Runs

The build is `go test -c -vet=off -o <work>/pkg.test -overlay <work>/overlay.json .`,
run in the package directory with the caller's environment. With a suite,
the engine also builds the test binary of each other package that the
caller names and whose test binary links the package, as `go list -test`
reports its dependencies, with the same overlay and that package's import
path in place of `.`.

- **No mutant to run.** When each mutant is `not-selected`, `suppressed`
  or `not-viable`, the engine skips the build and the control runs, and the
  record has no `control`.
- **A build that fails.** The engine builds the failed test binary from
  the unchanged source too. The run error `build` then states that the
  tests do not build from the unchanged source, with the compiler's
  message, or that only the instrumented build fails. A build that the
  caller cancels states no run error.

Every go command that the engine runs, the build included, runs in a
process group of its own, and the caller's cancellation sends `SIGKILL` to
the group. A go command that runs through a wrapper script then ends with
every process that the wrapper started, and the engine waits at most 5 s
for the command's output after the group ended.

Every run of a test binary runs in its package's directory with a fresh
`TMPDIR`, in a process group of its own, with these flags:

| Flag | Runs | Why |
|---|---|---|
| `-test.v=test2json` | Every run | The testing package frames the start, the pause, the continuation and the end of each test, so the engine names the tests that were running when it ended a run, and sees the first failed test as it fails. A parallel test that paused and did not continue is not running, and neither is a test while a subtest of it that did not pause runs. A test whose subtests in progress all paused counts only when no other test runs, because its own function can still run |
| `-test.paniconexit0` | Every run | `go test` passes it to every test binary. Without it, a test that calls `os.Exit(0)` ends the binary with status 0, and its mutant counts as a survivor |
| `-test.timeout=<deadline>` | Every run | The testing package names the tests that were running when the deadline passes |
| `-test.failfast` | Mutant runs | The binary starts no test after the first failure. It lets the running tests and the paused parallel tests run to their end, so the engine ends the process group at the first line that states a failed test |
| `-test.run=^(...)$` | A mutant's first run of a binary whose tests ran alone, and each run of one test alone | The run starts with the tests that executed the mutant's site, or traces one test on its own |

On Go 1.27.1, a test binary run directly reported each kind of failure
this way:

| The test | Exit status | What names the failure |
|---|---|---|
| calls `t.Error` | 1 | `--- FAIL: TestError` |
| panics | 2 | `--- FAIL: TestPanic`, then the panic |
| starts a goroutine that panics | 2 | The panic only |
| calls `os.Exit(1)` | 1 | Nothing |
| calls `os.Exit(0)`, without `-test.paniconexit0` | 0 | Nothing |
| calls `os.Exit(0)`, with `-test.paniconexit0` | 2 | `--- FAIL: TestExit0`, then the panic |
| hangs, under `-test.timeout=1s` | 2 | `panic: test timed out after 1s`, and `TestHang` under `running tests:` |

The engine also enforces the limits from outside the binary:

- **Exit.** A run ends when the test binary exits, whatever its output
  does. The engine then sends `SIGKILL` to the process group, which ends
  each process that a test started and left, so such a process neither
  delays the run nor turns its verdict into a timeout. The engine reads the
  rest of the output for at most 5 s, the bound for a process that left the
  group and keeps the output open.
- **Deadline.** 5 s after the deadline, the engine sends `SIGKILL` to the
  process group. This stops a hang that the testing package's timer cannot
  stop, such as one during package initialization.
- **Memory ceiling, on Linux.** The engine reads the process's resident
  pages from `/proc/<pid>/statm` every 25 ms, and sends `SIGKILL` to the
  process group when they exceed the ceiling. The control run's peak comes
  from `ru_maxrss` in the `rusage` that `wait4` returns.
- **Elsewhere.** On macOS the engine does not apply a memory ceiling, and
  each binary's `memoryCeilingBytes` in the record is `null`. On Windows it
  does not apply one either, and it kills only the test binary, because the
  standard library has no call that ends a process tree there. Neither
  platform has been run.

The engine gives each run of a test binary its verdict by the first rule
that matches:

1. The engine ended the run for memory: `exhausted`, with the tests that
   were running.
2. The engine ended the run at the backup deadline: `timed-out`, with the
   tests that were running.
3. The engine ended the run at the first failed test: `killed`, with the
   tests that failed by then.
4. A signal the engine did not send ended the run: `error`.
5. The exit status is 0: `survived`.
6. The output contains `panic: test timed out after`: `timed-out`, with the
   tests that were running.
7. Otherwise: `killed`, with the test that each `--- FAIL:` line names, or
   the tests that were running when the output has no such line.

A mutant's run runs, in order, each test binary whose opening control run
executed the mutant's site: the package's own first, then the others by
import path. Each binary's run ends at that binary's deadline, 10 times its
time in the opening control run plus 2 s, and at its memory ceiling. The
mutant's verdict is the verdict of the first run that does not survive,
and `survived` when every run survives. A test of another package reads as
that package's import path, a colon, a space and the test's name.

After the control runs before the mutants, the engine runs each top-level
test of a binary alone, with `-test.run` and a trace of its own, when the
binary has more than one test and fewer tests than the mutants whose sites
it executed. The top-level tests are those that the binary's opening
control run started. A mutant's run of such a binary first runs the tests
whose runs alone executed the mutant's site, which the record names in the
mutant's `coveredBy`. That first part ends at 10 times the sum of those
tests' wall times in their runs alone plus 2 s, so a mutant that hangs in a
fast test does not wait for the binary's slowest tests. When the tests
pass, the run goes on with the binary's whole suite, and the binary's
deadline applies to both parts together. A binary without such a record
runs its whole suite in each mutant's run. A binary has none after any of
these:

- A test fails alone.
- The trace of a run alone does not read, or lacks its start mark.
- The caller's deadline leaves too little time for the runs alone.

The engine starts the mutants' runs in the order of their keys, one on
each worker that is free. A key is the start of a SHA-256 digest, so the
order is a pseudo-random permutation that every run of the same code
repeats.

The caller's deadline is the test's deadline for Check, and `-timeout` for
the command. Under a deadline, the opening control run of each test binary
gets at most half of the time left. A mutant starts only while the time
left covers twice the sum of the binaries' deadlines: once for its own run
and once for the closing control run. A run of one test alone starts only
while the time left also covers its binary's deadline. While every worker
is busy, a timer ends the wait for a free worker once the time left no
longer covers the next run, so no run starts late. Every mutant that
has not started by then is `not-run`, and so is every mutant whose run an
interrupt ends. The record's `sample` then states the score of the mutants
whose keys sort before the least key of a `not-run` mutant. Those mutants
are a uniform sample of the package's mutants. Under `-sample n`, the
engine starts the runs of the first n mutants in key order, and every later
mutant is `not-run`, so the same code gives the same sample on every
machine. When that limit alone ends the runs, the record's `sample` states
the limit, the run does not fail, and its `score` is the sample's score.

### Confirmation

Under `Confirm` or `-confirm`, the engine runs each survivor, and each
mutant whose site no binary executed, once more in that mutant's ordinary
build:

- **The ordinary control run.** After the opening control run, the engine
  builds each test binary of the suite from the unchanged source, with
  `go test -c -vet=off` and no overlay. It runs each binary with
  `DOKIMI_MUTATE_MUTANT=0`, without `DOKIMI_MUTATE_INSTRUMENTED`, under the
  binary's limits. A failing test stops the run with the run error
  `ordinary-control-failed`.
- **The mutant's source.** The engine writes the mutant alone into its
  file. The file keeps the code that the mutant leaves out, behind a
  constant that skips it. Every line of the file keeps its number. The
  source writes the constants `true` and `false` as `(0 == 0)` and
  `(0 != 0)`, which no declaration of the package can hide.
  - `aor`, `ror-boundary` and `uoi-incdec` write their operator in place of
    the site's.
  - `ror-true` and `ror-false` write `(a < b || (0 == 0))` and
    `(a < b && (0 != 0))`, which still evaluate both operands.
  - `lcr-left` writes `((a) || (0 != 0) && (b))`, `lcr-right`
    `((0 != 0) && (a) || (b))`, `lcr-true` `((0 == 0) || (a) || (b))` and
    `lcr-false` `((0 != 0) && ((a) && (b)))`.
  - `uoi-not` writes `!(x)` for `x` and `(x)` for `!x`, and `uoi-minus`
    writes `(x)` for `-x`.
  - `sbr-delete` writes `if (0 != 0) { stmt }`, and `sbr-zero` writes
    `if (0 == 0) { return _mutateZero0, _mutateZero1 };` before the
    original return, with the variables of the function's results that the
    instrumented form binds.
- **The confirmation run.** The engine builds each test binary whose
  opening control run executed the site, or every test binary for a mutant
  whose site none executed, with an overlay of that one file. It runs each
  binary's whole suite with the mutant's ordinal and without
  `DOKIMI_MUTATE_INSTRUMENTED`, under the binary's limits, until one does
  not pass. The verdict of that run is the mutant's verdict, and the record
  marks the mutant `confirmed`. A mutant without coverage keeps
  `no-coverage` when every binary passes. A build that the toolchain
  rejects makes the mutant `not-viable`, with the toolchain's message as
  its reason.
- **Workers.** A mutant is confirmed on the worker that ran it. Each
  worker's go command gets the package's share and its part of the threads
  that the running packages leave free, divided by the workers.
- **The caller's deadline.** Under confirmation, a mutant starts only while
  the time left covers three times the sum of the binaries' deadlines, for
  its run, its confirmation run and the closing control run, plus the time
  that the ordinary control run's builds took.

The tests of the renderer build each of the 127 runnable mutants of three
fixtures both ways, and every mutant's ordinary build computes the value
that its instrumented form computes. Four of them leave out the only use
of a variable or of an imported package. Local variables of the first
fixture hide the names `true`, `false`, `nil` and `new` and a type's name,
and the second fixture declares constants named `true` and `false`. The
third declares `_mutateZero0` as a package-level variable that a function
reads, as a named result and as a local variable of a branch, a
package-level variable `_mutateIs`, and a local variable `_mutateActive`.

On go-humanize v1.1.0, with one worker, a run took 13.5 s without
confirmation and 18.5 s with it. All 42 survivors survived their ordinary
builds too, and the score was the same.

After the closing control run, the engine reads the data files of every
package of the suite again, and compares them with the files before the
opening control run. A file that the runs added, changed or removed is the
run error `changed-files`, whose message lists the files relative to the
module root. A run whose closing control run does not complete, because
the caller ended the run during it, fails without a run error, because no
run checked that the mutant runs left the tests' state intact.

A package's data files are the files in its directory, hidden ones such
as `.state` included, and in its subdirectories that contain no Go
package. Every file below a directory that the go command ignores,
`testdata` or a name that starts with `_`, is a data file, whatever its
name and its directory's content, so `testdata/fixture/data.go` is one.
Outside such a directory, the engine leaves out each directory whose name
starts with a dot, such as `.git`, where a tool keeps its state. The
engine reads a regular file as a stream, a symbolic link as its target's
path, and any other file, such as a named pipe, as its type. It opens no
other file and follows no link, so a named pipe does not block the read.

### Record and inputs

The record's `engine.name` is `mutate-go`, and `overlay` is the version of
the Go overlay that the engine embeds. `suite` lists the import paths of
the other packages whose test binaries the run built. `limits` states the
deadline and the memory ceiling of each binary, the package's own first,
and `selection` the selected ranges in the package's files. In a run that
confirms, `control.ordinary` states the ordinary control run's seconds, and
each confirmed mutant is `confirmed`.
A mutant's `coveredBy` lists the tests whose runs alone executed its site,
in the order of the binaries and of their tests, where every binary that
executed the site ran each of its tests alone. `generated` lists each file
that `go/ast.IsGenerated` reports and that lacks the include directive,
with the number of mutants that a separate walk of the file makes at its
sites, and whether the run included the file.
The `inputs` digest is the SHA-256 of these fields:

- The engine's version. A `(devel)` version, or one that ends in `+dirty`,
  adds the build ID that `go tool buildid` reports for the engine's
  executable, because such a version does not identify one build.
- `go env GOVERSION`.
- The build ID of each instrumented test binary of the suite.
- What the engine reads of every data file of every package of the suite
  before the opening control run, in path order: the SHA-256 of a regular
  file, the target of a link, and the type of any other file.

### Bounds

| Resource | Bound |
|---|---|
| Builds | One `go test -c` per package, and one per other package of its suite. 0.24 to 0.44 s per package on three packages. Under confirmation, the same again for the ordinary control run, one per survivor and test binary that executed its site, and one per mutant without coverage and test binary |
| Build cache | 4.0 to 8.7 MB per package on three packages |
| Time | Two runs of the suite, a run of each top-level test alone where a binary has fewer tests than mutants to run, and each covered mutant's run, each at most the sum of the binaries' deadlines. Under confirmation, also the ordinary control run and each confirmed mutant's builds and run: 0.12 s per survivor on go-humanize |
| Memory | Workers times one test binary, each bounded by the ceiling on Linux |
| Disk | The instrumented files, the helper file, one test binary and the trace, in a directory removed when the run ends |

With no mutant active, the instrumented binary ran decimal's suite 1.9%
slower than the ordinary one, and btree's 12.6% slower, over five
alternating rounds.

## Alternatives considered

### Load with golang.org/x/tools/go/packages

**Why not:** the engine's packages would require golang.org/x/tools, and
every module that adds the engine would require it too. The standard
library's importer loads the same packages with no error.

### A build per mutant

gremlins, ooze and gomutants each build the package once per mutant.

**Why not:** it was 1.1 to 55 times slower on three packages, and it grew
the build cache by 1.3 to 2.4 MB per mutant.

### A copy of the module per mutant

**Why not:** ooze does this, and its run on btree grew the build cache by
477.5 MB. A new path per mutant changes every compiled package's cache key.

### Rewrite files through `-toolexec`

**Why not:** it puts a second executable between the go command and the
compiler. `-overlay` hands the compiler the rewritten files without one.

### Only the test entry point

**Why not:** it adds a dependency to the module under test and a test file
to every package. The command works on the module as it is, which matters
most to a team trying mutation testing on code it does not want to change.

### Only the command

**Why not:** the test entry point runs from the `go test` a developer
already uses, and puts each survivor in the test runner's output at its
file and line.

### One process for many mutants

**Why not:** Go cannot run a package's initialization again in a running
process, so a mutant in an initializer would have no effect. Package-level
state, such as a cache that one mutant fills, would also remain for the next
mutant's run.

### `go vet` on each mutant

**Why not:** one vet run per mutant is one build per mutant. A mutant is
`not-viable` when the compiler rejects it. A mutant that only vet rejects,
such as one that leaves the connector `x != 1 || x != 2`, runs and gets a
verdict.

## Drawbacks

- **The test entry point changes the module under test.** A module below
  go 1.27.0 gets its go line raised to 1.27.0, and each package that opts in
  gets a test file. A module below go 1.22 then gets a variable per loop
  iteration, which changes a loop whose closures capture its variable.
- **The engine needs a Go 1.27 toolchain.** It refuses a go command of
  another toolchain than the one that built it. A module that selects an
  older toolchain runs the engine with `GOTOOLCHAIN` set to a Go 1.27
  release, so the engine and its go commands use one toolchain.
- **Instrumented suites run slower.** 1.9% and 12.6% on two packages, with
  no mutant active.
- **An instrumented build changes allocation counts.** The forms make a
  function larger, so the compiler can stop inlining it, and a value that
  the ordinary build keeps on the stack then moves to the heap. A test that
  asserts an allocation count can fail the opening control run. It skips
  while `DOKIMI_MUTATE_INSTRUMENTED` is set, and kills a mutant only in a
  confirmation run.
- **Confirmation costs a build per survivor and per mutant without
  coverage.** On go-humanize it added 37% to the run's time, 0.12 s per
  survivor, and killed no mutant, because no test of the package checks a
  property of the build.
- **A run of each test alone costs a process per test.** A `TestMain` that
  starts a database runs once per top-level test, and a survivor runs the
  tests of its site twice.
- **Some sites cannot be instrumented.** Generic code, mixed interface
  comparisons and cgo files keep their operators. The number of skipped
  sites is not counted for the three measured packages.
- **A process per mutant.** Every mutant costs one process start. On
  go-humanize, whose suite runs in 6 ms, the median mutant took 3 ms.
- **Workers above 1 can produce false kills.** Copies of a package's tests
  that share a port or a file fail each other.
- **The memory ceiling exists on Linux only.** macOS and Windows runs are
  bounded by the deadline alone, and Windows has not been run.

## Open questions

- Should one command dispatch to the engine of each language, such as
  `dokimi-mutate-go`, which every engine's name makes possible to find?

## Unresolved and future work

- Mutating operands of type-parameter type is not proposed. It needs a
  switch per instantiation, or a type switch inside the helper.

## References

| What | Where |
|---|---|
| Research-0001, mutation testing as a library from one build. The three packages in both modes, the tools on btree, the overhead with no mutant active, the overlay through a symbolic link | mutate-spec, `docs/research/0001-mutation-testing-as-a-library-from-one-build.md` |
| The probes behind the loader's timings and the failure-output table | mutate-spec, `docs/research/0001-mutation-testing-as-a-library-from-one-build/probes/` |
| `go help build`, Go 1.27.1: `-overlay`, and that files beneath GOMODCACHE may not be replaced | https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies |
| `go help run`, Go 1.27.1: a version suffix ignores the current `go.mod` | https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program |
| `cmd/go/internal/test/test.go`, Go 1.27.1, line 1596: `-test.paniconexit0` passed to every test binary | https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/test/test.go |
| `go/importer.ForCompiler` and its `lookup` function | https://pkg.go.dev/go/importer#ForCompiler |
| `go/ast.IsGenerated` | https://pkg.go.dev/go/ast#IsGenerated |
| `testing.T.Deadline` | https://pkg.go.dev/testing#T.Deadline |
| cargo-mutants' `--in-diff`: a `b/` prefix or none, and a diff that does not replace the full run | https://mutants.rs/in-diff.html |
| cargo-mutants' `affected_lines`: the lines that a diff adds, and the lines next to each deletion | https://github.com/sourcefrog/cargo-mutants/blob/main/src/in_diff.rs |
