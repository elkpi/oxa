# 02 — Loss Policy

Status: normative. Spec version 0.1.0.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119 and RFC 8174 when,
and only when, they appear in all capitals, as shown here.

## 1. Why losses exist

The three faces do not express the same things. A conversion input can be
perfectly valid yet contain fields, values, or whole constructs that the
target dialect cannot represent. oxa's contract is: the conversion always
completes, and every fidelity cost is reported as a **loss record**.
Losses are first-class output, never silently dropped, and never turned
into failures (§4).

## 2. The Loss record

Defined structurally by [`spec/schema/loss.schema.json`](schema/loss.schema.json).

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Path` | `path` | string | yes | dot/bracket path into the source payload; empty string denotes the payload root |
| `Field` | `field` | string | yes, non-empty | leaf field name (or value identifier) at `path` |
| `Reason` | `reason` | enum, §3 | yes | every loss MUST carry a reason code |
| `Detail` | `detail` | string | no | human-readable context; SHOULD name what was dropped or distorted |

Example: an Anthropic cache-control annotation that Chat Completions cannot
carry is reported as

```json
{
  "path": "messages[2].content[0].cache_control",
  "field": "cache_control",
  "reason": "unmapped-field",
  "detail": "no equivalent field in Chat Completions"
}
```

`path` is root-relative: object keys are joined by `.` (no leading dot),
array elements are addressed by zero-based index in brackets, as in
`messages[2].content[0].cache_control`. Losses MUST be reported in the
order the corresponding source constructs are encountered while decoding
the source document, so that loss lists are deterministic and comparable
in vectors.

## 3. Reason codes

| Code | Meaning |
|------|---------|
| `unmapped-field` | the source field has no representation in the target dialect; the field is dropped |
| `unmapped-value` | the field exists on both sides, but this specific value has no mapping (e.g. a stop reason native to one face, or an image media type the target cannot carry) |
| `unsupported-semantic` | a whole construct or combination the target dialect cannot express (e.g. a tool-choice mode the target lacks) |
| `degraded` | best-effort carry with known distortion: the data still flows to the output, distorted; reserved for this case and MUST NOT be used for drops |

## 4. Errors vs losses

An **error** means the conversion cannot proceed: the input is structurally
unparseable. Errors are allowed ONLY for:

1. **malformed JSON** — the payload is not valid JSON;
2. **type violations** — the JSON parses, but a known field violates the
   face's structural expectations (e.g. `messages` is not an array, `role`
   is not a string; the per-face expectations are pinned by documents
   10–12);
3. **broken stream invariants** — an inbound event stream violates INV-5
   or INV-6 of document [01](01-intermediate-representation.md).

Everything semantically unmappable is a loss, never an error. A conversion
that completes MUST produce a result plus a loss list; a conversion that
errors MUST NOT produce a partial result.

**Unrecognized fields.** The library MUST NOT fail on unrecognized fields,
and MUST NOT pass them through: the target APIs reject unknown fields
(both OpenAI and Anthropic error on unrecognized parameters), so
passthrough is impossible — an unrecognized source field would simply move
the failure downstream. Unrecognized fields are dropped and recorded with
reason `unmapped-field`. The type-violation rule above applies only to
known fields; the value of an unrecognized field is ignored entirely.

## 5. Streaming

During a streaming conversion, losses accumulate as they are discovered.
They are returned via the converter's `Losses()` accessor (Go naming;
every implementation exposes an equivalent) after the stream ends.

Loss events MUST NOT be injected into the IR event stream. Rationale: the
IR event stream is a pure, face-neutral sequence (INV-5, INV-8); keeping
losses out of it lets all three-language implementations behave uniformly
and keeps the vector format simple — an expected event sequence in a
vector contains only IR events, and expected losses live in a separate
field of the vector.

## 6. Rules

- Every loss MUST have a reason code from the §3 enum (schema-enforced).
- `degraded` is reserved for best-effort carry with known distortion; a
  drop is never `degraded`.
- Worked example: during streaming tool aggregation, the concatenated
  argument string of a tool_use block fails JSON validation. The string is
  still forwarded as-is in the aggregated `ToolUseBlock.input` (INV-1
  forbids touching it), and a loss with reason `degraded` is recorded,
  with a `detail` naming the block and the validation failure.
- Losses never alter the converted result beyond what their reason code
  describes; in particular they are never visible in the IR event stream
  (§5).

## 7. Schema agreement

The JSON shape of the loss record is defined by
[`spec/schema/loss.schema.json`](schema/loss.schema.json). This document
and the schema MUST agree; CI checks the agreement from milestone M2
onward.
