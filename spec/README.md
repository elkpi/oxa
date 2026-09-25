# oxa specification

This directory contains the normative specification for oxa, a collection of
pure protocol-conversion libraries translating between three protocol faces —
OpenAI Chat Completions (CC), OpenAI Responses, and Anthropic Messages —
through a hub-and-spoke intermediate representation (IR). The specification
is the contract every implementation (Go first, then TypeScript, Rust, Python,
and C++) MUST satisfy.

The specification versions itself independently of the implementations.
Current spec version: **2.0.0** — all five implementations (Go, TypeScript,
Rust, Python, and C++) implement and validate the full 154-vector set including
reasoning and usage. See [CHANGELOG.md](CHANGELOG.md) and
the versioning policy below.

## Versioning policy

The specification carries two version axes:

- **Spec version** — declared here and recorded in
  [CHANGELOG.md](CHANGELOG.md): the version of the written specification
  itself. A patch release (0.0.x, 1.0.x) clarifies wording without changing any
  rule; a minor release (0.x → 0.(x+1)) may add semantics under the
  evolution rules below; a major release (2.0.0) is required for sealed-union
  or enum extensions.
- **IR contract version** — the `specVersion` property declared in
  [`spec/schema/ir.schema.json`](schema/ir.schema.json) and echoed by
  every vector's `spec_version`: the version of the IR document shapes
  themselves. It changes only when the IR contract changes and may lag
  the spec version.

The version series are tied to the implementation roadmap:

| Version | Scope |
|---------|-------|
| 0.0.x | The specification baseline and the Go reference implementation |
| 0.1.0 | The Rust implementation |
| 0.2.0 | The Python implementation |
| 0.3.0 | The C++ implementation |
| 1.0.0 | All supported languages — Go, Rust, Python, and C++ — implement the same spec and vector set |
| 1.0.x | The TypeScript implementation — added after 1.0.0 with no spec change, validated against the identical vector set |
| 2.0.0 | Reasoning content and usage granularity across all faces (Go Wave 1; TypeScript Wave 2; Rust Wave 3; Python Wave 4; C++ Wave 5) |

Post-1.0 evolution ladder:

- **Patch releases (1.0.x)** — wording clarifications, documentation, and
  glossary additions with zero schema, rule, or behavioral change.
- **Minor releases (1.x)** — additive optional members on non-sealed types
  (such as additional parameters or non-breaking usage fields).
- **Major releases (2.x)** — sealed union or enum extensions (such as new
  `Block` or `Delta` variants, new stop reasons, or new tool choice modes).

Freeze rules for every series:

1. A frozen series evolves only additively. New optional semantics may
   arrive with a minor bump; within a series, existing rules, IR shapes,
   and loss semantics MUST NOT change (clarifications are patch
   releases).
2. Extending a sealed union or an enum
   ([01, §2](01-intermediate-representation.md)) after 1.0 is a major
   bump.
3. The behavioral source of truth stays `vectors/`: any spec change that
   alters observable behavior MUST land together with the vector change
   that pins it.

## Reading order

Read the documents in this order:

1. [00 — Scope and Architecture](00-scope-and-architecture.md)
2. [01 — Intermediate Representation](01-intermediate-representation.md)
3. [02 — Loss Policy](02-loss-policy.md)
4. [03 — Model Handling](03-model-handling.md)
5. 10–12 — per-face mapping documents (one per face)
6. [20 — streaming semantics](20-streaming-semantics.md)
7. [90 — glossary](90-glossary.md)

| Document | Scope | Status |
|----------|-------|--------|
| [00](00-scope-and-architecture.md) | scope, non-goals, architecture | ready |
| [01](01-intermediate-representation.md) | the IR: types and invariants | ready |
| [02](02-loss-policy.md) | loss reporting | ready |
| [03](03-model-handling.md) | model handling | ready |
| [10](10-mapping-openai-chat-completions.md) | Chat Completions face mapping | ready |
| [11](11-mapping-openai-responses.md) | Responses face mapping | ready |
| [12](12-mapping-anthropic-messages.md) | Anthropic Messages face mapping | ready |
| [20](20-streaming-semantics.md) | streaming semantics | ready |
| [90](90-glossary.md) | glossary | ready |

Documents marked planned are intentionally not created yet; each arrives with
its milestone. Documents marked ready are complete for their stated scope.

## Source-of-truth precedence

When artifacts disagree, precedence is:

1. **`vectors/`** — behavior. The golden vectors define what correct
   conversion output is.
2. **`spec/schema/*.json`** — structure. The JSON Schemas define the exact
   shape of IR documents and loss records.
3. **Markdown (`spec/*.md`)** — semantics and rationale.

A conflict is a bug in the upstream artifact: if a vector contradicts a
schema, the schema is wrong and MUST be fixed (and, if the Markdown led the
schema astray, so must the Markdown); if a schema contradicts the Markdown,
the schema wins on structural questions and the Markdown MUST be fixed. The
Markdown remains authoritative for everything the other two artifacts cannot
express: semantics, rationale, and procedure.

## Numbering convention

- `0x` — cross-cutting documents (scope, IR, loss policy, model handling)
- `1x` — per-face mapping documents
- `2x` — streaming semantics
- `9x` — appendices

## Requirements language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
specification are to be interpreted as described in RFC 2119 and RFC 8174
when, and only when, they appear in all capitals, as shown here. This
boilerplate applies to every document under `spec/`.

## How the spec is enforced

The specification is one of three locking deliverables (see
[00, §5](00-scope-and-architecture.md)): `spec/` (this contract), `vectors/`
(the behavioral source of truth), and the per-language implementations. CI
enforces their consistency: vectors are validated against the schemas,
implementations are validated against the vectors, and no layer may drift
from the layer above it in the precedence chain.
