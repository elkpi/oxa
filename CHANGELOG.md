# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.0.0] - 2026-09-26

### Added

- **Spec 2.0.0 Coordinated Milestone Release**: Reasoning Content and Usage Granularity across all five reference implementations (Go, TypeScript, Rust, Python, C++), verified against all 154 golden vectors.
  - IR contract version 0.2.0: first-class `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` sealed variants. Dual-read compatibility accepting both `0.1.0` and `0.2.0`.
  - Request reasoning effort: closed enum `minimal`, `low`, `medium`, `high` mapped across all faces.
  - Granular usage: cache read/creation input tokens and token detail breakdowns (`input_tokens_details.cached_tokens`, `output_tokens_details.reasoning_tokens`), preserving absent vs. zero.
  - M9 streaming profile (N-S-11): streaming reasoning deltas, opaque provider signature deltas, and lifecycle closures across OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages spokes.
  - Go (`github.com/elkpi/oxa/go/v2`): module path upgraded to `v2` with full semantic import versioning support.
  - TypeScript (`@elkpi/oxa`): package updated to 2.0.0 with typed root and subpath exports.
  - Rust (`elkpi-oxa`): workspace and 7 modular crates updated to 2.0.0.
  - Python (`elkpi-oxa`): package updated to 2.0.0.
  - C++ (`oxa`): CMake package updated to 2.0.0 with standard and `-fno-exceptions` support.

- Python (Wave 4): Spec 2.0 reasoning content and usage granularity support.
  - Implemented `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` across all spokes and IR codec/checker.
  - Added request `reasoning_effort` mapping and dual-read support for IR specVersion `0.1.0` and `0.2.0`.
  - Added granular usage accounting (`cache_read_input_tokens`, `cache_creation_input_tokens`, `input_tokens_details`, `output_tokens_details`).
  - Implemented M9 streaming reasoning and signature lifecycle events across Chat Completions, Responses, and Anthropic Messages spokes.
  - Verified against all 154 golden vectors.

- C++ (Wave 5): Spec 2.0 reasoning content and usage granularity support under C++20 with `-fno-exceptions`.
  - Implemented `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` across all spokes and IR codec/checker.
  - Added request `reasoning_effort` mapping, dual-read `specVersion` support, and granular usage details.
  - Implemented M9 streaming reasoning deltas and lifecycle closures across Chat Completions, Responses, and Anthropic Messages spokes.
  - Verified against all 154 golden vectors across standard and `-fno-exceptions` builds.

- Multi-language convergence: all five implementations (Go, TypeScript, Rust, Python, and C++) now validate the full 154 Spec 2.0 golden vector suite.

## [v1.0.1] - 2026-09-17

### Added

- Multi-language constant convergence: synchronized and type-safe IR and spoke protocol constants across Python (`oxa.ir.constants`), TypeScript (`@elkpi/oxa/ir`), C++ (`oxa::*`), Go (`ir.*`), and Rust (`oxa_ir::*`).
- Automated CI constant-drift verification (`scripts/check-constants.py`) ensuring 100% coverage of closed schema enums against `spec/schema/*.json`.
- TypeScript: ESM-only `@elkpi/oxa` package at version `1.0.1`, with typed root and focused subpath exports for the IR, JSON, SSE, stream adapters, and all three protocol faces. It has no runtime dependencies and supports Node.js 20+ and Web runtimes.
- TypeScript packaging gates: exact npm tarball-content validation plus a clean installed consumer that type-checks and imports every public ESM entry point.
- Rust: `elkpi-oxa` umbrella crate with `[lib] name = "oxa"` re-exporting all protocol faces and IR under `use oxa::...`.

### Changed

- Python: distribution package renamed to `elkpi-oxa` on PyPI (`pip install elkpi-oxa`); in-code imports remain `import oxa`.
- Downstream packaging: verified a clean consumer for the `@elkpi/oxa` npm tarball, and `elkpi-oxa` on both Python (wheel & sdist) and Rust.
- Release version 1.0.1 synchronized across Python, TypeScript, Rust, C++, and Go module tags.

## [v1.0.0] - 2026-09-04

### Added

- **oxa 1.0.0 Milestone Release**: At the original 2026-09-04 milestone, all four
  then-shipped reference implementations (Go, Rust, Python, C++)
  implement the complete pure in-process protocol-conversion specification across OpenAI Chat
  Completions, OpenAI Responses, and Anthropic Messages, verified against the identical 125 golden vectors.
  - Go, Python, and C++ have no third-party runtime dependencies; Rust production crates use `serde` and `serde_json`.
  - Hub-and-spoke intermediate representation (IR) with pure face ↔ IR converters.
  - Full non-streaming, streaming (M7 tool-argument aggregation), and cross-protocol conversion support.
  - Opaque tool input handling preserving byte-for-byte fidelity (INV-1).
  - Explicit loss tracking for unmapped fields and semantics (spec/02).
  - Multi-OS continuous integration across Linux (Ubuntu default and `-fno-exceptions`), macOS, and Windows.
  - Downstream packaging support: CMake `install()` and `find_package(oxa CONFIG REQUIRED)` exports for C++,
    workspace package versioning for Rust crates, and PEP 621 package metadata for Python.

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
