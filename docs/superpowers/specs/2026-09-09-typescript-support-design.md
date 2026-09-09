# TypeScript Support Design

## Status

Approved design for a fifth oxa implementation. This document does not alter
the normative protocol contract: `vectors/`, `spec/schema/`, and `spec/*.md`
remain its sources of truth in that order.

## Goal

Publish one ESM TypeScript package, `@elkpi/oxa`, that implements every
currently checked-in golden vector, including M7 streaming tool-argument
aggregation, without HTTP, provider SDK, global-state, or runtime-dependency
coupling.

## Scope

- Implement `ir`, `modelmap`, `sse`, Chat Completions, Responses, and
  Anthropic Messages as face-to-IR and IR-to-face spokes only.
- Support non-streaming conversion and incremental M6/M7 stream conversion.
- Support Node.js 20+, modern browsers, and standard Web/edge runtimes.
- Provide typed-object core APIs, stateful streaming APIs, and thin
  `AsyncIterable` helpers.
- Publish only after all shared vectors, packaging, consumer, type, Node, and
  Web Runtime checks pass.

Out of scope: transport, HTTP, authentication, routing, model capability
logic, provider SDK integrations, CJS output, and an npm preview release.

## Package and Module Layout

`ts/` is an npm package with ESM JavaScript, declaration files, and source
maps produced directly by `tsc`. Public subpath exports are `ir`, `modelmap`,
`sse`, `json`, `openai/chatcompletions`, `openai/responses`, and
`anthropic/messages`.

Each face imports only the standard TypeScript platform, `ir`, and `modelmap`.
An architecture test enforces that no face imports another face. `sse` imports
neither protocol faces nor IR.

The repository schemas generate and CI-check IR, loss, and vector-fixture
types. The provider wire shapes have no JSON Schemas, so their types are
hand-written from `spec/10` through `spec/12` and their behavior is locked by
the shared vectors. Generated files are committed. Semantic conversion types
remain hand-written.

## Public API

Non-streaming conversion returns `Readonly<{ value: T; losses: readonly Loss[]
}>` and throws `OxaError` only for malformed structure or lifecycle/grammar
violations. `OxaError` contains a stable machine-readable `code`, a message,
and an optional cause. Semantic representability gaps always produce ordered
loss records.

Every face exposes typed `decodeRequest`, `encodeRequest`, `decodeResponse`,
and `encodeResponse` functions. Options accept an optional `ModelMapper`;
identity mapping is the default and no model aliases are built in.

Each streaming decoder exposes `feed(event)`, `flush()`, and `losses()`.
Each encoder exposes `apply(event)`. A stream cannot accept input after its
single flush/terminal completion. `losses()` returns a non-mutating cumulative
snapshot. Convenience async helpers are thin wrappers over these state
machines and preserve event ordering, final flush, losses, and error timing.

## Lossless JSON Boundary

Native `JSON.parse` and `JSON.stringify` are not sufficient: vectors require
integer/non-integer distinction (`1` versus `1.0`), exact large integers, and
verbatim nested tool JSON. The internal `json` module therefore parses JSON
into a lossless tree with a `JsonNumber` token and serializes it without
number normalization. It also exposes explicit construction helpers such as
`json.fromValue` and `json.integer` for callers who intentionally canonicalize
ordinary JavaScript values.

`JsonText` represents opaque JSON text. Tool inputs, tool schemas,
`partial_json`, and Anthropic `tool_use.input` retain raw text or lossless
subtrees according to their mapping rule; converters never parse and
re-marshal them incidentally. JSON APIs are explicit (`parseJson` and
`stringifyJson`); callers needing wire fidelity use them rather than native
JSON APIs.

## Streaming and SSE

Streaming follows `spec/20` and the IR grammar exactly. M7 input JSON deltas
are accumulated in encounter order and emitted/encoded without reformatting.
The separate SSE adapter consumes and emits `Uint8Array` chunks only; it
frames bytes and does not decode provider JSON, produce losses, or assign
`[DONE]` semantics.

## Testing and Release Gates

Tests replay every shared vector and compare lossless JSON semantically under
the vectors comparison rules. Focused unit tests cover lossless numbers,
opaque JSON, error codes, model mapping, stream lifecycle, M7 aggregation,
and SSE UTF-8 chunk boundaries. CI also runs type checking, formatting,
architecture checks, Node 20+, a Web Runtime suite, and a packed-tarball
clean-consumer test. The published tarball contains only compiled output,
types, source maps, README, and license.

The package starts at `0.x`; vector behavior, loss codes, and raw JSON
preservation are stable from the first release. Protected CI publishes the
version tag with npm provenance/trusted publishing after all gates pass.

## Delivery Sequence

1. Tooling, generated types, lossless JSON, IR, loss/error, and model mapping.
2. SSE and test/vector harness infrastructure.
