# Anthropic structured output implementation plan

> **For agentic workers:** Use superpowers:executing-plans to execute the work sequentially.

**Goal:** Support Anthropic Messages `output_config.format` in all five implementations.

**Architecture:** Extend the generic response-format contract, then lock behavior with shared vectors before implementing each face and codec. Preserve opaque schemas, preserve effort, and report cross-face losses without synthesizing names.

**Tech Stack:** JSON Schema, shared vectors, Go, TypeScript, Python, Rust, C++.

**Spec:** `spec/01-intermediate-representation.md`, `spec/02-loss-policy.md`, `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md`.

## Global Constraints

- Specification precedes vectors and implementation; generated vector manifest must be refreshed.
- Unknown fields are audited in source order; schema contents are opaque under INV-1.
- Do not synthesize an OpenAI format name; omit the unrepresentable format with an explicit loss.
- Keep unrelated work and existing files untouched; use narrow commits.
- User approved extension of the public IR/API on 2026-10-29. Rust's new `schema_raw` variant field is source-breaking and must be disclosed; `serde_json/arbitrary_precision` has transitive Cargo feature effects.
- Lossless JSON-text entrypoints retain source schema bytes; mutable sidecars must be checked against the current schema. General-purpose JSON serialization may normalize bytes.
- Anthropic `stream` remains a separately reported unmapped transport marker; it does not affect schema mapping.

## Review Focus

- Missing name when targeting either OpenAI face: omit format and report a target-requirement loss.
- Missing, null, or non-object schema and invalid `type`: deterministic structural error or mapped value loss.
- Unknown format properties: report nested paths without traversing schema.
- `output_config.effort` and format coexist under native-option replay and edits.
- Schema bytes and strict presence (including false) survive appropriate round trips or incur explicit losses.

## Work units

1. **Contract:** update the above specs and `spec/schema/ir.schema.json`; verify schema compilation with `make vectors`. Timeout 5 minutes for the check; no temporary resources.
2. **Shared vectors:** replace the obsolete audited-format vector and add decode, encode, cross-face, effort and loss cases under `vectors/`; regenerate `vectors/manifest.json`. Validate with `make vectors`; timeout 5 minutes. Incomplete implementations are expected to fail the new vectors.
3. **Go:** update `go/ir`, `go/anthropic/messages`, and both OpenAI faces; add targeted tests for malformed input and raw schema fidelity. Run targeted tests and `make test`; timeout 10 minutes per command.
4. **TypeScript:** update `ts/src/ir`, `ts/src/anthropic/messages`, and both OpenAI faces; regenerate schema types and add equivalent tests. Run `npm test` and `npm run generate:check`; timeout 10 minutes per command.
5. **Python:** update `python/src/oxa/ir`, `python/src/oxa/anthropic/messages`, and both OpenAI faces; add equivalent tests. Run `PYTHONPATH=src python3 -m unittest discover -s tests`; timeout 10 minutes.
6. **Rust:** update `rust/crates/oxa-ir`, `oxa-anthropic`, `oxa-chatcompletions`, and `oxa-responses`; add equivalent tests. Run `cargo test`; timeout 10 minutes.
7. **C++:** update `cpp/include/oxa/ir.hpp`, `cpp/src/ir.cpp`, `cpp/src/anthropic`, and both OpenAI faces; add equivalent tests. Run CMake build and CTest; timeout 10 minutes per command.
8. **Integration:** check all five test suites, vector manifest, formatting and lint; inspect diff and release metadata. Commit tightly related units, push branch, and create a PR. Do not tag or publish a release. Remove only disposable artifacts created by this work when safe; retain worktree until user chooses to exit.
