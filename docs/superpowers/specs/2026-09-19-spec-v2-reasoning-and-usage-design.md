# Spec 2.0 — Reasoning Content and Usage Granularity (Design Draft)

Date: 2026-09-19
Status: APPROVED for planning. Spec 2.0 implementation plan tracked in
`docs/superpowers/plans/2026-09-19-spec-v2-reasoning-usage-wave1.md`.

## 1. Problem

All three faces now emit reasoning content, and the IR v1 has nowhere to put it:

- Anthropic Messages: `thinking` content blocks with `thinking_delta` /
  `signature_delta` streaming; `thinking: {type: "enabled", budget_tokens}`
  on requests.
- OpenAI Responses: `reasoning` output items (`summary[]` text parts,
  optional `encrypted_content`); `reasoning: {effort, summary}` request
  object; `response.reasoning_summary_text.delta` streaming.
- OpenAI Chat Completions: `reasoning_effort` request parameter and the
  de-facto `reasoning_content` message field (and `delta.reasoning_content`
  when streaming), widely emitted by compatible providers.

Today spec/20 §9 requires an `unsupported-semantic` loss for AN thinking
blocks; Responses reasoning items record `unmapped-field` losses
(`responses.nonstream.reasoning-item-loss-response-to-ir`); CC
`reasoning_effort` is a named asymmetry in spec/03 §3. Every conversion
silently strips the most characteristic output of current models.

Usage has the same problem at smaller scale: IR `Usage` carries only
`input_tokens`/`output_tokens`, so cache read/creation tokens (AN) and
cached/reasoning token details (CC, RE) are dropped — the numbers cost
dashboards need.

## 2. Governance decision

spec/README freezes the 1.0.x series as additive-only and makes
sealed-union/enum extension a major bump. Reasoning requires new Block and
Delta variants, therefore it is a major: **spec 2.0.0**. This design also
codifies the post-1.0 evolution ladder:

- **1.0.x (patch)** — documentation only (the current doc-consistency batch
  lands as spec 1.0.1).
- **1.x (minor)** — additive optional members on non-sealed types. Usage
  granularity qualifies on its own.
- **2.0.0 (major)** — any sealed union or enum extension (new Block/Delta
  variants, new stop reasons, new ToolChoice modes).

**Decision point A — bundle or sequence?**
Approved: **A1** — spec 2.0.0 bundles reasoning + usage granularity in one bump.
One migration story, one five-language implementation wave; reasoning
without reasoning-token usage is incomplete for cost dashboards.

## 3. Approaches considered for carrying reasoning

- **R1 (approved): first-class IR semantics.** New sealed variants
  ThinkingBlock / ThinkingDelta / SignatureDelta with per-face mapping and
  loss rules. Correctness is argued against the IR once, per 00 §4.
- **R2: opaque sidecar** (N-AN-4-style raw passthrough). Smaller IR change,
  but two tiers of block semantics; stream equivalence (00 §3) cannot see
  it; cross vectors cannot compare it. Rejected.
- **R3: keep dropping, document loudly.** Does not close the gap. Rejected.

## 4. Approved IR shape (normative text lands in spec/01 + schema)

- `ThinkingBlock`, JSON `type: "thinking"` (Decision E: naming matches AN wire):
  - `thinking`: string, required — the model's reasoning text.
  - `signature`: string, optional — opaque provider integrity token (AN);
    carried verbatim, never validated or rewritten (INV-1-style opacity).
- `ThinkingDelta`, JSON `type: "thinking_delta"`: `text`.
- `SignatureDelta`, JSON `type: "signature_delta"`: `signature`; zero or
  one per block, emitted after the thinking deltas.

INV-5 correspondence extension: a `ThinkingBlock` admits `thinking_delta*`
followed by at most one `signature_delta`; other block rules unchanged.
INV-6 unchanged. Response content and request assistant messages MAY carry
ThinkingBlocks (01 §3.4/§4.1 notes updated).

Request-side (Decision B): `Params` gains optional `reasoning_effort`
(enum `minimal | low | medium | high`). CC and RE share the concept; doc 12
owns the AN `thinking.budget_tokens` conversion table.

## 5. Per-face mapping rules (normative text lands in spec/10–12)

- **AN (doc 12)**: thinking blocks map 1:1 including `signature`;
  `thinking_delta`/`signature_delta` map directly; request
  `thinking.budget_tokens` ↔ `params.reasoning_effort` via a documented
  budget table (`degraded` loss when approximating: minimal→1024, low→2048,
  medium→8192, high→16384 on encode; decode ≤2048→low, ≤8192→medium, else→high).
  Unsigned request thinking blocks record `degraded` on encode.
- **CC (doc 10)**: `reasoning_content` (message + delta) ↔
  ThinkingBlock.thinking. CC has no signature: encoding a signed
  ThinkingBlock to CC records `unmapped-field` on `signature`.
  Request `reasoning_effort` maps 1:1; unknown values dropped with `unmapped-value`.
- **RE (doc 11)**: `reasoning` output item `summary[]` parts ↔ ThinkingBlocks;
  `response.reasoning_summary_text.delta` ↔ thinking_delta;
  `response.reasoning_summary_part.added/done` ↔ block start/stop.
  Decision C1: `encrypted_content` dropped with `unmapped-field` loss.
  Request `reasoning.effort` ↔ `params.reasoning_effort`; `reasoning.summary`
  preference dropped with `unmapped-field`.

## 6. Usage granularity

`Usage` (01 §4.2, non-sealed) gains optional members; absent ≠ 0:

- `cache_read_input_tokens`, `cache_creation_input_tokens` (int64 ≥ 0) —
  AN-native; CC/RE cached-token details map onto `cache_read_input_tokens`
  where semantics match, else drop with `unmapped-field`.
- `input_tokens_details.cached_tokens`,
  `output_tokens_details.reasoning_tokens` (int64 ≥ 0) — OpenAI-native shape;
  AN has no reasoning-token count, so absent, never zero.

`total_tokens` remains a derived exempt field. INV-7 integer fidelity
applies to all new members.

## 7. Streaming profile (spec/20 new section — M9 reasoning profile)

Extends N-S-4: decoders accept and encoders emit thinking blocks/deltas.
Faces without a native concept record standard per-unit `unsupported-semantic`
losses. Terminal/Flush rules unchanged; signature_delta ordering pinned after
thinking deltas.

## 8. Vectors

Per face: nonstream and stream thinking to-ir/from-ir
(`*.stream.m9-thinking-to-ir` naming), cross vectors for every ordered pair
exercising signature-drop and the CC→AN degraded replay caveat, plus usage
granularity vectors. Manifest regenerated via
`go run ./cmd/veccheck -root .. -write-manifest`.

## 9. Rollout order

spec (00 note, 01 shapes+INV, 10/11/12 mappings, 20 profile, schema major
bump) → vectors + manifest → Go → TS → Rust → Python → C++ (precedent
order; `scripts/check-constants.py` picks up new enum values) → per-language
major releases via the existing tag automation.

## 10. Compatibility

IR document `specVersion` const bumps `0.1.0` → `0.2.0`.
Decision D: reader policy — decoders accept `0.1.0` and `0.2.0`, encoders
emit only `0.2.0`. `ir.schema.json` pins `enum: ["0.1.0", "0.2.0"]`.
Existing 130 vectors bumped mechanically to `0.2.0` in dedicated commit.
Language packages release as majors (`v2.0.0`, `go/v2.0.0`, `ts/v2.0.0`,
`rust/v2.0.0`, `py/v2.0.0`).

## 11. Out of scope for 2.0.0

Audio, documents/PDF blocks, citations/annotations, hosted/server tools,
tool_choice extensions, logprobs, response_format. Each follows the same
ladder later.

## 12. Approved decision points

- **A**: Bundle usage into 2.0.0 (**A1**, approved).
- **B**: Include request-side `reasoning_effort` (**Approved**).
- **C**: RE `encrypted_content` — drop with loss (**C1**, approved).
- **D**: Accept `specVersion 0.1.0` documents on read with enum schema (**Approved**).
- **E**: Block type name `thinking` (**Approved**).
