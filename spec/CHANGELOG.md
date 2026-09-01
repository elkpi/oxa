# Spec changelog

Version history of the oxa specification itself, independent of the
implementations. The spec follows [Semantic Versioning](https://semver.org/);
precedence between spec, vectors, and schemas is defined in
[README.md](README.md#source-of-truth-precedence).

## 0.1.2 - 2026-09-02

The 0.1 series is **frozen** as the baseline for the Rust implementation
(see the versioning policy in [README.md](README.md)).

### Added

- Versioning policy (README): the two version axes (spec version vs. the
  IR contract `specVersion`), the 0.x cadence tied to the implementation
  roadmap (0.1 Go + Rust baseline, 0.2 Python, 0.3 C++, 1.0 only once
  every supported language implements the spec against the same vectors),
  and the additive-only freeze rules.

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
