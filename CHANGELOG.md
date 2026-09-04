# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.3.0] - 2026-09-04

### Added

- C++ implementation (`cpp/`): C++20 library (`oxa`) targeting the frozen spec
  baseline with zero third-party runtime dependencies and exception-free error handling.
  - `oxa::status`: Abseil-style `Status`, `StatusOr<T>`, and `Conversion<T>` types
    supporting `-fno-exceptions`.
  - `oxa::json`: lightweight self-contained JSON value type with source span
    tracking, byte-for-byte slice extraction (INV-1), float/int preservation (INV-7),
    and duplicate key rejection.
  - `oxa::ir`: neutral request, response, block, event, and loss types; document
    codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5) and block
    indexing (INV-6) invariant validator.
  - `oxa::modelmap`: model renaming table with identity fallback (spec/03).
  - `oxa::openai::chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa::anthropic::messages`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa::openai::responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa::sse`: standalone byte-level Server-Sent Events frame decoder and encoder
    (spec/20 §6).
  - `oxa::vectest`: test harness running all shared nonstream (105), cross-protocol
    (12), and stream (8) golden vectors against each C++ face with full INV-1
    lexical precision.
  - CI: CMake build and CTest execution on Ubuntu.

## [v0.2.0] - 2026-09-03

### Added

- Python implementation (`python/`): PEP 621 package (`oxa`) targeting the
  frozen spec baseline with zero runtime dependencies.
  - `oxa.ir`: neutral request, response, block, event, and loss types; document
    codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5), block
    indexing (INV-6), and tool-argument concatenation (INV-1) invariant checker.
  - `oxa.modelmap`: model renaming table with identity fallback (spec/03).
  - `oxa.openai.chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa.anthropic.messages`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa.openai.responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa.sse`: standalone byte-level Server-Sent Events frame decoder and encoder with zero
    external dependencies (spec/20 §6).
  - `oxa.vectest`: test harness running all shared nonstream, cross-protocol,
    and stream golden vectors against each Python face with full INV-1 lexical
    precision.
  - CI: matrix testing across Python 3.10, 3.11, 3.12, and 3.13.

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
