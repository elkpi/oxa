# 11 — Mapping: OpenAI Responses

This document is the normative mapping between the OpenAI Responses (RE)
face and the oxa intermediate representation (spec/01). It covers the
non-streaming request and response conversion implemented by the Go spoke
`go/openai/responses`. Streaming is out of scope here (see §6).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" are to be
interpreted as described in RFC 2119 and RFC 8174 (see
[README.md](README.md)).

## 1. Overview

The RE face is OpenAI's `POST /v1/responses` protocol: an `input` that is
either a plain string (the string shorthand) or an array of self-delimited
input items (message items, `function_call` items, `function_call_output`
items), top-level `instructions`, flat function tool definitions, and a
response object whose `output` array carries message, `function_call`, and
`reasoning` items. Conversion to the IR is total for the supported subset
(text, system, multi-turn, parameters, tools, images); every input the
converter cannot carry semantically is dropped with a loss record rather
than an error (spec/02). Errors are reserved for structural type violations
of known fields: an `input` that is neither string nor item array, unknown
message-item `role` values, a non-string content leaf where a string is
required, a request with no conversation input, an unknown response
`status`, or tool input that is not a JSON string on encode.

## 2. Wire Objects

Reference snapshot (retrieved 2026-08-28):

- API reference: <https://platform.openai.com/docs/api-reference/responses>
- Guide: <https://platform.openai.com/docs/guides/responses-vs-chat-completions>

The wire objects in scope are:

- **Request**: `model`, `input` (string or item array), `instructions`,
  `temperature`, `top_p`, `max_output_tokens`, `tools[]`, `tool_choice`,
  `metadata`, and the unsupported `text` (`verbosity`, `format`),
  `reasoning`, `parallel_tool_calls`. Responses has NO stop-sequences
  parameter.
- **InputItem**: a message item (`type` absent or `"message"`, `role`
  `system` | `user` | `assistant`, `content` string or parts array), a
  `function_call` item (`call_id`, `name`, `arguments`), a
  `function_call_output` item (`call_id`, `output`), or an unknown type.
- **ContentPart**: `type` (`input_text` | `output_text` | `input_image`),
  `text`, `image_url` (a plain https or `data:` URL string, unlike the
  Chat Completions object form).
- **Tool**: flat `type: "function"` with `name`, `description`,
  `parameters` (JSON Schema), `strict`.
- **Response**: `id`, `object`, `status` (`completed` | `incomplete` |
  `failed`), `model`, `output[]`, `usage` (`input_tokens`,
  `output_tokens`, `total_tokens`), `incomplete_details.reason`, `error`.
- **OutputItem**: `type` (`message` | `function_call` | `reasoning`),
  `id`, `status`, `role`, `content[]` (`output_text` parts with
  `annotations`), `call_id`, `name`, `arguments`.

## 3. Request Field Mapping (RE → IR / IR → RE)

| RE field | IR destination | Notes |
|---|---|---|
| `model` | `Request.Model` | passed through the model map (spec/03) |
| `instructions` | `Request.System[]` (first block) | N-R-1 |
| `input` (string) | one user message, one text block | N-R-2 |
| `input[role=system]` | `Request.System[]`, after instructions, in wire order | N-R-1 |
| `input[role=user/assistant].content` | `Messages[].Content` blocks | N-R-3 |
| `input[type=function_call]` (consecutive run, incl. adjacent assistant message items) | one assistant message; `ToolUseBlock`s after any text blocks | N-R-5 |
| `input[type=function_call_output]` (consecutive run) | one user message of `ToolResultBlock`s | N-R-6 |
| `tools[].{name,description,parameters}` | `Tools[].{Name,Description,InputSchema}` | flat form; schema bytes carried verbatim (INV-1) |
| `tool_choice` | `ToolChoice` (`required` ⇄ mode `any`; `{type:"function",name}` ⇄ mode `tool`) | N-R-9 |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `max_output_tokens` | `Params.MaxTokens` | name mapping only; loss-free both directions (N-R-7) |
| `image_url` (part field, https or `data:image/...;base64,...`) | `ImageBlock.URL` or `ImageBlock.{MediaType,Data}` | N-R-4 |
| `metadata` | — | unmapped-field loss, both directions as a single loss each way |
| `text.verbosity`, `text.format` | — | unmapped-field loss |
| `reasoning` | — | unmapped-field loss |
| `parallel_tool_calls` | — | unmapped-field loss |
| `tools[].strict` | — | unmapped-field loss per tool |
| unknown `input[].type` | — | unsupported-semantic loss per item |
| `Params.StopSequences` (IR side) | — | Responses has no stop parameter; presence records exactly one unmapped-field loss on encode (N-R-7) |

On encode (IR → RE), `Request.System` is rendered as the `instructions`
string equal to the concatenation of all system block texts (N-R-1). The
input string shorthand is rendered exactly when there is no system content
and the conversation is one user message whose content is exactly one text
block; every other conversation renders as an item array (N-R-2).
User message-item content renders as a plain string when it is exactly one
text block and as a parts array otherwise; assistant message-item content
always renders as a string.

## 4. Response Field Mapping

| RE field | IR destination | Notes |
|---|---|---|
| `output[type=message].content[type=output_text].text` | `Response.Content` text blocks | N-R-3 |
| `output[type=function_call]` | `ToolUseBlock`s after the text blocks | N-R-5 |
| `output[type=reasoning]` | — | unsupported-semantic loss per item |
| `status` + `incomplete_details.reason` + `error` | `Response.StopReason` | N-R-11 |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.input_tokens` | `Usage.InputTokens` | |
| `usage.output_tokens` | `Usage.OutputTokens` | |
| `usage.total_tokens` | — | DERIVED, exempt (recomputed on encode) |
| `usage` absent | zero usage | ENVELOPE-exempt, no loss |
| `object`, `output[].id`, `output[].status`, `output[].role` | — | ENVELOPE, exempt |
| empty `annotations` | — | ENVELOPE, exempt; non-empty `annotations` record an unmapped-field loss |
| output content `type` ≠ `output_text` | — | unsupported-semantic loss |

`status` mapping (N-R-11): `error` present or `status` `"failed"` → IR
`other` plus an unsupported-semantic loss naming the error; `completed` →
`tool_use` when any function_call item was converted, else `end_turn`;
`incomplete` with `incomplete_details.reason` `max_output_tokens` →
`max_tokens` loss-free; `incomplete` with any other reason → `other` plus
an unmapped-value loss; any other status is a structural error. On encode
the inverse mapping is used: `end_turn` and `tool_use` render
`status: "completed"`, `max_tokens` renders `status: "incomplete"` with
`incomplete_details.reason` `"max_output_tokens"` (loss-free), `refusal`
renders `status: "failed"` with `error {code: "refusal"}`, and
`stop_sequence` renders `status: "completed"` plus an unmapped-value loss
(Responses carries no stop-sequence identity); an IR stop reason outside
the five known values is a structural error.

## 5. Envelope rendering defaults (IR → RE)

When the IR lacks RE envelope fields, the encoder synthesizes fixed
values and records no loss (from-ir rendering defaults,
[vectors/README.md](../vectors/README.md)): `object: "response"`,
`status: "completed"`, output-item ids (`msg_abc123` for message items,
`fc_abc123` for function_call items), output-item
`status: "completed"` and `role: "assistant"`, `annotations: []` on
every output_text part, and `usage.total_tokens` recomputed as input +
output.

## 6. Streaming Event Mapping

Deferred to [spec/20](20-streaming-semantics.md) (streaming semantics). This document
MUST NOT be cited for streaming behavior.

## 7. Normalization Rules

Each rule has a stable ID usable as a vector tag.

- **N-R-1 (system extraction and ordering).** `instructions` converts to
  the first IR system block. Every message item with `role: "system"`,
  in wire order, appends further system blocks after it (text content
  only; non-text parts are unsupported-semantic losses). On encode, all
  IR system blocks MUST be concatenated, in order, into the
  `instructions` string.
- **N-R-2 (input string shorthand).** A string `input` converts to one
  IR user message with one text block. On encode, the string shorthand is
  rendered exactly when the IR conversation is one user message whose
  content is exactly one text block AND there is no system content
  (`instructions` would force the array form); every other conversation,
  including one whose single user message has multiple text blocks, renders
  as an item array. A non-system message whose decoded content is empty MUST carry a
  single empty `TextBlock` (spec/01 §3.3).
- **N-R-3 (content normalization).** String item content becomes one IR
  `TextBlock`. A parts array becomes one block per part (`input_text` and
  `output_text` → `TextBlock`, `input_image` → `ImageBlock` via N-R-4);
  other part types are dropped as unsupported-semantic losses. On encode,
  user content renders as a plain string when it is exactly one text block
  and as a parts array otherwise (multiple text blocks, images); assistant
  text renders as a message item with string content.
- **N-R-4 (image URL normalization).** An `input_image` part's
  `image_url` (a plain string, not an object) that is a syntactically
  valid absolute `https:` URL converts to `ImageBlock.URL`; a
  `data:<media>;base64,<payload>` URL whose media type starts with
  `image/` (case-insensitive) and is not bare `image/` converts to
  `ImageBlock.{MediaType, Data}`. Anything else is dropped as an
  unsupported-semantic loss. On encode the inverse forms are emitted; an
  IR image carrying both data and URL, a non-image or bare media type, or
  an unusable URL is dropped with an unsupported-semantic loss.
- **N-R-5 (function call items).** Each `function_call` item (request) or
  output item (response) becomes a `ToolUseBlock` with
  `ID: call_id`, keyed by `name`, appended after the text blocks; a maximal
  run of consecutive `function_call` items — including adjacent assistant
  message items — merges into ONE IR assistant message whose content is
  ordered text blocks first, then tool uses, regardless of how the wire
  interleaved the items.
  `arguments` is a wire STRING of opaque JSON text: the decoder MUST wrap
  it into the IR raw JSON string token without parsing or reformatting,
  and the encoder MUST unwrap only the JSON string envelope, preserving
  the argument bytes exactly (INV-1, spec/01). On encode, assistant text
  renders as one message item and each `ToolUseBlock` as one
  `function_call` item.
- **N-R-6 (function_call_output run merge).** A maximal run of
  consecutive `function_call_output` items converts to one IR user
  message whose content is the ordered list of `ToolResultBlock`s, one
  per item, keyed by `call_id`, each with content `[TextBlock(output)]`
  (INV-4). On encode, each IR `ToolResultBlock` renders as one
  `function_call_output` item whose `output` is the concatenation of the
  result's text blocks; non-text result content is an
  unsupported-semantic loss, and a true `IsError` records an
  unmapped-field loss (function_call_output items have no error marker).
- **N-R-7 (parameter mapping and the stop-sequences loss).**
  `temperature` and `top_p` map 1:1. `max_output_tokens` ⇄
  `Params.MaxTokens` is a NAME mapping only and is loss-free in both
  directions. Responses has NO stop-sequences parameter: IR
  `Params.StopSequences` cannot round-trip, and a non-empty value on
  encode records exactly one unmapped-field loss.
- **N-R-8 (unmapped request fields).** `metadata` (string-valued map),
  `text.verbosity`, `text.format`, `reasoning`, `parallel_tool_calls`,
  and per-tool `strict` have no IR equivalent in v1 and are dropped with
  unmapped-field losses (metadata as a single loss per direction).
  Unknown `input[].type` values are dropped with unsupported-semantic
  losses per item.
- **N-R-9 (tool choice mapping).** `auto` and `none` map to the
  eponymous IR modes; `required` and IR mode `any` are equivalent and map
  to each other loss-free; a named `{type: "function", name}` maps to
  mode `tool` with the name. Any other form is an unsupported-semantic
  loss. The mapping is inverted on encode; IR mode `tool` with an empty
  name is an unsupported-semantic loss.
- **N-R-10 (normalized tool-result ordering).** On encode, a
  `function_call_output` item MUST immediately follow the assistant
  message carrying the corresponding function calls. When an IR user turn
  carries both `ToolResultBlock`s and ordinary content blocks, the
  encoder MUST emit the function_call_output items first, followed by one
  trailing user message item holding the ordinary content. This ordering
  is a normalization, not a round-trip guarantee: whenever the semantic
  source order of the IR turn is not preserved (ordinary content that
  preceded or was interleaved with tool results in the IR), the converter
  MUST record a `degraded` loss identifying the reordering.
- **N-R-11 (status mapping).** As specified in §4, covering the
  `status` / `incomplete_details.reason` / `error` triple in both
  directions.
- **N-R-12 (envelope defaults).** As specified in §5; no losses are
  recorded for synthesized envelope fields, the derived
  `total_tokens`, empty `annotations`, or absent `usage`.

## 8. Loss Catalog

Buckets follow [vectors/README.md](../vectors/README.md): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| `usage.total_tokens` | exempt (derived) | both | recomputed as input + output on encode |
| `usage` absent | exempt (envelope) | response → IR | IR usage carries zeros |
| `object`, `status` on encode, `output[].id`, `output[].status`, `output[].role`, regenerated response `id` | exempt (envelope) | both | rendering defaults / transport structure |
| empty `annotations` | exempt (envelope) | both | structural part of output_text |
| `metadata` | unmapped-field | request, both directions (single loss each way) | Responses metadata is string-valued with no IR equivalent; the IR metadata map has no Responses field. Dropped symmetrically as one loss per direction. |
| `text.verbosity`, `text.format` | unmapped-field | request → IR | no IR equivalent in v1 |
| `reasoning` | unmapped-field | request → IR | reasoning effort has no IR equivalent in v1 |
| `parallel_tool_calls` | unmapped-field | request → IR | no IR equivalent in v1 |
| `tools[i].strict` | unmapped-field | request → IR | strict mode has no IR equivalent in v1 |
| `tools[i].type` ≠ `function` | unsupported-semantic | request → IR | tool variant has no IR equivalent |
| unknown `input[i].type` | unsupported-semantic | request → IR | item type has no IR equivalent |
| content part `type` ∉ {`input_text`,`output_text`,`input_image`} | unsupported-semantic | request → IR | part has no IR equivalent |
| malformed https/data `image_url`, non-image data URL | unsupported-semantic | request → IR | only valid https and `data:image/*;base64` URLs are supported |
| `tool_choice` unsupported forms | unsupported-semantic | both | see N-R-9 |
| `params.stop_sequences` | unmapped-field | IR → request | Responses has no stop-sequences parameter; one loss when non-empty |
| `output[i]` `type` = `reasoning` or other | unsupported-semantic | response → IR | one loss per item |
| output content `type` ≠ `output_text` | unsupported-semantic | response → IR | part has no IR equivalent |
| non-empty `annotations` | unmapped-field | response → IR | annotations have no IR equivalent in v1 |
| `incomplete_details.reason` ≠ `max_output_tokens` | unmapped-value | response → IR | maps to IR `other` |
| `error` / `status: "failed"` | unsupported-semantic | response → IR | maps to IR `other`; loss names the error code |
| `stop_sequence` value | unmapped-value | IR → response | Responses status carries no stop-sequence identity |
| `is_error` on tool result | unmapped-field | IR → request | function_call_output items have no error marker |
| IR image both data+URL / bad media type / unusable URL | unsupported-semantic | IR → request | see N-R-4 |
| IR `ImageBlock`/`ToolResultBlock` in assistant input; non-text blocks in tool result | unsupported-semantic | IR → request | see N-R-5, N-R-6 |
| interleaved tool results and ordinary content in one IR user turn | degraded | IR → request | N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved |
| IR stop reason outside the five known values; `status` outside the three known values; tool input not a JSON string | structural error | both | not a loss; conversion fails |

## 9. References

- [00 — Scope and Architecture](00-scope-and-architecture.md)
- [01 — Intermediate Representation](01-intermediate-representation.md)
- [02 — Loss Policy](02-loss-policy.md)
- [03 — Model Handling](03-model-handling.md)
- [vectors/README.md](../vectors/README.md) — loss buckets and
  from-ir rendering defaults
- OpenAI API reference,
  <https://platform.openai.com/docs/api-reference/responses>
  (retrieved 2026-08-28)
