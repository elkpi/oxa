# Spec 2.0.0 Wave 1 — Reasoning Content + Usage Granularity (spec, schema, vectors, Go)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the approved spec 2.0.0 design (thinking/reasoning blocks with
signatures, request-side `reasoning_effort`, usage granularity) through the
repo's locking order — spec → schema → vectors → Go reference — plus the
already-approved P0 documentation batch as Phase 0.

**Architecture:** Hub-and-spoke unchanged. Sealed unions `Block` and `Delta`
gain variants (`thinking` block; `thinking_delta`, `signature_delta`).
`Params` gains optional `reasoning_effort`; `Usage` gains optional
cache/detail members. Streaming extends via a new M9 profile (N-S-11).
IR contract version moves 0.1.0 → 0.2.0 (dual-read).

**Tech Stack:** Go 1.23+ (reference), JSON Schema 2020-12, `cmd/veccheck`,
golden-vector CI.

**Spec:** `docs/superpowers/specs/2026-09-19-spec-v2-reasoning-and-usage-design.md`
(approved decision points: A1 bundle usage, B include `reasoning_effort`,
C1 drop `encrypted_content` with loss, D accept `specVersion 0.1.0` on read,
E block type name `thinking`).

## Global Constraints

- Precedence is locking: spec → vectors → implementation. A behavior change
  lands with its vector in the same series, implementation after.
- Spoke packages import only stdlib, `ir`, `modelmap` (`go/internal/deps`).
- INV-1 opacity extends to `ThinkingBlock.signature`: carry verbatim, never
  validate or rewrite. `thinking` text is a normal string (exact comparison).
- Sealed-union extension => spec major 2.0.0; everything else in this plan is
  additive. IR document `specVersion`: converters EMIT `"0.2.0"`, decoders
  ACCEPT `"0.1.0"` and `"0.2.0"`; `ir.schema.json` pins `enum: ["0.1.0","0.2.0"]`.
- Streaming M6/M7 semantics are untouched outside the new M9 section.
- `absent ≠ 0` for every new optional `Usage`/`Params` member (pointer-style).
- Unknown inbound `reasoning_effort` values: drop the param + `unmapped-value`
  loss (enums never extend inbound).
- Vector naming per `vectors/README.md`; expected losses separate from events;
  manifest regenerated and committed with any vector change.
- Commit granularity: one logical change per commit, cherry-pickable; spec,
  vectors, and implementation are separate commits unless a test verifies the
  implementation in the same change.
- Verification commands run from `go/` unless a Make target is used:
  `go test -count=1 ./...`, `go vet ./...`, `gofmt -l .`,
  `go run ./cmd/veccheck -root .. -check-manifest`.

---

## Phase 0 — P0 documentation batch (spec 1.0.1)

- [ ] **Task 1: doc drift (commit `docs: correct golden-vector count (125->130) and refresh released versions to v1.0.1`)**
  `README.md`: status line -> `v1.0.1 released`; `125 golden vectors` -> `130`;
  language matrix + directory overview + Rust paragraph `v1.0.0` -> `v1.0.1`.
  `spec/README.md` line "identical 125 golden" -> `130`. Do NOT touch historical
  CHANGELOG entries.
- [ ] **Task 2: TS policy (commit `spec: add TypeScript to the implementation version policy`)**
  `spec/README.md`: "all four supported languages (Go, Rust, Python, and C++)"
  -> "all five supported languages (Go, TypeScript, Rust, Python, and C++)";
  append version-table row `| 1.0.x | The TypeScript implementation — added after 1.0.0 with no spec change, validated against the identical vector set |`.
- [ ] **Task 3: glossary (commit `spec: add glossary (90), bump spec to 1.0.1`)**
  Create `spec/90-glossary.md` with the approved 36-term glossary; `spec/README.md`:
  version -> **1.0.1**, reading-order item 7 and status-table row 90 -> link + `ready`;
  add the `## 1.0.1 - 2026-09-19` section to `spec/CHANGELOG.md`.
- [ ] **Task 4: loss-catalog migration (commit `spec: migrate loss conventions into normative per-face loss catalogs`)**
  Append `## 9. Loss conventions` to `spec/02-loss-policy.md` (bucket taxonomy);
  update references in spec/10/11/12 loss catalogs to point to spec/02 §9;
  replace the two migrated sections in `vectors/README.md` with a pointer;
  append the Changed bullet to the 1.0.1 changelog section.

---

## Wave 1 — spec 2.0.0 documents, schema, vectors, Go

### Task 5: spec/01 — IR types

**Files:** Modify `spec/01-intermediate-representation.md`
**Interfaces:** Produces the type contract every later task argues from.

- [ ] Edit §2 opaque note: append "A `ThinkingBlock`'s `signature` is carried
  verbatim with the same discipline as raw JSON text: never validated or
  rewritten."
- [ ] §3.4 Block table: add row
  `| ThinkingBlock | thinking | the model's reasoning content, with optional opaque provider signature |`;
  add a ThinkingBlock subsection:
  `thinking` string, required, non-empty; `signature` string, optional, opaque.
- [ ] §3.7 Params: add `ReasoningEffort | reasoning_effort | enum minimal|low|medium|high | no | absent means unset` and note: unknown inbound values are dropped with `unmapped-value` (mapping docs).
- [ ] §4.1 note: responses contain TextBlock, ToolUseBlock, and — since 2.0 — ThinkingBlock; request assistant messages MAY carry ThinkingBlocks for replay.
- [ ] §4.2 Usage table: add `CacheReadInputTokens | cache_read_input_tokens | int64 ≥ 0 | no`, `CacheCreationInputTokens | cache_creation_input_tokens | int64 ≥ 0 | no`, `InputTokensDetails | input_tokens_details | {cached_tokens int64 ≥ 0} | no`, `OutputTokensDetails | output_tokens_details | {reasoning_tokens int64 ≥ 0} | no`; absent ≠ 0.
- [ ] §5.2 Delta table: add `ThinkingDelta | thinking_delta | Text` and
  `SignatureDelta | signature_delta | Signature`; extend the correspondence
  sentence: "a `ThinkingBlock` admits `thinking_delta*` followed by at most
  one `signature_delta`".
- [ ] §7 INV-1: include `signature` in the opaque-carry sentence. INV-5 text
  itself is unchanged (block/delta matching lives in §5.2).
- [ ] Commit: `spec(01): add thinking block, reasoning effort param, and usage details (2.0)`

### Task 6: spec/00 + spec/02 + spec/03 touch-ups

- [ ] `spec/00` §1: add to the covered list: "Reasoning content: thinking
  blocks with opaque signatures, streaming thinking deltas, and request-side
  reasoning effort (since 2.0)." and "Usage granularity: cache and token
  detail accounting (since 2.0)."
- [ ] `spec/03` §3: note `reasoning_effort` became a first-class Params member
  in 2.0; CC/RE map it natively, AN via the doc-12 budget table.
- [ ] Commit: `spec(00,03): cover reasoning content and usage granularity in scope notes`

### Task 7: spec/10/11/12 — per-face mapping rules

**Interfaces:** Stable rule IDs: N-CC-12, N-R-13, N-AN-11 (accounting for existing N-CC-11 and N-AN-10).

- [ ] `spec/10` add **N-CC-12**: decode `message.reasoning_content` (nonstream)
  and `choices[0].delta.reasoning_content` (stream) -> `ThinkingBlock.thinking`
  / `thinking_delta`; CC never supplies `signature`. Encode: ThinkingBlock ->
  `reasoning_content`; a present `signature` records
  `{path:"content[i].signature", field:"signature", reason:"unmapped-field"}`;
  request `reasoning_effort` ↔ `params.reasoning_effort` 1:1, unknown values
  dropped with `unmapped-value`.
- [ ] `spec/11` add **N-R-13**: decode `reasoning` output item `summary[]`
  parts -> ThinkingBlocks (part order preserved); `encrypted_content` dropped
  with `{path:"output[i].encrypted_content", field:"encrypted_content",
  reason:"unmapped-field"}` (Decision C1); streaming:
  `response.reasoning_summary_part.added/done` = block start/stop,
  `response.reasoning_summary_text.delta` = `thinking_delta`;
  request `reasoning.effort` ↔ `params.reasoning_effort`;
  `reasoning.summary` preference dropped with `unmapped-field`.
- [ ] `spec/12` add **N-AN-11**: `thinking` blocks map 1:1 incl. `signature`
  (verbatim); `thinking_delta`/`signature_delta` map directly;
  request `thinking.budget_tokens` ↔ `params.reasoning_effort` via:
  minimal->1024, low->2048, medium->8192, high->16384 (encode), and decode
  ≤2048->low, ≤8192->medium, else->high, each with
  `{path:"params"/"thinking", field:"reasoning_effort"|"budget_tokens",
  reason:"degraded", detail:"budget approximated"}`; a request-side unsigned
  ThinkingBlock encodes as a thinking block WITHOUT signature plus
  `{path:"content[i]", field:"signature", reason:"degraded",
  detail:"unsigned thinking block; Anthropic may reject on replay"}`.
- [ ] Commit: `spec(10-12): add reasoning mapping rules N-CC-12, N-R-13, N-AN-11`

### Task 8: spec/20 — M9 profile + catalog update

- [ ] Add `## 12. M9 reasoning profile — N-S-11` (renumber References): M9
  extends the stream contract with thinking blocks; grammar per INV-5 +
  §5.2 correspondence; `signature_delta` only after `thinking_delta*` within
  the same block, at most one; block closes with its own
  `content_block_stop`; terminal/Flush rules unchanged; a face-decoder
  receiving reasoning it cannot represent uses standard N-S-3 absorption.
- [ ] §9 catalog: change the AN row to "AN `server_tool_use`,
  provider-hosted tool" (thinking is now converted, not dropped).
- [ ] Commit: `spec(20): add M9 reasoning streaming profile (N-S-11)`

### Task 9: spec/schema — ir.schema.json

**Files:** Modify `spec/schema/ir.schema.json`

- [ ] Add `$defs`: `ThinkingBlock`, `ThinkingDelta`, `SignatureDelta`,
  `InputTokensDetails`, `OutputTokensDetails`.
- [ ] Add `ThinkingBlock` to the Block `oneOf`; `ThinkingDelta`,
  `SignatureDelta` to the Delta `oneOf`; the four usage members to `Usage`
  (integers ≥ 0, details via `$ref`); `reasoning_effort` enum
  `["minimal","low","medium","high"]` to Params.
- [ ] `specVersion` const -> `enum: ["0.1.0", "0.2.0"]` in all three document
  forms (Request/Response/EventStream). Converters emit `0.2.0`; acceptance
  of `0.1.0` is converter behavior (Task 15).
- [ ] Update `go/cmd/veccheck` to accept `spec_version` matching any value in the schema enum.
- [ ] Commit: `spec(schema): add thinking/usage shapes and dual specVersion (2.0)`

### Task 10: spec/README + CHANGELOG — version 2.0.0

- [ ] `spec/README.md`: current version -> **2.0.0**; add the post-1.0
  evolution ladder (patch=docs, minor=additive non-sealed, major=sealed
  union/enum) to the versioning policy; add `| 2.0.0 | Reasoning content and usage granularity across all faces |` to the scope table.
- [ ] `spec/CHANGELOG.md`: `## 2.0.0 - <landing date>` with Added (thinking
  block/deltas, reasoning_effort, usage members, M9) and Changed (specVersion
  0.2.0 dual-read; M6/M7 unchanged).
- [ ] Commit: `spec: promote specification to 2.0.0`

### Task 11: vectors — mechanical IR-contract bump of the existing 130

- [ ] In `vectors/`, update IR-side `specVersion` / `spec_version` from `0.1.0` to `0.2.0`.
- [ ] Regenerate: `cd go && go run ./cmd/veccheck -root .. -write-manifest`.
- [ ] Commit: `vectors: bump IR contract version to 0.2.0` (manifest included).

### Task 12: vectors — thinking, nonstream (6 new vectors)

1. `anthropic.nonstream.thinking-response-to-ir`
2. `anthropic.nonstream.thinking-response-from-ir`
3. `chatcompletions.nonstream.thinking-response-to-ir`
4. `chatcompletions.nonstream.thinking-response-from-ir`
5. `responses.nonstream.thinking-item-to-ir`
6. `responses.nonstream.thinking-item-from-ir`
- [ ] Author, validate: `cd go && go run ./cmd/veccheck -root .. -write-manifest`; commit `vectors: add nonstream thinking vectors (m9)`.

### Task 13: vectors — thinking, stream M9 (4 new vectors)

1. `anthropic.stream.m9-thinking-to-ir`
2. `chatcompletions.stream.m9-reasoning-to-ir`
3. `responses.stream.m9-reasoning-summary-to-ir`
4. `chatcompletions.nonstream.specversion-010-from-ir` (Decision D pin)
- [ ] Author, validate, regenerate manifest, commit `vectors: add M9 stream thinking vectors`.

### Task 14: vectors — request effort/budget + usage granularity (8 new vectors)

- `chatcompletions.nonstream.reasoning-effort-request-to-ir/-from-ir`
- `responses.nonstream.reasoning-request-to-ir`
- `anthropic.nonstream.thinking-budget-request-to-ir/-from-ir`
- `anthropic.nonstream.usage-cache-tokens-to-ir`
- `chatcompletions.nonstream.usage-details-to-ir`
- `responses.nonstream.usage-details-to-ir`
- Cross (3): `cross.nonstream.anthropic-to-chatcompletions-thinking-response`,
  `cross.nonstream.chatcompletions-to-anthropic-thinking-response`,
  `cross.nonstream.responses-to-anthropic-thinking-response`.
- [ ] Author, validate, regenerate manifest, commit `vectors: add reasoning request and usage granularity vectors`.

### Task 15: Go — IR types and document codec

**Files:** Modify `go/ir`
**Produces:** `ir.ThinkingBlock`, `ir.ThinkingDelta`, `ir.SignatureDelta`,
`Params.ReasoningEffort`, `Usage` extended fields, and document codec emitting
`0.2.0` while accepting `0.1.0`.

- [ ] Add types following existing sealed-union idiom.
- [ ] Document codec: emit `0.2.0`; decode: accept `0.1.0` and `0.2.0`.
- [ ] Validator tests for new block/delta rules.
- [ ] Run: `cd go && go test -count=1 ./ir/... && go vet ./... && gofmt -l .`
- [ ] Commit: `feat(go/ir): add thinking block, reasoning effort, usage details (spec 2.0)`

### Task 16: Go — three spokes (decode/encode, nonstream + stream)

- [ ] chatcompletions: tests green including new vectors; commit `feat(go/chatcompletions): reasoning content and usage details (N-CC-12)`.
- [ ] responses: tests green including new vectors; commit `feat(go/responses): reasoning items and usage details (N-R-13)`.
- [ ] anthropic: tests green including new vectors; commit `feat(go/anthropic): thinking blocks and budget mapping (N-AN-11)`.
- [ ] Whole suite + gates: `make test && make lint && make vectors && cd go && go run ./cmd/veccheck -root .. -check-manifest && go test -race -count=1 ./...`

### Task 17: docs sync

- [ ] Root `README.md`: capabilities table streaming cells gain "thinking / reasoning events"; golden-vector count updated.
- [ ] Commit: `docs: refresh capability matrix and vector count for spec 2.0`

## Waves 2–5 (separate plans)

TS -> Rust -> Python -> C++, one plan each, same vectors, ordered after Wave 1
goes green; then per-language major releases via existing tag automation.
