# 10 — Mapping: OpenAI Chat Completions

This document is the normative mapping between the OpenAI Chat Completions
(CC) face and the oxa intermediate representation (spec/01). It covers the
non-streaming request and response conversion implemented by the Go spoke
`go/openai/chatcompletions`. Streaming is out of scope here (see §6).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" are to be
interpreted as described in RFC 2119 and RFC 8174 (see
[README.md](README.md)).

## 1. Overview

The CC face is the OpenAI `POST /v1/chat/completions` protocol: a flat
`messages` array of role-tagged messages, function tools, and sampling
parameters. Conversion to the IR is total for the supported subset
(text, system, multi-turn, parameters, tools, images); every input the
converter cannot carry semantically is dropped with a loss record rather
than an error (spec/02). Errors are reserved for structural type
violations of known fields: unknown `role` values, a non-string content
leaf where a string is required, a missing `finish_reason`, a request
with no conversation messages, or choices missing entirely.

## 2. Wire Objects

Reference snapshot (retrieved 2026-08-28):

- API reference: <https://platform.openai.com/docs/api-reference/chat>
- Guide: <https://platform.openai.com/docs/guides/text-generation>

The wire objects in scope are:

- **Request**: `model`, `messages[]`, `temperature`, `top_p`,
  `max_tokens`, `stop`, `tools[]`, `tool_choice`, and the unsupported
  `parallel_tool_calls`, `functions`, `function_call`,
  `response_format`, `logprobs`, `top_logprobs`, `metadata`.
- **Message**: `role` (`system` | `user` | `assistant` | `tool`),
  `content` (string or content-parts array), `tool_calls[]`
  (assistant), `tool_call_id` (tool), `function_call` (legacy).
- **ContentPart**: `type` (`text` | `image_url`), `text`,
  `image_url.url`.
- **Tool**: `type: "function"` with `function.name`,
  `function.description`, `function.parameters` (JSON Schema).
- **Response**: `id`, `object`, `created`, `model`, `choices[]`,
  `usage` (`prompt_tokens`, `completion_tokens`, `total_tokens`).
- **Choice**: `index`, `message`, `finish_reason`.

## 3. Request Field Mapping (CC → IR / IR → CC)

| CC field | IR destination | Notes |
|---|---|---|
| `model` | `Request.Model` | passed through the model map (spec/03) |
| `messages[role=system].content` | `Request.System[]` (in wire order) | N-CC-1 |
| `messages[role=user].content` | `Messages[role=user].Content` blocks | N-CC-2 |
| `messages[role=assistant].content` | `Messages[role=assistant].Content` blocks | N-CC-2 |
| `messages[role=assistant].tool_calls[]` | `ToolUseBlock`s appended after the text blocks | N-CC-4 |
| `messages[role=tool]` (consecutive run) | one user message of `ToolResultBlock`s | N-CC-5 |
| `messages[].tool_call_id` | `ToolResultBlock.ToolUseID` | N-CC-5 |
| `tools[].function.{name,description,parameters}` | `Tools[].{Name,Description,InputSchema}` | schema bytes carried verbatim (INV-1) |
| `tool_choice` | `ToolChoice` (`required` ⇄ mode `any`; named function ⇄ mode `tool`) | N-CC-7 |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `max_tokens` | `Params.MaxTokens` | 1:1 |
| `stop` | `Params.StopSequences` | 1:1 both directions (the Loss Catalog `stop_sequence` entry is the response-side value loss, not this parameter) |
| `image_url.url` (https or `data:image/...;base64,...`) | `ImageBlock.URL` or `ImageBlock.{MediaType,Data}` | N-CC-3 |
| `metadata` | — | unmapped-field loss, both directions as a single loss each way |
| `parallel_tool_calls` | — | unmapped-field loss |
| `functions`, `function_call` | — | unmapped-field loss |
| `response_format` | — | unmapped-field loss |
| `logprobs`, `top_logprobs` | — | unmapped-field loss |

On encode (IR → CC), `Request.System` is rendered as exactly one leading
`role: "system"` message whose string content is the concatenation of all
system block texts (N-CC-1); `Params.StopSequences` renders as `stop`;
all other params render 1:1 with their omitempty semantics.

## 4. Response Field Mapping

| CC field | IR destination | Notes |
|---|---|---|
| `choices[0].message.content` | `Response.Content` blocks | only the first choice is converted |
| `choices[0].message.tool_calls[]` | `ToolUseBlock`s appended after the text blocks | N-CC-4 |
| `choices[0].finish_reason` | `Response.StopReason` | N-CC-11 |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.prompt_tokens` | `Usage.InputTokens` | |
| `usage.completion_tokens` | `Usage.OutputTokens` | |
| `usage.total_tokens` | — | DERIVED, exempt (recomputed on encode) |
| `object`, `created`, `choices[].index`, `message.role` | — | ENVELOPE, exempt |
| `choices[1:]` | — | not carried; a response MUST carry at least one choice (error otherwise) |

`finish_reason` mapping: `stop` → `end_turn`, `length` → `max_tokens`,
`content_filter` → `refusal`, `tool_calls` → `tool_use`; any other value
maps to `other` with an `unmapped-value` loss; a missing value is a
structural error. On encode the inverse mapping is used, with the
exception that IR `stop_sequence` renders as `finish_reason: "stop"`
plus a value loss for the unmatched-sequence identity (see Loss
Catalog); an IR stop reason outside the five known values is a
structural error.

## 5. Envelope rendering defaults (IR → CC)

When the IR lacks CC envelope fields, the encoder synthesizes fixed
values and records no loss (from-ir rendering defaults,
[vectors/README.md](../vectors/README.md)): `object: "chat.completion"`,
`created: 0`, a single choice with `index: 0` and
`message.role: "assistant"`, and `usage.total_tokens` recomputed as
input + output.

## 6. Streaming Event Mapping

Deferred to [spec/20](20-streaming-semantics.md) (streaming semantics). This document
MUST NOT be cited for streaming behavior.

## 7. Normalization Rules

Each rule has a stable ID usable as a vector tag.

- **N-CC-1 (system extraction and concatenation).** Every
  `role: "system"` message, in wire order, converts to IR system blocks
  (text parts only; non-text parts in a system message are
  unsupported-semantic losses). On encode, all IR system blocks MUST be
  concatenated, in order, into exactly one leading system message whose
  content is the concatenated string.
- **N-CC-2 (content normalization).** String content becomes one IR
  `TextBlock`. A parts array becomes one block per part (`text` →
  `TextBlock`, `image_url` → `ImageBlock` via N-CC-3); part types other
  than `text` and `image_url` are dropped as unsupported-semantic
  losses. A non-system message whose decoded content is empty MUST
  carry a single empty `TextBlock` (spec/01 §3.3). On encode, user
  content renders as a plain string when it is text-only and as a parts
  array when it contains at least one image.
- **N-CC-3 (image URL normalization).** An `image_url.url` that is a
  syntactically valid absolute `https:` URL converts to
  `ImageBlock.URL`. A `data:<media>;base64,<payload>` URL whose media
  type starts with `image/` (case-insensitive) and is not bare
  `image/` converts to `ImageBlock.{MediaType, Data}`. Anything else —
  non-https schemes, malformed https/data URLs, non-image data URLs —
  is dropped as an unsupported-semantic loss. On encode the inverse
  forms are emitted; an IR image carrying both data and URL, a
  non-image or bare media type, or an unusable URL is dropped with an
  unsupported-semantic loss.
- **N-CC-4 (assistant tool calls).** Each `tool_calls[]` entry of type
  `function` becomes a `ToolUseBlock` appended after the assistant
  message's normal content blocks, in wire order; non-`function` tool
  call types are unsupported-semantic losses. `function.arguments` is
  opaque JSON text: the decoder MUST wrap it into the IR raw JSON
  string token without parsing or reformatting, and the encoder MUST
  unwrap only the JSON string envelope, preserving the argument bytes
  exactly (INV-1, spec/01). A message with `content: null` and tool
  calls converts to tool-use blocks only.
- **N-CC-5 (tool-result run merge).** A maximal run of consecutive
  `role: "tool"` messages converts to one IR user message whose content
  is the ordered list of `ToolResultBlock`s, one per wire message, keyed
  by `tool_call_id` (INV-4). Each result's content is decoded by
  N-CC-2.
- **N-CC-6 (IR tool result rendering).** On encode, each IR
  `ToolResultBlock` in a user message renders as one
  `role: "tool"` message with `tool_call_id` and string content
  equal to the concatenation of the result's text blocks. Non-text
  result content blocks are unsupported-semantic losses, and a true
  `IsError` records an unmapped-field loss (CC tool messages have no
  `is_error`).
- **N-CC-7 (tool choice mapping).** `auto` and `none` map to the
  eponymous IR modes; `required` maps to mode `any`; a named
  `{type: "function", function: {name}}` maps to mode `tool` with the
  name. Any other form is an unsupported-semantic loss. The mapping is
  inverted on encode; IR mode `tool` with an empty name is an
  unsupported-semantic loss.
- **N-CC-8 (assistant rendering).** On encode, assistant `TextBlock`s
  are concatenated, in order, into the message's string `content`, and
  `ToolUseBlock`s render as `tool_calls[]` (N-CC-4). `ImageBlock` and
  `ToolResultBlock` in an assistant message are unsupported-semantic
  losses.
- **N-CC-9 (normalized tool-result ordering).** On encode, a wire
  `role: "tool"` message MUST immediately follow the assistant message
  carrying the corresponding tool calls. When an IR user turn carries
  both `ToolResultBlock`s and ordinary content blocks, the encoder MUST
  emit the tool messages first, followed by one trailing
  `role: "user"` message holding the ordinary content. This ordering is
  a normalization, not a round-trip guarantee: whenever the semantic
  source order of the IR turn is not preserved (ordinary content that
  preceded or was interleaved with tool results in the IR), the
  converter MUST record a `degraded` loss identifying the reordering.
- **N-CC-10 (finish reason mapping).** As specified in §4. Unknown
  values map to IR `other` with an `unmapped-value` loss; the empty
  value is a structural error.
- **N-CC-11 (envelope defaults).** As specified in §5; no losses are
  recorded for synthesized envelope fields or the derived
  `total_tokens`.

## 8. Loss Catalog

Buckets follow [vectors/README.md](../vectors/README.md): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| `usage.total_tokens` | exempt (derived) | both | recomputed as prompt + completion on encode |
| `object`, `created`, `choices[].index`, `message.role`, regenerated response `id` | exempt (envelope) | both | rendering defaults / transport structure |
| `metadata` | unmapped-field | request, both directions (single loss each way) | CC request metadata has no IR equivalent; the IR metadata map has no CC field. Dropped symmetrically as one loss per direction. |
| `logprobs`, `top_logprobs` | unmapped-field | request → IR | log-probability sampling has no IR equivalent |
| `parallel_tool_calls` | unmapped-field | request → IR | no IR equivalent in v1 |
| `functions`, `function_call` | unmapped-field | request → IR (also per-message `function_call`) | legacy shapes have no IR equivalent |
| `response_format` | unmapped-field | request → IR | no IR equivalent in v1 |
| `tools[i].type` ≠ `function` | unsupported-semantic | request → IR | tool variant has no IR equivalent |
| content part `type` ∉ {`text`,`image_url`} | unsupported-semantic | request → IR | part has no IR equivalent |
| malformed https/data `image_url`, non-image data URL | unsupported-semantic | request → IR | only valid https and `data:image/*;base64` URLs are supported |
| `tool_choice` unsupported forms | unsupported-semantic | both | see N-CC-7 |
| `finish_reason` unknown value | unmapped-value | response → IR | maps to IR `other` |
| `stop_sequence` value | unmapped-value | IR → response | CC `finish_reason: "stop"` does not identify the matched stop sequence |
| `is_error` on tool result | unmapped-field | IR → request | CC tool messages have no `is_error` field |
| IR image both data+URL / bad media type / unusable URL | unsupported-semantic | IR → request | see N-CC-3 |
| IR `ImageBlock`/`ToolResultBlock` in assistant message; non-text blocks in tool result | unsupported-semantic | IR → request | see N-CC-6, N-CC-8 |
| interleaved tool results and ordinary content in one IR user turn | degraded | IR → request | N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved |
| IR stop reason outside the five known values | structural error | IR → response | not a loss; conversion fails |

## 9. References

- [00 — Scope and Architecture](00-scope-and-architecture.md)
- [01 — Intermediate Representation](01-intermediate-representation.md)
- [02 — Loss Policy](02-loss-policy.md)
- [03 — Model Handling](03-model-handling.md)
- [vectors/README.md](../vectors/README.md) — loss buckets and
  from-ir rendering defaults
- OpenAI API reference, <https://platform.openai.com/docs/api-reference/chat>
  (retrieved 2026-08-28)
