# oxa Go module

Module path: `github.com/elkpi/oxa/go`.

Go reference implementation of oxa pure protocol-conversion library.

## Layout

- `ir/` — the neutral intermediate representation types, JSON codec, and streaming invariant checker.
- `openai/chatcompletions/` — Chat Completions request/response conversion and streaming decoder/encoder with tool call aggregation.
- `openai/responses/` — OpenAI Responses request/response conversion and streaming decoder/encoder with function call aggregation.
- `anthropic/messages/` — Anthropic Messages request/response conversion and streaming decoder/encoder with block/input_json_delta aggregation.
- `sse/` — standalone byte-level Server-Sent Events frame decoder and encoder (spec/20 §6).
- `modelmap/` — model renaming table with identity fallback (spec/03).
- `cmd/veccheck/` — golden-vector validation and manifest generation tool.
- `internal/vectest/` — vector test harness loading vectors from the repository root.

## Running veccheck

From `go/`:

    go run ./cmd/veccheck -root ..                 # validate all vectors
    go run ./cmd/veccheck -root .. -write-manifest # regenerate vectors/manifest.json
    go run ./cmd/veccheck -root .. -check-manifest # CI: verify manifest is fresh
    go run ./cmd/veccheck -root .. -schema-only    # just compile the schemas

From the repo root, `make vectors` runs the same check.

## Tests

From `go/`:

    go test ./... -race

From the repo root, `make test` runs the Go test suite.

## Dependencies

Zero third-party runtime dependencies. Standard library only (`encoding/json`, `io`, etc.).

`github.com/santhosh-tekuri/jsonschema/v6` is used only by `cmd/veccheck` for JSON Schema 2020-12 validation as a dev/CI tool.
