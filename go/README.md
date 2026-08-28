# oxa Go module

Module path: `github.com/elkpi/oxa/go`.

## Layout

- `cmd/veccheck/` — the golden-vector checker (M2). Validates every file in
  `vectors/` against `spec/schema/vector.schema.json` and the IR sides
  against `spec/schema/ir.schema.json`; generates and verifies
  `vectors/manifest.json`.
- `ir/` — the IR types and face converters (arrives in M3; not yet present).

## Running veccheck

From `go/`:

    go run ./cmd/veccheck -root ..                 # validate all vectors
    go run ./cmd/veccheck -root .. -write-manifest # regenerate vectors/manifest.json
    go run ./cmd/veccheck -root .. -check-manifest # CI: verify manifest is fresh
    go run ./cmd/veccheck -root .. -schema-only    # just compile the schemas

From the repo root, `make vectors` runs the same check.

## Tests

    go test ./...

(Converter tests arrive with the M3 `ir` package.)

## Dependencies

`github.com/santhosh-tekuri/jsonschema/v6` is used by veccheck for JSON
Schema 2020-12 validation. It is a dev/CI-only dependency; the runtime
converter code added in M3 does not depend on it.
