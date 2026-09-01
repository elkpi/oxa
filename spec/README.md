# oxa specification

This directory contains the normative specification for oxa, a collection of
pure protocol-conversion libraries translating between three protocol faces —
OpenAI Chat Completions (CC), OpenAI Responses, and Anthropic Messages —
through a hub-and-spoke intermediate representation (IR). The specification
is the contract every implementation (Go first, then Rust, Python, and C++)
MUST satisfy.

The specification versions itself independently of the implementations.
Current spec version: **0.1.2** — the 0.1 series is **frozen** as the
baseline for the Rust implementation. See
[CHANGELOG.md](CHANGELOG.md) and the versioning policy below.

## Versioning policy

The specification carries two version axes:

- **Spec version** — declared here and recorded in
  [CHANGELOG.md](CHANGELOG.md): the version of the written specification
  itself. A patch release (0.1.x) clarifies wording without changing any
  rule; a minor release (0.x → 0.(x+1)) may add semantics under the
  evolution rules below.
- **IR contract version** — the `specVersion` property pinned by `const`
  in [`spec/schema/ir.schema.json`](schema/ir.schema.json) and echoed by
  every vector's `spec_version`: the version of the IR document shapes
  themselves. It changes only when the IR contract changes and may lag
  the spec version.

The `0.x` series is tied to the implementation roadmap:

| Spec series | Scope |
|-------------|-------|
| 0.1 | The Go reference implementation; frozen baseline for the Rust implementation |
| 0.2 | The Python implementation |
| 0.3 | The C++ implementation |
| 1.0 | All supported languages — Go, Rust, Python, and C++ — implement the same spec and vector set |

The version is NOT promoted to 1.0 until every supported language
implements the spec against the same vectors. Freeze rules for every 0.x
series:

1. A frozen series evolves only additively. New optional semantics may
   arrive with a minor bump; within a series, existing rules, IR shapes,
   and loss semantics MUST NOT change (clarifications are patch
   releases).
2. Extending a sealed union or an enum
   ([01, §2](01-intermediate-representation.md)) before 1.0 is a minor
   bump; after 1.0 it is a major bump.
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
7. 90 — glossary (planned)

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
| 90 | glossary | planned |

Documents marked planned are intentionally not created yet; each arrives with
its milestone. The glossary (90) lands when the multi-language implementations
first need shared terminology, starting with Rust. Documents marked ready are
complete for their stated scope.

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
