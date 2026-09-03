# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.1.0] - 2026-09-03

### Added

- Rust implementation (`rust/`): Cargo workspace targeting the frozen spec
  baseline with zero cross-face dependencies.
  - `oxa-ir`: neutral request, response, block, event, and loss types;
    document codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5)
    and block indexing (INV-6) invariant checker.
  - `oxa-modelmap`: model renaming table with identity fallback.
  - `oxa-chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa-anthropic`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa-responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa-sse`: byte-level Server-Sent Events frame decoder and encoder with zero
    external dependencies (spec/20 §6).
  - `oxa-vectest`: test harness running all shared nonstream, cross-protocol,
    and stream golden vectors against each Rust face with full INV-1 lexical
    precision.

## [v0.0.1] - 2026-09-02

### Added

- Written protocol-conversion specification (`spec/`): hub-and-spoke IR,
  loss policy, model handling, per-face mappings for Chat Completions,
  Responses, and Anthropic Messages, and streaming semantics.
- Golden vector set (`vectors/`): per-face nonstream and stream vectors,
  cross-protocol (`protocol-to-protocol`) nonstream vectors, validated by
  `cmd/veccheck` and pinned by `manifest.json`.
- Go reference implementation (`go/`): face-neutral `ir` package; three
  protocol faces (`openai/chatcompletions`, `openai/responses`,
  `anthropic/messages`) with nonstream request/response conversion and
  incremental stream decoders/encoders; opaque SSE byte framing (`sse`);
  optional model-name mapping (`modelmap`).
- Streaming tool-argument aggregation: Chat Completions `tool_calls`
  index aggregation, Responses function-call argument deltas, and
  Anthropic `input_json_delta`, preserving tool inputs as opaque raw JSON.
- Public documentation: project README with support matrix and quick
  start, a compile-verified cross-face godoc example, and a release
  checklist.
