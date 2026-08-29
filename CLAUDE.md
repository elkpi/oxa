# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project contract

oxa is a collection of pure, in-process protocol-conversion libraries for
OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages. It is not a
proxy, HTTP client, router, retry layer, authentication service, or model
capability database.

The project has three locking deliverables, in this precedence order:

1. `vectors/` defines conversion behavior.
2. `spec/schema/` defines structural JSON shapes.
3. `spec/*.md` defines semantics and rationale.

When a behavior changes, update **specification → golden vectors →
implementation** in that order. `vectors/manifest.json` is generated and must
be refreshed when vectors change.

## Common commands

The Go reference implementation is a separate module at
`github.com/elkpi/oxa/go`; run Go commands from `go/` unless a Make target is
used.

```bash
# Repository root
make test                 # go test ./... in go/
make lint                 # go vet ./... in go/
make fmt                  # list files that gofmt would change
make vectors              # validate vector schema, naming, and manifest
make check-modulepath     # reject module-path placeholders

# Go module (go/)
go test -count=1 ./...    # uncached full test suite
go build ./...            # build every Go package
go vet ./...
gofmt -l .                # formatting check
gofmt -w <files>          # apply formatting to selected files
go test -count=1 ./openai/responses -run '^TestName$'  # one test

go run ./cmd/veccheck -root .. -check-manifest # CI manifest check
go run ./cmd/veccheck -root .. -write-manifest # regenerate after vector edits
```

CI runs Go 1.23 and 1.24 tests with `-race`, and separately runs `gofmt`,
`go vet`, `golangci-lint`, vector validation, and the manifest check.

## Architecture

- The architecture is hub-and-spoke: each protocol face implements only
  **face → IR** and **IR → face**. Do not add direct face-to-face conversions.
  `go/internal/deps` enforces that spoke packages import only the standard
  library, `ir`, and `modelmap`—never another face package.
- `go/ir` owns the face-neutral request, response, block, event, and loss
  types. Wire structs remain inside their face package; do not expose wire
  shapes through IR.
- The three Go spokes are `openai/chatcompletions`, `openai/responses`, and
  `anthropic/messages`. Each provides non-streaming request/response
  conversion and incremental streaming converters. Streaming decoders use
  `Feed`, `Flush`, and `Losses`; encoders consume ordered IR events with
  `Apply`.
- `spec/01-intermediate-representation.md` defines the IR invariants. In
  particular, streaming events must satisfy INV-5 grammar and INV-6 contiguous
  IR block indexes. Tool inputs and `input_json_delta.partial_json` are opaque
  raw JSON text under INV-1: never parse and re-marshal them.
- Semantic gaps produce ordered `ir.Loss` records; structural input/lifecycle
  errors return `error`. Streaming losses stay in `Losses()` and are never IR
  events.
- `modelmap` is the only model-name transformation point. The default is
  identity passthrough; do not add built-in model aliases or capability logic.
- `go/sse` is deliberately an opaque byte-framing adapter. It must not gain
  provider JSON decoding, IR conversion, loss handling, or `[DONE]` sentinel
  semantics.
- M6 stream support is text-only. The streaming semantics in `spec/20` defer
  tool-argument aggregation (`tool_calls`, function-call argument deltas, and
  `input_json_delta`) to M7.

## Vectors and specifications

Vector files live under `vectors/<face>/<mode>/`. A vector's `name` equals its
relative path with `/` changed to `.` and without `.json`; `veccheck` enforces
this and global uniqueness. Keep expected losses separate from stream events.

For stream vectors, preserve total event order, validate INV-5/INV-6, and keep
text or tool-argument fragments in encounter order. Native stream byte/chunk
boundaries need not round-trip exactly; comparison is through normalized IR
semantics.

Read the relevant mapping document (`spec/10`, `spec/11`, or `spec/12`) and
`spec/20` before changing a face converter. Use `spec/README.md` for the
specification reading order and source-of-truth rules.

## Scope and commit boundaries

Keep commits narrowly cherry-pickable. Specification, vector, implementation,
and unrelated refactoring changes are separate unless a test directly verifies
the implementation in the same change. Do not add Rust, Python, or C++
implementation skeletons before the v1 specification freeze.
