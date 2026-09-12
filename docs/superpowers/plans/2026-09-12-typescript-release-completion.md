# TypeScript Release Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` task-by-task. Steps use checkbox syntax.

**Goal:** Finish the TypeScript implementation so all checked-in vectors, M7 stream profiles, package surfaces, and release gates pass before any npm publish.

**Architecture:** Preserve hub-and-spoke conversion: Chat Completions, Responses, and Anthropic each translate only to/from IR. M7 streams use stateful decoders (`Feed`, `Flush`, `Losses`) and encoders (`Apply`), with opaque tool JSON retained exactly. Vector harnesses execute shared fixtures through typed face APIs.

**Tech Stack:** Node 20+, TypeScript 5.9 ESM, node:test, Prettier, no runtime dependencies.

**Spec:** `docs/superpowers/specs/2026-09-09-typescript-support-design.md`; normative order is `vectors/`, `spec/schema/`, then `spec/*.md`.

## Global Constraints

- Do not implement direct protocol-to-protocol imports, HTTP, provider SDKs, CJS, or runtime dependencies.
- Preserve opaque tool argument strings and `JsonText` exactly; never parse/re-marshal them on a conversion path.
- Treat structural/lifecycle errors as `OxaError`; report semantic gaps as ordered `Loss` values.
- Add a failing `node:test` before each production behavior.
- Keep each face, its streams, vector binding, and release integration in separate commits.
- Do not run `npm publish`; publishing is a separately authorized terminal action.

### Task 1: Close Chat Completions M7 review and public surface

**Files:** `ts/src/openai/chatcompletions/*`, `ts/test/chatcompletions-stream.test.ts`, `ts/src/index.ts`.

- [ ] Review commits `7b6edf6..11dbb5c` against `vectors/chatcompletions/stream/m7-*.json` and N-S-10.
- [ ] Write a failing public-import test for `openai/chatcompletions`, then export only the face API needed by consumers.
- [ ] Run `npm test`, `npm run check`, `npm run fmt`, `npm run generate:check`; commit review fixes separately.

### Task 2: Responses M7 streams

**Files:** create `ts/src/openai/responses/{types,stream,index}.ts`; create `ts/test/responses-stream.test.ts`.

- [ ] Write failing tests from `vectors/responses/stream/m7-function-call-to-ir.json`, `m7-function-call-from-ir.json`, and `m7-function-call-output-loss.json`.
- [ ] Implement typed native event shapes and stateful decoder: aggregate raw function arguments, validate optional `arguments.done`, absorb unsupported `function_call_output` with one loss, and emit final usage.
- [ ] Implement encoder: synthesize function-call items, preserve/synthesize argument deltas, emit done/item completion/terminal events.
- [ ] Run all TypeScript gates and commit `feat(ts): add M7 responses streams`.

### Task 3: Anthropic M7 streams

**Files:** create `ts/src/anthropic/messages/{types,stream,index}.ts`; create `ts/test/anthropic-stream.test.ts`.

- [ ] Write failing tests from all `vectors/anthropic/stream/m7-*.json`, including start-input fallback.
- [ ] Implement decoder/encoder block lifecycle and raw `input_json_delta` handling; synthesize exactly one full delta when no native delta exists.
- [ ] Run all TypeScript gates and commit `feat(ts): add M7 anthropic streams`.

### Task 4: Vector bindings and non-streaming faces

**Files:** create per-face non-stream conversion modules and vector-driven tests under `ts/test/`.

- [ ] Add a failing vector runner that filters each face/mode and compares output plus losses through `vectest`.
- [ ] Implement Chat Completions non-streaming request/response conversion against every matching vector.
- [ ] Implement Responses non-streaming request/response conversion against every matching vector.
- [ ] Implement Anthropic non-streaming request/response conversion against every matching vector.
- [ ] Run every shared vector, `make vectors`, TypeScript gates, and commit one face at a time.

### Task 5: Consumer APIs, architecture checks, and CI

**Files:** add `ts/src/stream/*`, architecture tests, workflow/Makefile wiring, and runtime tests.

- [ ] Write failing AsyncIterable tests proving final flush, ordered events/losses, and source error timing.
- [ ] Implement thin wrappers over the existing state machines without buffering/reinterpreting events.
- [ ] Add a failing architecture test that rejects face-to-face imports and SSE-to-IR/face imports; add Node 20 and Web Runtime test commands to CI.
- [ ] Run full Go plus TypeScript checks and commit integration changes.

### Task 6: Package and release-preparation gates

**Files:** `ts/package.json`, `ts/README.md`, package exports, package tests, root docs/CI as required.

- [ ] Write a failing `npm pack --json --dry-run` test that asserts compiled exports/types/docs/licenses only, and a clean temporary consumer import/type-check test.
- [ ] Configure subpath exports for every completed face, `prepack`, correct files/types metadata, and accurate non-preview documentation.
- [ ] Add final release command that runs vector, generation, format, type, Node/Web, consumer, and Go gates; it must not publish.
- [ ] Run the full command from a clean worktree and commit release preparation.

## Completion Criteria

All shared vectors pass through TypeScript face bindings; M7 tool data remains raw-string exact; every public ESM subpath packs and imports in a clean consumer; CI runs required checks; no `npm publish` has occurred.
