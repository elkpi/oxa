# Spec changelog

Version history of the oxa specification itself, independent of the
implementations. The spec follows [Semantic Versioning](https://semver.org/);
precedence between spec, vectors, and schemas is defined in
[README.md](README.md#source-of-truth-precedence).

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
