# Vendored definition

A copy of the mutate-spec definition: the operator catalogue, the run
protocol, the record schema, the Go overlay, the manifest, and of each
case of the corpus its `case.json` and its Go fixture. Do not edit it by
hand. `make spec-sync` refreshes it from mutate-spec with
`tools/spec-sync.sh`, checks every file against the manifest, and copies
the files that the engine reads into `internal/spec`. `make spec-check`
runs the check again, and reports whether the copy is behind mutate-spec.

| File | Upstream path |
|---|---|
| `VERSION` | `VERSION` |
| `catalogue.json` | `spec/catalogue.json` |
| `protocol.json` | `spec/protocol.json` |
| `record.schema.json` | `spec/record.schema.json` |
| `overlay.json` | `spec/overlays/go.json` |
| `manifest.json` | `spec/manifest.json` |
| `corpus/<case>/case.json` | `spec/corpus/<case>/case.json` |
| `corpus/<case>/go/` | `spec/corpus/<case>/go/` |

The engine reads `VERSION`, `catalogue.json`, `protocol.json` and
`overlay.json` when it runs. `internal/spec` embeds copies of them, so a
run does not read a file of this directory or of mutate-spec. A test of
`internal/spec` fails when a copy differs from the file here. The test of
the package `conformance` runs each case's Go fixture through the engine.
