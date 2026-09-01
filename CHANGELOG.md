# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
