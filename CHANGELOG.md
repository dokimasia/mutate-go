# go.dokimi.dev/mutate

## 0.3.0

### Minor Changes

- d7f7143: Add `--all-killers` and `mutate.AllKillers` to name every test that kills a mutant.
- d7f7143: Add `--subtests` and `mutate.Subtests` to trace each subtest alone and name the subtests that cover a mutant.

### Patch Changes

- d7f7143: Read the variable of the trace from the protocol, and document the variables that a child process of a test inherits.
- 7f740cc: Fix the control runs to fail on a failed test of a test binary that exits with the status 0.
- 7f740cc: Give `killed` to a mutant whose run had a failed test before the deadline, the memory ceiling or a signal ended it.
- 47203aa: Fix the peak and the memory ceiling of a test binary on Linux to leave out the memory of the process that started it.
- 7f740cc: Fix the tests of a kill to name the first test that failed, without the test that started it.
- d7f7143: Note the work directory that a run cannot remove, with its path and the error of the removal.
- ca8e566: Fix the removal of a run's temporary directory in which a test locked a directory.
- 72e17b8: Update `go.stealthscale.io/cli` to v0.8.0.

## 0.2.0

### Minor Changes

- 8da21de: Add `--history` to `dokimi-mutate-go` to reuse the verdicts of unchanged mutants from the records of earlier runs.
- 18db1f6: **Breaking:** Write records of version 2, which state the observations of the run and the observation of each verdict.

## 0.1.0

### Minor Changes

- c9e8726: Add `mutate.Check` and the `dokimi-mutate-go` command to run mutation testing on Go packages.

### Patch Changes

- 2ef217f: Publish `dokimi-mutate-go` as release archives, Linux packages and a cask of the tap `dokimasia/homebrew-tap`.
