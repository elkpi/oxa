# oxa golden vectors

Golden vectors are the **behavioral source of truth** for oxa conversion. Per
the precedence rules in [spec/README.md](../spec/README.md): when artifacts
disagree, `vectors/` wins on behavior, `spec/schema/*.json` wins on structure,
and `spec/*.md` carries semantics and rationale. A vector contradicting a
schema is a schema bug; a converter disagreeing with a vector is a converter
bug.

Every file in `vectors/` except `manifest.json` is one vector and validates
against [spec/schema/vector.schema.json](../spec/schema/vector.schema.json).

## Layout and naming

Vectors live at `vectors/<face>/<mode>/<area>-<case>.json`, where:

- `<face>` is one of `chatcompletions`, `responses`, `anthropic`, or
  `cross`
- `<mode>` is `nonstream` or `stream`
- `<area>-<case>` is a short kebab-case slug, e.g. `minimal-text-to-ir`

The vector's `name` field equals the file path relative to `vectors/` with
slashes replaced by dots and the `.json` suffix removed:

```
vectors/anthropic/nonstream/multiturn-to-ir.json
  -> "anthropic.nonstream.multiturn-to-ir"
```

veccheck enforces `name == <dotted relative path>` and global name
uniqueness.

## Comparison rules (normative)

These rules are the single source of truth for how expected and actual
outputs are compared. Future language harnesses (Rust, Python, C++) MUST copy
them verbatim:

1. **Structural JSON equality.** Object key order is irrelevant; arrays are
   ordered and compare element-wise; string values compare by exact
   code-point sequence.
2. **Integers stay integers.** Numeric leaves compare numerically but with
   type fidelity: expected `1` does not match actual `1.0`. A converter that
   coerces an integer to a floating-point form fails vector comparison even
   though its output remains schema-valid (spec/01 INV-7).
3. **Raw-JSON strings compare as strings.** `tool_use.input` and
   `input_json_delta.partial_json` are opaque JSON *text* (spec/01 INV-1).
   They compare by exact string equality; the JSON inside them is never
   structurally compared. The same applies to face-side tool-argument strings
   (Chat Completions `tool_calls[].function.arguments`, Responses
   `arguments`, Anthropic `tool_use.input`).
4. **Losses compare as unordered sets.** `expected_losses` and the reported
   losses match as sets keyed on `(path, field, reason)`; `detail` is
   informational and not compared. Every expected loss must be reported and
   every reported loss must be expected.

## Loss conventions

Not every field absent from a conversion's output is a loss. Dropped fields
fall into three buckets (this section will later migrate into the per-face
loss catalogs in spec/10–12):

1. **DERIVED fields — exempt.** Fields recomputable from carried data carry
   no information loss and MUST NOT record a loss. Examples: Chat
   Completions and Responses `usage.total_tokens` (= prompt + completion /
   input + output, recomputed on encode).
2. **ENVELOPE fields — exempt.** Per-face structural/transport fields with
   no conversational semantics. Examples: Chat Completions `object`,
   `created`, `choices[].index`, `message.role`, and response `id` when
   regenerated on encode; Responses `object`, `status`, output-item `id`
   and `status`, and empty `annotations`; Anthropic `type` and `role`.
3. **Everything else — MUST record a loss.** Any non-exempt input field with
   no IR destination is dropped with an `unmapped-field` loss record
   (path, field, reason), exactly as `loss-logprobs-request-to-ir`,
   `loss-cache-control-request-to-ir`, and `loss-verbosity-request-to-ir`
   demonstrate.

### From-ir rendering defaults

from-ir vectors show the converter's **documented envelope rendering
defaults**, not round-trip guarantees. When an IR document lacks a face
envelope field, the converter synthesizes a fixed value and NO loss is
recorded (the direction is IR → face; nothing is being dropped). Examples in
the seed set: Chat Completions `object: "chat.completion"` and `created: 0`;
Responses `status: "completed"`, `object: "response"`, synthesized output
item ids (`msg_abc123` for message items, `fc_abc123` for function_call
items), output-item `status: "completed"` and `role: "assistant"`,
`annotations: []` on every output_text part, `usage.total_tokens` recomputed
as input + output, and the request-side input string shorthand for a
single-text user turn (spec/11 N-R-2); Anthropic `type: "message"` and
`role: "assistant"`.

## Stream self-consistency assertions

For `stream` vectors, the harness validates the following assertions in
order:

- the vector document and its `expected_ir` / `expected_output` match
  their respective JSON schemas;
- the event sequence obeys the INV-5 grammar and INV-6 contiguous-index
  discipline (spec/01 §7);
- every tool-use block's `ToolUseBlock.input` equals the exact
  concatenation of its `InputJSONDelta.partial_json` fragments in
  encounter order;
- text and tool-argument fragments remain in their received order;
- native events compare as ordered arrays in `input.events` and
  `expected_output.events`; and
- losses compare as unordered sets keyed on `(path, field, reason)`.

Tool argument payloads compare as exact opaque JSON strings (spec/01
INV-1); neither the vector format nor the checker re-parses or
re-serializes them. No assertion compares stream text fragments against
a generated non-stream aggregate, because a stream vector carries the
event sequence directly rather than delegating to an aggregated response.

## Cross vectors

`vectors/cross/nonstream/` holds `protocol-to-protocol` vectors: a source
wire document is decoded to the IR and re-encoded to a target face in one
composition (`source.Decode → IR → target.Encode`). Each vector names its
`source` and `target` protocols; `input` is the source wire document and
`expected_output` is the target wire document. `expected_ir` is forbidden
by the vector schema: the intermediate IR is not part of the cross
contract — the per-face vectors already lock each stage separately.

`expected_losses` is the concatenation of the source-decode losses
followed by the target-encode losses (spec/02 §6); it still compares as
an unordered set keyed on `(path, field, reason)`. Cross vectors are
nonstream only.

## manifest.json

`vectors/manifest.json` is generated, never hand-edited:

    cd go && go run ./cmd/veccheck -root .. -write-manifest

It lists every vector (`name`, `file`, `tags`, `sha256`) sorted by name, with
no timestamps, so it is byte-stable across runs. It is committed. CI verifies
it with:

    cd go && go run ./cmd/veccheck -root .. -check-manifest

which recomputes the manifest and fails on any drift (new, removed, modified,
or renamed vectors).

## How implementations locate vectors

Converters' vector-driven tests walk up from the implementation directory
(parent by parent) looking for a directory that contains both `vectors/` and
`.git/`; that directory is the repo root. If none is found before the
filesystem root, the tests skip (the module is being consumed as a dependency
outside the monorepo). Never hardcode an absolute path.
