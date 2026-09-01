# 12 — Mapping: Anthropic Messages

This document is the normative mapping between the Anthropic Messages
(AN) face and the oxa intermediate representation (spec/01). It covers
the non-streaming request and response conversion implemented by the Go
spoke `go/anthropic/messages`. Streaming is out of scope here (see §6).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" are to be
interpreted as described in RFC 2119 and RFC 8174 (see
[README.md](README.md)).

## 1. Overview

The Messages face is Anthropic's `POST /v1/messages` protocol: a
required top-level `max_tokens`, an optional `system` (string or block
array), and a `messages` array whose content is a string or a block
array (`text`, `image`, `tool_use`, `tool_result`). Conversion is
near-identity for the supported subset; Anthropic-only annotations
(`cache_control`, `disable_parallel_tool_use`, `metadata.user_id`) are
dropped with losses rather than errors (spec/02). Errors are reserved
for structural violations: a missing or non-positive `max_tokens`,
unknown `role` values, missing or malformed content, missing
`tool_use` id/name/input, missing `tool_result.tool_use_id`, a
`tool_use.input`/`input_schema` that is not a JSON object, a missing
`stop_reason`, or a request with no messages.

## 2. Wire Objects

Reference snapshot (retrieved 2026-08-28):

- API reference: <https://docs.claude.com/en/api/messages>
- Documentation ("Set max_tokens", which recommends 4096 and uses it in
  the docs' own examples): <https://docs.claude.com/en/docs>

The wire objects in scope are:

- **Request**: `model`, `system`, `messages[]`, `max_tokens`
  (required), `temperature`, `top_p`, `stop_sequences`, `metadata`,
  `tools[]`, `tool_choice`.
- **Message**: `role` (`user` | `assistant`), `content` (string or
  block array).
- **Block**: `text` (`text`), `image` (`source`), `tool_use`
  (`id`, `name`, `input`), `tool_result` (`tool_use_id`, `content`,
  `is_error`); every block may carry a `cache_control` annotation.
- **Source**: `base64` (`media_type`, `data`) or `url` (`url`).
- **Tool**: `name`, `description`, `input_schema` (JSON Schema).
- **ToolChoice**: `type` (`auto` | `any` | `tool`), `name`,
  `disable_parallel_tool_use`.
- **Response**: `id`, `type`, `role`, `model`, `content[]` (blocks),
  `stop_reason`, `stop_sequence`, `usage` (`input_tokens`,
  `output_tokens`).

## 3. Request Field Mapping (AN → IR / IR → AN)

| AN field | IR destination | Notes |
|---|---|---|
| `model` | `Request.Model` | via model map (spec/03) |
| `system` (string) | one `SystemBlock` | N-AN-1 |
| `system[]` (block array, `type: "text"`) | `SystemBlock[]` in order | N-AN-1; `cache_control` lost |
| `messages[].content` | `Message.Content` blocks (or one `TextBlock` for string content) | N-AN-5 |
| `max_tokens` | `Params.MaxTokens` | required and MUST be positive on decode; defaulted on encode (N-AN-2) |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `stop_sequences` | `Params.StopSequences` | 1:1 (N-AN-7) |
| `tools[].{name,description,input_schema}` | `Tools[].{Name,Description,InputSchema}` | schema bytes carried verbatim (INV-1); MUST be a JSON object |
| `tool_choice` | `ToolChoice` (modes `auto`/`any`/`none`/`tool` map 1:1 by name) | N-AN-6 |
| `metadata` | — | single unmapped-field loss, both directions (N-AN-8) |
| `tool_choice.disable_parallel_tool_use` | — | unmapped-field loss |
| `cache_control` (any block, system block) | — | unmapped-field loss |

On encode (IR → AN), `Request.System` renders as a system block array
of `type: "text"` blocks; message content renders as block arrays,
with the single exception of the exact string shorthand (N-AN-3);
`max_tokens` is always emitted, defaulting to 4096 with a `degraded`
loss when the IR carries no `Params.MaxTokens` (N-AN-2); other params
render 1:1 with their omitempty semantics.

## 4. Content Block Mapping

| AN block | IR block | Notes |
|---|---|---|
| `{type: "text", text}` | `TextBlock` | |
| `{type: "image", source: {type: "base64", media_type, data}}` | `ImageBlock{MediaType, Data}` | |
| `{type: "image", source: {type: "url", url}}` | `ImageBlock{URL}` | |
| `{type: "tool_use", id, name, input}` | `ToolUseBlock{ID, Name, Input}` | raw input, N-AN-4 |
| `{type: "tool_result", tool_use_id, content, is_error}` | `ToolResultBlock{ToolUseID, Content, IsError}` | content is string or block array, decoded recursively by N-AN-5 |
| other block types | — | one whole-block unsupported-semantic loss (N-AN-9) |
| `source.type` ∉ {`base64`, `url`} | — | unsupported-semantic loss |

**Raw tool input fidelity (N-AN-4).** `tool_use.input` is an opaque
JSON object on the wire and a raw JSON string token in the IR
(spec/01 INV-1). The decoder MUST convert the exact source bytes of
the object into the string payload and MUST NOT parse, validate
beyond object-ness, or re-serialize the object; the encoder MUST
unwrap the string token back to those exact bytes. Payload bytes MUST
survive a round trip byte-identically (key order, whitespace,
numeric spelling included). This byte-fidelity guarantee is normative
for documents obtained by JSON-decoding the wire payload into the
implementation's typed content representations — the normal decode
path. For callers that instead supply generic (untyped, in-memory)
representations such as `map[string]any`, which carry no source
bytes, the decoder MUST produce the implementation's canonical
encoding of the supplied value (compact bytes; no key-order or
spelling preservation is required), and this canonicalization is NOT
a loss and MUST NOT be recorded as one.

On encode, image blocks MUST carry exactly one of data or URL; data
requires `media_type`, and a URL image MUST NOT carry `media_type`.
Violations are unsupported-semantic losses. Tool-result content
renders as an array of `text`/`image` blocks; other IR block types
inside a tool result are unsupported-semantic losses.

## 5. Response Field Mapping

| AN field | IR destination | Notes |
|---|---|---|
| `content[]` blocks | `Response.Content` blocks | N-AN-5, same block table as §4 |
| `stop_reason` | `Response.StopReason` | `end_turn`→`end_turn`, `max_tokens`→`max_tokens`, `stop_sequence`→`stop_sequence`, `tool_use`→`tool_use`, `refusal`→`refusal`; unknown → `other` + `unmapped-value` loss; missing → error |
| `stop_sequence` | `Response.StopSequence` | carried verbatim; emitted on encode when the stop reason is `stop_sequence` |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.input_tokens` / `usage.output_tokens` | `Usage.InputTokens` / `Usage.OutputTokens` | 1:1 |
| `type` (`"message"`), `role` (`"assistant"`) | — | ENVELOPE, exempt; rendered as fixed defaults on encode with no loss |

## 6. Streaming Event Mapping

Deferred to [spec/20](20-streaming-semantics.md) (streaming semantics). This document
MUST NOT be cited for streaming behavior.

## 7. Normalization Rules

Each rule has a stable ID usable as a vector tag.

- **N-AN-1 (system).** A string `system` converts to one
  `SystemBlock`; a block-array `system` converts element-wise, in
  order, and every element MUST be `type: "text"` (other types are
  structural errors). Each block's `cache_control` is an unmapped-field
  loss. On encode, IR system blocks render as the block-array form
  (never the string shorthand).
- **N-AN-2 (max_tokens default).** Anthropic Messages requires
  `max_tokens`. On decode the field MUST be present and positive
  (otherwise a structural error) and maps to `Params.MaxTokens`. On
  encode, a missing `Params.MaxTokens` MUST be filled with the default
  4096 — the value the Anthropic docs use as the recommended default
  maximum token count — and MUST record a `degraded` loss naming the
  applied default (spec/03 §3).
- **N-AN-3 (string shorthand exactness).** The encoder MUST emit
  message `content` as the string shorthand if and only if the request
  carries no system blocks, exactly one message, and that message's
  content is exactly one `TextBlock`; the string is that block's text.
  Every other request emits block arrays. The decoder accepts string
  or block-array content everywhere.
- **N-AN-4 (raw tool input).** As specified in §4: byte-exact
  conversion between the wire JSON object and the IR raw JSON string
  token on typed (JSON-decoded) paths; never parse, never reformat.
  Generic in-memory inputs convert with the implementation's
  canonicalized bytes; this is not a loss.
- **N-AN-5 (block mapping).** Content blocks map per the table in §4,
  recursively for `tool_result.content`. Unknown block types and
  unknown image source types are dropped as unsupported-semantic
  losses (N-AN-9).
- **N-AN-6 (tool choice).** `tool_choice.type` `auto`, `any`, `none`
  map to the eponymous IR modes; `type: "tool"` maps to mode `tool`
  with `name` (a missing name is a structural error); unknown types
  are unsupported-semantic losses. `disable_parallel_tool_use: true`
  is an unmapped-field loss. The mapping inverts on encode; IR mode
  `tool` with an empty name is a structural error.
- **N-AN-7 (stop sequences).** `stop_sequences` maps 1:1 to
  `Params.StopSequences` in both directions; response
  `stop_reason: "stop_sequence"` carries the matched sequence in
  `stop_sequence`, preserved in the IR and re-emitted on encode.
- **N-AN-8 (metadata).** Anthropic request `metadata` is the specific
  `{user_id}` semantic, not an arbitrary string map, so it has no IR
  destination; conversely the IR metadata map has no Messages field.
  Presence of wire `metadata` on decode, or a non-empty IR metadata
  map on encode, records exactly one unmapped-field loss per
  direction.
- **N-AN-9 (unknown semantics).** A block or image source whose type
  has no IR equivalent is dropped as one whole-block
  unsupported-semantic loss. When such an unknown block also carries
  `cache_control`, the annotation is part of the already-dropped block
  and MUST NOT add a second loss: the whole block is one
  unsupported-semantic loss, and no separate annotation loss is
  recorded. (Known block types that survive decoding still record
  their own `cache_control` unmapped-field loss per N-AN-1/N-AN-5.)
- **N-AN-10 (envelope exemptions).** Response `type` and `role` are
  envelope fields: dropped on decode and rendered as
  `type: "message"` / `role: "assistant"` on encode, with no loss
  either way.

## 8. Loss Catalog

Buckets follow [vectors/README.md](../vectors/README.md): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| response `type`, `role` | exempt (envelope) | both | N-AN-10 |
| `cache_control` on a mapped block or system block | unmapped-field | request/response → IR | Anthropic prompt-caching annotations have no IR equivalent in v1 |
| unknown block type or unknown image source type (with any annotations it carries) | unsupported-semantic | both | one whole-block loss, N-AN-9 |
| `metadata` | unmapped-field | request, both directions (single loss each way) | N-AN-8 |
| `tool_choice.disable_parallel_tool_use` | unmapped-field | request → IR | no IR equivalent in v1 |
| `max_tokens` default on encode | degraded | IR → request | N-AN-2: default 4096 applied when IR lacks `Params.MaxTokens` |
| IR image with both/neither of data and URL, data without `media_type`, URL with `media_type` | unsupported-semantic | IR → request | §4 |
| IR block type with no Messages equivalent in its position | unsupported-semantic | IR → request | N-AN-5 |
| IR tool-choice mode with no Messages equivalent | unsupported-semantic | IR → request | N-AN-6 |
| unknown `stop_reason` value | unmapped-value | response → IR | maps to IR `other` |
| missing/non-positive `max_tokens`, missing `stop_reason`, malformed blocks | structural error | as noted | not losses; conversion fails |

## 9. References

- [00 — Scope and Architecture](00-scope-and-architecture.md)
- [01 — Intermediate Representation](01-intermediate-representation.md)
- [02 — Loss Policy](02-loss-policy.md)
- [03 — Model Handling](03-model-handling.md)
- [vectors/README.md](../vectors/README.md) — loss buckets and
  from-ir rendering defaults
- Anthropic Messages API reference,
  <https://docs.claude.com/en/api/messages> (retrieved 2026-08-28)
