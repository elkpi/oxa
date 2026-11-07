# 17 — Mapping: SystemOne Jev

This document is the normative mapping between the SystemOne Jev face and the
oxa intermediate representation (spec/01). Jev is a structured decision
protocol, not a conversation protocol: a request evaluates a `state` against
a fixed set of `questions` and returns typed `answers`. Conversion therefore
projects evaluation semantics onto the IR conversation model — it is a
deterministic, loss-reporting projection, not a face-to-face chat
translation. It covers the non-streaming conversion implemented by the Go
spoke `go/systemone/jev`. Jev has no streaming mode; spec/20 does not apply
to this face.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" are to be
interpreted as described in RFC 2119 and RFC 8174 (see
[README.md](README.md)).

## 1. Overview

The Jev face is the TypeSafe AI `POST /v1/systemone` protocol: a `model`, a
`state` (the content evaluated), and a map of caller-keyed `questions`
(`noul`, `choice`, `score`), answered by typed `answers` with token usage.
Conversion to the IR is defined for the supported subset (string/object/array
state, the three question types); experimental capabilities (`assets`, `box`
questions) are recorded as losses. Errors are reserved for structural
violations: a missing `model` or `state`, an empty `questions` map, criteria
of the wrong shape, a response whose aggregated text is not a JSON object,
or a missing answer key.

Jev is a client-side protocol: callers speak Jev and the IR carries the
evaluation into a conversational face. The reverse directions (IR → Jev) are
defined for face completeness and lock their own fidelity rules.

## 2. Wire Objects

Reference snapshot (retrieved 2026-11-07):

- HTTP API reference: <https://jevwiki.ai/wiki/reference/http-api.md>
- Worked example: <https://docs.digitalocean.com/products/inference/how-to/use-system-one-api/>

The wire objects in scope are:

- **Request**: `model`, `state` (string | object | array), `questions`
  (map of caller-chosen IDs to question objects), `assets` (experimental,
  out of scope).
- **Question**: `type` (`noul` | `choice` | `score`), `instructions`,
  `criteria` (noul: optional `true`/`false` descriptions; choice: map of
  option name to description, 1–255 entries; score: ordered array of level
  descriptions, 1–10 entries).
- **Response**: `model`, `answers` (map keyed by question IDs), `usage`
  (`input_tokens`, `output_tokens`).
- **Answer**: `type` matching its question; `noul` (number 0–1), `choice`
  (string) with `probabilities` and `confidence`, `score` (number) with
  `legend`, `probabilities` and `confidence`; experimental `box` with
  normalized coordinates.

## 3. Request Field Mapping (Jev → IR / IR → Jev)

| Jev field | IR destination | Notes |
|---|---|---|
| `model` | `Request.Model` | passed through the model map (spec/03) |
| `state` (string) | `Request.System[]` — one block | N-JEV-1 |
| `state` (object/array) | `Request.System[]` — one block, canonical compact JSON text | N-JEV-1, one `degraded` loss |
| `questions` (instructions/criteria) | `Messages[role=user].Content` text block, rendered per §7.2 | N-JEV-2 |
| `questions` (type projection) | `Params.ResponseFormat` (`json_schema`), synthesized per §7.3 | N-JEV-3 |
| fixed instruction template | appended to the system text, per §7.1 | N-JEV-2 |
| `assets` | — | N-JEV-4, `unsupported-semantic` |
| question `type: "box"` | — | N-JEV-5, `unsupported-semantic` per question |

An empty `questions` map, a missing `model`, or a missing `state` is a
structural error. `questions` entries with a `type` outside the three
supported values are skipped with N-JEV-6 and do not enter the synthesized
schema.

The synthesized `Params.ResponseFormat` carries `Type: "json_schema"`,
`Name: "jev_answers"`, no description, and no `Strict` value; its schema is
carried verbatim per INV-1 once synthesized. `jev.EncodeRequest` is the one
authorized read-only consumer of `Params.ResponseFormat.Schema`: INV-1
prohibits mutating or re-ordering the schema, not reading it to produce the
target face wire. The schema itself is never written into Jev wire.

### 3.1 IR → Jev request reconstruction

The reverse direction rebuilds a Jev request from an IR request:

| IR source | Jev destination | Notes |
|---|---|---|
| `Params.ResponseFormat` (`json_schema`) | `questions` map, per §7.4 | N-JEV-7 |
| system text (template-stripped) | `state` (string) | N-JEV-8 |
| `model` | `model` | through the model map |

An IR request without a `json_schema` response format, or with no system
text, is a structural error: Jev cannot express an evaluation without
questions or state. Reconstructed question `instructions` come from the
schema property `description`; reconstructed `criteria` descriptions are
not recoverable (they live in the prompt text), so choice criteria carry
the option name as its own description and score criteria carry empty
level descriptions, each choice/score question recording one N-JEV-9
`degraded` loss; noul criteria are optional on the wire and stay absent
without a loss. A user message is treated as the derived §7.2 rendering —
and skipped without a loss — only when every text line is a question
line, a criteria line, or blank, and its question (id, type) set equals
the reconstructed questions'; any other user message content, and all
assistant/tool message content, has no Jev destination and records one
N-JEV-10 `unsupported-semantic` loss per message. Sampling parameters,
tools, tool choice, metadata, native annotations, and positional tool
availability changes (`tool_change_groups`) record losses (N-JEV-10).

## 4. Response Field Mapping

| Jev field | IR destination | Notes |
|---|---|---|
| `model` | `Response.Model` | direct |
| `answers` | `Content[]` — one `TextBlock`, canonical compact JSON per §7.5 | N-JEV-11 |
| `answers[].type = "box"` | carried verbatim in the text | N-JEV-5, `unsupported-semantic` per answer |
| `usage.input_tokens` / `usage.output_tokens` | `Usage.InputTokens` / `Usage.OutputTokens` | direct |
| — | `StopReason = end_turn` | derived envelope, no loss |
| — | `Response.ID = "jev"` | synthesized envelope (the IR document schema requires a non-empty id); exempt |

The answers text preserves every present answer field verbatim
(`probabilities`, `confidence`, `legend` included): decode carries, encode
drops.

### 4.1 IR → Jev response parsing (encode)

The reverse direction parses model output back into typed answers:

- All `TextBlock`s concatenate in order; the result MUST parse as a
  non-empty JSON object of answer objects, else a structural error. An
  empty content list is a structural error.
- **Answer completeness.** With `WithQuestions` context installed, every
  supported-type question (noul/choice/score) MUST have an answer keyed
  by its ID; a missing key is a structural error. Box and unsupported-type
  questions are excluded from this contract. An answer keyed beyond the
  requested questions carries and records one `unmapped-field` loss.
- Answer objects are carried verbatim (`json.RawMessage` discipline,
  INV-1); the encoder never fabricates `probabilities`, `confidence`, or
  `legend` values that the upstream response did not contain.
- **Type recovery.** With `WithQuestions` context installed (the original
  request's questions), the question's type governs: scalar values are
  interpreted as that type (boolean/number → `noul` with 1/0 for booleans,
  string → `choice`, number → `score`), and a scalar that cannot express
  the question's type carries verbatim and records N-JEV-12
  (`unmapped-value`). An object answer whose carried type contradicts the
  matching question also carries verbatim and records N-JEV-12; a matching
  or untyped answer takes the question's type. Without context, a typed
  object passes through; an untyped object's type comes from its value
  field (`noul`/`choice`/`score`). Scalar values follow the output
  contract directly: boolean → `noul` (1/0), string → `choice`, and a
  bare number infers `score` with one N-JEV-13 `degraded` loss (noul/score
  are ambiguous).
- **Range validation.** With context: a `choice` value outside its
  criteria options, a `score` value outside its discrete levels (integers
  1..N), or a `noul` outside [0,1] records N-JEV-14 (`degraded`) and keeps
  the carried value — the wire may be non-conforming but the data is not
  rewritten or dropped.
- Non-text blocks (`tool_use`, `thinking`, …) record N-JEV-15
  (`unsupported-semantic`) and are excluded from the answers object.
- An experimental `box` answer carries verbatim and records N-JEV-5
  (`unsupported-semantic`), symmetric with decode.
- A scalar answer value (boolean, string, or number) follows the output
  contract directly: boolean → `noul` (1/0), string → `choice`, number →
  `score`. An object answer without a recognized value field is a
  structural error.
- `StopReason` `end_turn` and `other` are silent; every other stop reason
  records N-JEV-16 (`unmapped-field`). Usage granularity details record
  N-JEV-17 (`unmapped-field`).

## 5. Envelope Rendering Defaults (IR → Jev)

- `model`: direct from `Response.Model` / `Request.Model`.
- `id`: Jev responses carry no identifier; `Response.ID` is dropped
  (envelope, exempt).
- Usage: `input_tokens` / `output_tokens` direct; all other usage fields
  have no Jev equivalent (N-JEV-17).

## 6. Streaming

Jev is a request/response protocol with no streaming mode. This face
implements no streaming converters, produces no IR events, and spec/20
does not apply. Vector coverage is `nonstream` only.

## 7. Normalization Rules

### 7.1 Instruction template (fixed text)

The system text is the state text, then two newlines, then exactly this
template:

```
Answer the structured questions that follow as a single JSON object. Keys MUST be exactly the question IDs given in the user message; values MUST match each question's type: noul answers are numbers in [0,1], choice answers are one of the listed option names, score answers are one of the listed level numbers. Output raw JSON only: no prose, no markdown fences.
```

### 7.2 User text rendering

Question entries render in ascending lexicographic ID order, one block per
question, joined by a single newline:

```
question <id> (<type>): <instructions>
```

When the question carries criteria, the next lines are:

```
criteria:
- <name>: <description>
```

choice uses option names in ascending lexicographic order; score uses
`level <n>` names in ascending numeric order (n = 1-based index). An entry
with an empty or absent description renders as `- <name>` (no colon). noul
true/false descriptions render under the names `true`/`false` when present;
noul without criteria renders no criteria lines.

### 7.3 Synthesized schema (Jev → IR)

The schema is a JSON object:

```json
{"type": "object", "properties": {"<id>": <property>}, "required": ["<id>", ...], "additionalProperties": false}
```

`properties` keys are the supported question IDs; `required` lists the same
IDs in ascending lexicographic order. Per question type:

- `noul` → `{"type": "number", "minimum": 0, "maximum": 1, "description": "<instructions>"}`
- `choice` → `{"type": "string", "enum": [<option names, ascending lexicographic>], "description": "<instructions>"}`
- `score` → `{"type": "number", "enum": [1, 2, ..., <level count>], "description": "<instructions>"}`

Property keys are rendered in ascending lexicographic ID order. The
`description` carries only the question `instructions`; criteria
descriptions never enter the schema (they stay in the prompt text, §7.2).

### 7.4 Schema reconstruction (IR → Jev)

Each `properties` entry reconstructs by exact shape: `{type: number,
minimum: 0, maximum: 1}` → `noul`; `{type: string, enum: [strings]}` →
`choice`; `{type: number, enum: [1..N]}` → `score`. Any other property
shape records N-JEV-6 (`unsupported-semantic`) and is skipped. A schema
with no reconstructable properties is a structural error. `required` and
`additionalProperties` are not consulted.

### 7.5 Answers JSON canon (decode)

Decode marshals the `answers` map to one canonical JSON object: top-level
answer keys sorted ascending; each answer object is re-serialized with its
recognized fields (`type`, `noul`, `choice`, `score`, `legend`,
`probabilities`, `confidence`, `box`) in ascending key order with their
values carried verbatim (number forms preserved). Unrecognized properties
on an answer are removed — never passed through — and each reports one
`unmapped-field` loss (spec/02 §4). The text block holds exactly this JSON:
no fences, no indentation. Implementations in every language MUST
reproduce this canon; answer values are opaque payloads under INV-1
discipline.

### 7.6 State JSON canon (decode)

Non-string `state` is serialized to canonical compact JSON: keys sorted
ascending, no whitespace, integer leaves kept integral.

## 8. Loss Catalog

Buckets follow [spec/02 §9](02-loss-policy.md#9-loss-conventions--derived-and-envelope-fields).

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| `state` non-string value | degraded | request → IR | N-JEV-1: state serialized to canonical JSON text |
| `assets` | unsupported-semantic | request → IR | N-JEV-4: experimental image assets are out of scope |
| `questions.<id>` type `box` | unsupported-semantic | request → IR | N-JEV-5: experimental box questions are out of scope |
| `questions.<id>` unknown type | unsupported-semantic | request → IR | N-JEV-6: skipped from schema and prompt |
| reconstructed `criteria` descriptions | degraded | IR → request | N-JEV-9: criteria descriptions live in the prompt text |
| IR params (temperature/top_p/max_tokens/stop/effort/verbosity) | unmapped-field | IR → request | N-JEV-10: one loss per present field |
| IR tools / tool_choice / metadata / native annotations | unmapped-field | IR → request | N-JEV-10: no Jev equivalent |
| IR positional tool changes (`tool_change_groups`) | unsupported-semantic | IR → request | N-JEV-10: one loss when present; positional availability has no Jev equivalent |
| non-derived user, assistant, and tool message content | unsupported-semantic | IR → request | N-JEV-10: not the derived question rendering; Jev has no conversation turns |
| answers beyond the requested questions | unmapped-field | IR → response | carries; one loss per extra answer key |
| `answers.<id>` type `box` | unsupported-semantic | response → IR | N-JEV-5: carried verbatim in the answers text |
| `answers.<id>` type mismatch vs context | unmapped-value | IR → response | N-JEV-12 |
| numeric answer type without context | degraded | IR → response | N-JEV-13: noul/score are ambiguous |
| choice/score/noul value outside criteria | degraded | IR → response | N-JEV-14: value kept, not rewritten |
| non-text response blocks | unsupported-semantic | IR → response | N-JEV-15 |
| stop reason other than end_turn/other | unmapped-field | IR → response | N-JEV-16: Jev responses carry no stop reason |
| usage granularity details | unmapped-field | IR → response | N-JEV-17 |
| `probabilities` / `confidence` / `legend` on encode | exempt (absent) | IR → response | never fabricated; absent unless the upstream carried them |
| unknown non-exempt property on a recognized Jev object | unmapped-field | request/response → IR | one loss per present property (spec/02 §4) |

Structural errors (never losses): missing `model`, missing `state`, empty
`questions`, criteria of the wrong shape for its question type, response
text that is not a JSON object, empty response content, a schema with no
reconstructable properties, an IR request without a `json_schema` response
format or system text on encode.

## 9. References

- [00 — Scope and Architecture](00-scope-and-architecture.md)
- [01 — Intermediate Representation](01-intermediate-representation.md)
- [02 — Loss Policy](02-loss-policy.md)
- [03 — Model Handling](03-model-handling.md)
- [vectors/README](../vectors/README.md)
- Jev HTTP API reference: <https://jevwiki.ai/wiki/reference/http-api.md>
- System One overview: <https://system-one.dev/en/api>
