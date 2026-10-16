# Spec changelog

Version history of the oxa specification itself, independent of the
implementations. The spec follows [Semantic Versioning](https://semver.org/);
precedence between spec, vectors, and schemas is defined in
[README.md](README.md#source-of-truth-precedence).

## 2.3.0 - 2026-10-16

### Added

- [14 — Apply Patch](14-apply-patch.md) 定义专用的 `apply_patch` Tool/ToolChoice 声明、三种扁平 Operation 联合体、`ApplyPatchToolUseBlock` 与 `ApplyPatchToolResultBlock`。
- 请求端成对关联校验（N-AP-3, INV-3）与成对语义过滤；未知操作类型或未知状态成对丢弃并记录对应 loss，杜绝悬垂引用。
- Responses 原子项流式生命周期（N-AP-4），added 时暂存快照，done 时验证并回放单组 block start/stop，活跃期间严禁任何增量。
- Chat Completions 与 Anthropic Messages 面对 Apply Patch 的规范降级（N-AP-5），记录 `unsupported-semantic` loss 并正确降级 `stop_reason` 至 `end_turn`。
- 新增 58 条 Apply Patch 黄金向量，全仓库向量总数达到 263 条；IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）。

## 2.2.0 - 2026-10-16

### Added

- [13 — Custom Tools](13-custom-tools.md) 定义独立的 custom 调用/结果 Block 和文本 Delta，扩展 Tool/ToolChoice 及 INV-1/3/4/5。
- Responses 非流式与流式双向映射，含任意文本、grammar、内容列表、关联及不支持目标的显式 loss。
- IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）；新增 20 个共享向量，总数 205。
- 规划在后续版本（如 2.3.0）补齐 Chat Completions 与 Anthropic Messages 的对等能力支持。

## 2.1.x - Unreleased

### Added

- Responses input messages accept `developer` alongside `system`, `user`, and
  `assistant`. N-R-1 retains instruction precedence over user content and the
  shared system/developer encounter order after top-level `instructions`.
- Developer instructions map to the existing IR system field with one
  `degraded` loss per developer role; the role distinction cannot round-trip.
- Instruction-only requests gain an empty user message with an explicit
  `degraded` loss, preserving the existing IR request schema.
- Twelve shared vectors cover developer messages, content parts, ordering,
  instruction-only normalization, and conversion to Chat Completions and
  Anthropic Messages. No IR schema or contract version changes.

## 2.1.0 - 2026-09-27

Minor evolution adding structured-output format mapping across all protocol
faces. IR documents continue to use `specVersion: "0.2.0"`.

### Added

- `Params.ResponseFormat` supports `text`, `json_object`, and `json_schema`
  (`name`, optional `description`, verbatim `schema`, optional `strict`).
- Chat Completions `response_format` and Responses `text.format` map losslessly
  through IR; Anthropic encoding drops the preference with one documented
  `unmapped-field` loss and does not synthesize tools.
- Nine shared golden vectors cover the new face and cross-protocol mappings;
  all five language implementations validate 163 vectors.

### Clarified

- The existing spec/02 unknown-field rule applies recursively to recognized
  face objects and native stream event objects. A whole unsupported unit still
  produces only its whole-unit loss; opaque payloads are not inspected. No IR,
  schema, or loss reason is added.
- Raw JSON implementations preserve source encounter order when combining
  field-level and semantic losses.

## 2.0.0 - 2026-09-19

Major milestone extending the protocol-conversion specification with reasoning
content, request-side reasoning effort, and granular usage accounting.

### Added

- **Reasoning content**: `ThinkingBlock` with model reasoning text and optional
  opaque provider `signature` (spec/01 §3.4); streaming `ThinkingDelta` and
  `SignatureDelta` (spec/01 §5.2); M9 streaming reasoning profile (spec/20 §11,
  rule N-S-11).
- **Request reasoning effort**: `Params.ReasoningEffort` enum (`minimal`, `low`,
  `medium`, `high`), mapped natively to/from OpenAI CC and Responses, and mapped
  to/from Anthropic `thinking.budget_tokens` via documented approximation table.
- **Usage granularity**: optional `cache_read_input_tokens`,
  `cache_creation_input_tokens`, `input_tokens_details.cached_tokens`, and
  `output_tokens_details.reasoning_tokens` (spec/01 §4.2).
- Per-face mappings: N-CC-12 (Chat Completions `reasoning_content`), N-R-13
  (Responses `reasoning` item and summary parts), and N-AN-11 (Anthropic
  `thinking` blocks and budget mapping).

### Changed

- Specification version promoted to **2.0.0** following sealed-union extensions
  for `Block` and `Delta`.
- The IR contract `specVersion` supports both `"0.1.0"` and `"0.2.0"` on read;
  converters emit `"0.2.0"`.

## 1.0.1 - 2026-09-19

### Added

- Glossary (`spec/90-glossary.md`): shared cross-language terminology for
  faces, the IR, invariants, vectors, losses, and streaming profiles.
  Documentation only: no rule, schema, or vector change; the IR contract
  `specVersion` remains `0.1.0`.

### Changed

- Loss conventions: normative bucket taxonomy (derived, envelope, unmapped)
  migrated into `spec/02-loss-policy.md` §9; per-face mapping documents (10–12)
  and `vectors/README.md` cross-referenced accordingly with no behavioral change.

## 1.0.0 - 2026-09-04

All four supported languages — Go, Rust, Python, and C++ — implement the
specification against the identical 125 golden vectors. The specification reaches
1.0.0 stability.

### Changed

- Specification promoted to 1.0.0 following the multi-language implementation
  milestones (0.0.x Go, 0.1.0 Rust, 0.2.0 Python, 0.3.0 C++, 1.0.0 all languages).
- Future additions to sealed unions or enums will constitute major version bumps.

## 0.0.2 - 2026-09-02

The 0.0 series is **frozen** as the specification baseline for the Rust
implementation (see the versioning policy in [README.md](README.md)).

### Added

- Versioning policy (README): the two version axes (spec version vs. the
  IR contract `specVersion`), the cadence tied to the implementation
  roadmap (0.0.x Go baseline, 0.1.0 Rust, 0.2.0 Python, 0.3.0 C++, 1.0.0
  only once every supported language implements the spec against the same
  vectors), and the additive-only freeze rules.

### Changed

- Fixed stale forward references: mapping documents 10–12 are no longer
  marked "planned" in 00 §1 and no longer call 20 "planned" in their
  §6; 00 §3 cites document 20 as landed; the glossary (90) notes when it
  will land.
- Pinned the RE streaming encoder's zero-delta rule: a tool block with no
  supplied argument deltas synthesizes one full-arguments
  `response.function_call_arguments.delta`, matching the CC and AN
  encoders and the Go implementation (20 §10.1).
- Repointed the schema `$id` identifiers from the `oxa-protocol`
  placeholder to `elkpi` (01 §6): v0.0.1 already shipped the final module
  path. Identifier-only change; no validation behavior.

## 0.1.1 - 2026-08-28

### Changed

- Clarified N-AN-4: raw tool-input byte fidelity is normative for
  JSON-decoded (typed) paths; generic in-memory inputs convert with
  canonicalized bytes. No schema or vector-format change.

## 0.1.0 - 2026-08-28

Initial spec core (00–03, IR and loss schemas).

### Added

- `spec/00` — scope, non-goals, stream-equivalence principle, hub-and-spoke
  architecture.
- `spec/01` — the intermediate representation: request, message, block,
  response, and event types; invariants INV-1 through INV-9.
- `spec/02` — loss policy: the loss record, reason codes, the error/loss
  boundary, streaming rules.
- `spec/03` — model handling: verbatim pass-through, the modelmap
  injection point, protocol-parameter asymmetry principles.
- `spec/schema/ir.schema.json` and `spec/schema/loss.schema.json`
  (JSON Schema 2020-12).
