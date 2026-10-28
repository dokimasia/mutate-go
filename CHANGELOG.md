# go.dokimi.dev/mutate

## 0.2.0

### Minor Changes

- 8da21de: Add `--history` to `dokimi-mutate-go` to reuse the verdicts of unchanged mutants from the records of earlier runs.
- 18db1f6: **Breaking:** Write records of version 2, which state the observations of the run and the observation of each verdict.

## 0.1.0

### Minor Changes

- c9e8726: Add `mutate.Check` and the `dokimi-mutate-go` command to run mutation testing on Go packages.

### Patch Changes

- 2ef217f: Publish `dokimi-mutate-go` as release archives, Linux packages and a cask of the tap `dokimasia/homebrew-tap`.
