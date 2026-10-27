# Function Tool Strict Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Preserve omitted, false and true function-tool strictness across all supported faces and implementations.

**Architecture:** Add an optional boolean to the shared Tool IR and canonical JSON, only for function tools. Map the three provider wire locations directly, including Responses container flatten/rebuild, without inspecting schema contents.

**Tech Stack:** JSON Schema, golden vectors, Go, TypeScript, Python, Rust, C++.

**Spec:** `spec/01-intermediate-representation.md` §3.5 and `spec/10`–`spec/12`.

## Global Constraints

- Preserve opaque input schema bytes and existing structured-output strict behavior.
- Follow specification → schema/vectors → implementations; do not change unrelated workspace files.
- No commits or publication without a user request.
- Every command has a finite timeout; do not leave background jobs or temporary artifacts.

## Review Focus

- Omitted strict must remain omitted, not become false.
- Explicit false must survive canonical JSON and all face mappings.
- Non-function IR tools must reject strict structurally.
- Nested Responses containers and additional_tools must retain strict through flatten and rebuild.
- Existing structured-output strict and native tools must not change behavior.

### Task 1: Contract

**Files:** `spec/01-intermediate-representation.md`, `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md`, `spec/schema/ir.schema.json`.

- [x] Specify tri-state function-only strict and the three bidirectional mappings, including Responses nested tools.
- [x] Add `strict` boolean to only the function schema variant; check JSON syntax and validate examples of all four variants.

### Task 2: Shared vectors

**Files:** `vectors/{responses,chatcompletions,anthropic,cross}/nonstream/`, `vectors/manifest.json`.

- [x] Change the existing Responses strict-loss vector into a preservation vector; add omitted/false/true, nested, additional_tools, cross-face and invalid IR cases supported by the vector harness.
- [x] Run vector validation and regenerate the manifest using `go run ./cmd/veccheck -root .. -write-manifest` from `go/`.
- [x] Run each language vector suite and record expected pre-implementation failures.

### Task 3: Implement each language

**Files:** `go/`, `ts/src/`, `python/src/`, `rust/crates/`, `cpp/include/`, `cpp/src/`, and their tests.

- [x] For each language, add a tri-state optional field to Tool and canonical codec; reject it on non-function types.
- [x] Map each wire field on decode and encode, retaining Responses container annotations; remove obsolete Responses strict losses.
- [x] Run that language's tests, including shared vectors and canonical false/omitted/true round trips, before proceeding.

### Task 4: Final verification

- [x] Run vector validation, all five language test suites, relevant formatting and lint checks with bounded timeouts.
- [x] Inspect `git diff --check` and working-tree status; report any failed or skipped validation and all remaining gaps.
