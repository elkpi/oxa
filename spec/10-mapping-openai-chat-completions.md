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
  `response_format`, `logprobs`, `top_logprobs`. `metadata` is a string
  map and maps to `Request.Metadata` (since 2.5.0).
- **Message**: `role` (`system` | `developer` | `user` | `assistant` | `tool`),
  `content` (string or content-parts array), `tool_calls[]`
  (assistant), `tool_call_id` (tool), `function_call` (legacy).
- **ContentPart**: `type` (`text` | `image_url`), `text`,
  `image_url.url`.
- **Tool**: `type: "function"` with `function.name`,
  `function.description`, `function.parameters` (JSON Schema); or `type: "custom"` with
  `custom.name`, `custom.description`, `custom.format` (spec/13, since 2.3.1).
- **ToolChoice**: string mode (`none` | `auto` | `required`) or named object (`type: "function", function: {name}` / `type: "custom", custom: {name}`).
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
| `messages[role=assistant].tool_calls[]` | `ToolUseBlock`s and `CustomToolUseBlock`s appended after text blocks | N-CC-4, N-CC-14 |
| `messages[role=tool]` (consecutive run) | one user message of `ToolResultBlock`s / `CustomToolResultBlock`s | N-CC-5, N-CC-14 |
| `messages[].tool_call_id` | `ToolResultBlock.ToolUseID` / `CustomToolResultBlock.ToolUseID` | N-CC-5, N-CC-14 |
| `tools[].function.{name,description,parameters,strict}` | `Tools[].{Name,Description,InputSchema,Strict}` | bidirectional; schema bytes carried verbatim (INV-1), strict preserves omission, false and true |
| `tools[].custom.{name,description,format}` | `Tools[type=custom].{Name,Description,Format}` | N-CC-14 (since 2.3.1) |
| `tool_choice` | `ToolChoice` (`required` ⇄ mode `any`; named function/custom ⇄ mode `tool`) | N-CC-7, N-CC-14 |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `max_tokens` | `Params.MaxTokens` | 1:1 |
| `max_completion_tokens` | `Params.MaxTokens` + `NativeAnnotation` | 映射至 Params.MaxTokens，通过 NativeAnnotation (kind: "max_completion_tokens") 记忆拼写，与 max_tokens 互斥（N-NAT-2/3，规范 2.4.10） |
| `store` | `Request.Native` (`kind: "store"`) | 布尔留存标记，同面与 Responses 转换无损保真，跨面至 Anthropic 时 store:true 报 unsupported-semantic，store:false 零损（N-NAT-2/3，规范 2.4.10） |
| `stream` | `Request.Native` (`kind: "stream"`) | 传输控制标记，同面精准还原，跨面转换 0 loss（N-NAT-2/3，规范 2.4.10） |
| `stream_options` | `Request.Native` (`kind: "stream_options"`) | 流式选项，包含 include_usage 校验与未知子字段审计；跨面降级为 degraded 损失（N-NAT-2/3，规范 2.4.10） |
| `prompt_cache_key` | `Request.Native` (`kind: "prompt_cache_key"`) | OpenAI prompt 缓存分区键；OpenAI 家族内 0 loss 回转；跨面至 Anthropic 时记 unmapped-field（spec/15 §3.7，规范 2.5.0） |
| `prompt_cache_options` | `Request.Native` (`kind: "prompt_cache_options"`) | OpenAI prompt 缓存全局配置；OpenAI 家族内 0 loss 回转；跨面至 Anthropic 时记 unmapped-field（spec/15 §3.7，规范 2.5.0） |
| `prompt_cache_breakpoint` (content part) | `Block.Native` / `SystemBlock.Native` (`kind: "prompt_cache_breakpoint"`) | OpenAI 显式内容缓存断点；OpenAI 家族内 0 loss 回转；单文本块简写防护；跨面至 Anthropic 时记 unmapped-field（spec/15 §3.7，规范 2.5.0） |
| `stop` | `Params.StopSequences` | 1:1 both directions (the Loss Catalog `stop_sequence` entry is the response-side value loss, not this parameter) |
| `reasoning_effort` | `Params.ReasoningEffort` | 1:1; unknown values dropped with unmapped-value (N-CC-12, since 2.0; xhigh and max since 2.4) |
| `verbosity` | `Params.Verbosity` | `low`, `medium`, `high` map 1:1 since 2.5.0. Absent or null means unset; unknown strings produce `unmapped-value` at `verbosity`; non-strings are structural errors |
| `image_url.url` (https or `data:image/...;base64,...`) | `ImageBlock.URL` or `ImageBlock.{MediaType,Data}` | N-CC-3 |
| `metadata` | `Request.Metadata` | 1:1 string map, both directions, 0 loss (since 2.5.0). Absent, JSON null, and `{}` are empty. A non-object, non-string value, or empty key (`""`) is a structural error |
| `parallel_tool_calls` | `ToolChoice.DisableParallel` | boolean; inverted polarity: `parallel_tool_calls = !disable_parallel` (N-CC-16, since 2.5.0) |
| `functions`, `function_call` | — | unmapped-field loss |
| `response_format` | `Params.ResponseFormat` | N-CC-13 (since 2.1); 1:1 unwrapped json_schema |
| `logprobs`, `top_logprobs` | — | unmapped-field loss |

For a function declaration, wire `function.strict: null` is normalized to an
absent `Tool.Strict` without loss, just like an omitted field. A present
non-null value MUST be a boolean. On encode an absent `Tool.Strict` is omitted;
this does not preserve the original `null` spelling. Canonical IR continues
to forbid `null` (spec/01 §2).

On encode (IR → CC), when no system block carries `role: "developer"`,
`Request.System` is rendered as exactly one leading `role: "system"` message
whose string content is the concatenation of all system block texts (N-CC-1).
When any system block carries `role: "developer"`, each system block renders
as its own `system` or `developer` message in order. `Params.StopSequences`
renders as `stop`; all other params render 1:1 with their omitempty semantics.

来源限定的 `Tool[type=native]` 及指向它的 named choice 不映射为 CC function/custom，按 [spec/16](16-native-tool-declarations.md) 逐声明省略并记录 `unsupported-semantic` loss。

## 4. Response Field Mapping

| CC field | IR destination | Notes |
|---|---|---|
| `choices[0].message.content` | `Response.Content` blocks | only the first choice is converted |
| `choices[0].message.reasoning_content` | `ThinkingBlock.Thinking` | N-CC-12 (since 2.0) |
| `choices[0].message.tool_calls[]` | `ToolUseBlock`s appended after the text blocks | N-CC-4 |
| `choices[0].finish_reason` | `Response.StopReason` | N-CC-11 |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.prompt_tokens` | `Usage.InputTokens` | |
| `usage.completion_tokens` | `Usage.OutputTokens` | |
| `usage.prompt_tokens_details.cached_tokens` | `Usage.InputTokensDetails.CachedTokens` | 1:1 (since 2.0) |
| `usage.completion_tokens_details.reasoning_tokens` | `Usage.OutputTokensDetails.ReasoningTokens` | 1:1 (since 2.0) |
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
  `role: "system"` or `role: "developer"` message, in wire order, converts to IR system blocks
  (text parts only; non-text parts in a system/developer message are
  unsupported-semantic losses; `developer` messages set `role: "developer"` on each extracted block since 2.5.0).
  On encode, when no system block carries `role: "developer"`, all IR system blocks MUST be
  concatenated, in order, into exactly one leading system message whose
  content is the concatenated string. When any system block carries `role: "developer"`,
  each system block renders as its own `system` or `developer` message in order.
- **N-CC-2 (content normalization).** String content becomes one IR
  `TextBlock`. A parts array becomes one block per part (`text` →
  `TextBlock`, `image_url` → `ImageBlock` via N-CC-3, `file` →
  `DocumentBlock` via N-CC-14); part types other
  than `text`, `image_url`, and `file` are dropped as unsupported-semantic
  losses. A non-system message whose decoded content is empty MUST
  carry a single empty `TextBlock` (spec/01 §3.3). On encode, user
  content renders as a plain string when it is text-only and as a parts
  array when it contains at least one image or document.
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
- **N-CC-12 (reasoning content and effort).** On decode,
  `message.reasoning_content` (nonstream) and
  `choices[0].delta.reasoning_content` (stream) convert to
  `ThinkingBlock.Thinking` / `thinking_delta`; Chat Completions never supplies
  `signature`. On encode, each IR `ThinkingBlock` renders as string
  `reasoning_content` on the message or choice delta; a present `signature`
  records an `unmapped-field` loss. Request `reasoning_effort` maps 1:1 to
  `Params.ReasoningEffort` (`minimal`, `low`, `medium`, `high`); unknown inbound
  values are dropped with an `unmapped-value` loss.

- **Response format mapping (N-CC-13).** `response_format` maps 1:1 to
  `Params.ResponseFormat` (since 2.1). `type: "text"` maps to text variant;
  `type: "json_object"` maps to json_object variant; `type: "json_schema"`
  unwraps nested `json_schema` into flat fields (`name`, `description`, `schema`,
  `strict`). Unrecognized `type` values are dropped with an `unmapped-value` loss.
  On encode, IR `ResponseFormat` renders back as CC `response_format` with nested
  `json_schema` wrapping. A JSON Schema without a non-empty name cannot be
  represented: omit the entire response format and record one
  `unsupported-semantic` loss at `params.response_format`, with field `name`.
  Do not synthesize a name.

- **Custom tool mapping (N-CC-14).** Chat Completions natively supports Custom Tools
  (spec/13, since 2.3.1). `tools[type=custom].custom.{name,description,format}` maps
  1:1 to IR `Tool{Type: "custom", Name, Description, Format}`. When `custom.format` is omitted,
  IR `Tool.Format` is absent; when `custom.format` is present with `type: "text"` or valid `grammar`,
  it maps to the corresponding IR format. Unrecognized format types or grammars record an
  `unsupported-semantic` loss on `tools[i].custom.format`. `tool_choice: {type: "custom", custom: {name}}`
  maps 1:1 to IR `ToolChoice{Mode: "tool", ToolType: "custom", Name}`. Assistant message
  `tool_calls[type=custom].custom.{name,input}` maps to IR `CustomToolUseBlock`, where `input`
  is a required string (even if empty). Tool messages (`role: "tool"`) answering custom tool calls
  map to IR `CustomToolResultBlock`. When `output` is a string, it encodes as string `content`;
  when `output` is an array of text/image content blocks, it encodes as an array of `ContentPart`s
  without collapsing to a plain string. Both directions preserve string vs array distinction (spec/13 §2).

- **N-CC-14 (file content parts, since 2.5.0).** Chat Completions `file` content parts
  in user messages map to `DocumentBlock` (request-side only).
  - **Decode.** Each `file` content part must be a JSON object containing a `file` object.
    The inner `file` object must specify exactly one of:
    - `file_id`: a non-empty string. Maps to `DocumentBlock{FileID: file_id}`.
    - `file_data`: a non-empty string. May be raw base64 (maps to `DocumentBlock{Data: file_data, MediaType: "application/pdf"}`)
      or a `data:<media>;base64,<payload>` URL (maps to `DocumentBlock{Data: payload, MediaType: media}`).
    Missing `file` object, specifying none or more than one of `file_id` and `file_data`, or non-string/empty values, is a structural error.
    Optional wire fields: `file.filename` (string) is dropped with an `unmapped-field` loss.
    Unknown fields on the part or on `file` record `unmapped-field` losses in source order.
    In response decoding, `file` content parts have no IR equivalent and record an `unsupported-semantic` loss.
  - **Encode.** In a user message, `DocumentBlock` renders as a `file` content part
    (forcing array representation of `content`):
    - Conflicting sources (more than one of `FileID`, `Data`, `URL` non-empty) or missing sources record an `unsupported-semantic` loss.
    - `FileID` present: renders `{"type": "file", "file": {"file_id": *FileID}}` with zero loss.
    - `Data` present: if `MediaType` is present and not `"application/pdf"`, the non-PDF document cannot be represented in Chat Completions and records an `unsupported-semantic` loss (detail: `"Chat Completions file content parts do not support non-PDF media types; file dropped"`). Otherwise renders `{"type": "file", "file": {"file_data": *Data}}` with zero loss.
    - `URL` present: Chat Completions has no URL source form for files. The block is dropped with an `unsupported-semantic` loss
      (detail: `"Chat Completions file content parts do not support url sources; file dropped"`).
    `DocumentBlock` in an assistant message cannot be rendered and records an `unsupported-semantic` loss (detail: `"IR DocumentBlock cannot be rendered in a Chat Completions assistant message"`).

- **N-CC-15 (refusal content and finish reason, since 2.5.0).** Chat Completions supports
  refusals via `message.refusal` (response and assistant replay).
  - **Decode.** When `choices[0].message.refusal` is a non-empty string, it maps to
    `RefusalBlock{Refusal: refusal}` in `Response.Content`. If `choices[0].finish_reason` is
    `content_filter`, `StopReason` is `refusal`. In request history, an assistant message
    with non-empty `refusal` similarly decodes to `RefusalBlock`.
  - **Encode.** When `Response.Content` contains a `RefusalBlock`, its `Refusal` string is
    rendered into `choices[0].message.refusal`. The block is omitted from `message.content`
    (rendering `content: null` when no other text blocks exist). If `RefusalBlock.Category`
    is non-empty, an `unsupported-semantic` loss is recorded at `choices[0].message.refusal`
    (detail: `"Anthropic refusal category has no Chat Completions equivalent"`).
    When `Response.StopReason == "refusal"`, `choices[0].finish_reason` renders as `"content_filter"`.
    In request history, `RefusalBlock` in an assistant message renders into `message.refusal`.
    `RefusalBlock` in a user or system message records an `unsupported-semantic` loss.

- **N-CC-16 (parallel tool calling, since 2.5.0).** Chat Completions `parallel_tool_calls`
  maps to IR `ToolChoice.DisableParallel` with inverted boolean polarity:
  - **Decode.** When `parallel_tool_calls` is present, it must be a boolean (non-booleans are structural errors).
    It maps to `ToolChoice.DisableParallel = !parallel_tool_calls` (`true` -> `false`, `false` -> `true`).
    If `tool_choice` is absent on wire, `ToolChoice` is synthesized with `Mode: "auto"` and `DisableParallel`.
    If `parallel_tool_calls` is absent, `ToolChoice.DisableParallel` is unset.
    0 decode loss.
  - **Encode.** When `Request.ToolChoice` carries `DisableParallel`, `parallel_tool_calls = !*req.ToolChoice.DisableParallel`.
    When `DisableParallel` is absent, `parallel_tool_calls` is omitted.
    0 encode loss.

## 8. Loss Catalog

Buckets follow [spec/02 §9](02-loss-policy.md#9-loss-conventions--derived-and-envelope-fields): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| `usage.total_tokens` | exempt (derived) | both | recomputed as prompt + completion on encode |
| `object`, `created`, `choices[].index`, `message.role`, regenerated response `id` | exempt (envelope) | both | rendering defaults / transport structure |
| `logprobs`, `top_logprobs` | unmapped-field | request → IR | log-probability sampling has no IR equivalent |
| `functions`, `function_call` | unmapped-field | request → IR (also per-message `function_call`) | legacy shapes have no IR equivalent |
| `response_format.type` unknown value | unmapped-value | request → IR | unknown response_format type dropped (N-CC-13) |
| `content[i].signature` | unmapped-field | IR → response/request | Chat Completions carries no signature field on reasoning_content (N-CC-12) |
| `content[i].citations` (cross-face: anthropic) | unsupported-semantic | IR → response/request | anthropic text citations; the target face cannot preserve source locations and replay metadata (N-AN-18) |
| `content[i].citations` (cross-face: openai) | unsupported-semantic | IR → response/request | openai text annotations; the target face cannot preserve source locations and replay metadata (N-R-18) |
| `messages[i].content[j]`, `content[i]` | unsupported-semantic | IR → request/response | `web_search_call` omitted; not rewritten as a function call (N-R-16) |
| `reasoning_effort` unknown value | unmapped-value | request → IR | unknown effort values are dropped (N-CC-12) |
| `tools[i].type` ∉ {`function`,`custom`} | unsupported-semantic | request → IR | tool variant has no IR equivalent |
| content part `type` ∉ {`text`,`image_url`,`file`} | unsupported-semantic | request → IR | part has no IR equivalent |
| `part.file.filename` | unmapped-field | request → IR | Chat Completions file filename has no IR equivalent (N-CC-14) |
| `IR DocumentBlock with url source` | unsupported-semantic | IR → request | Chat Completions file content parts do not support url sources; file dropped (N-CC-14) |
| malformed https/data `image_url`, non-image data URL | unsupported-semantic | request → IR | only valid https and `data:image/*;base64` URLs are supported |
| `tool_choice` unsupported forms | unsupported-semantic | both | see N-CC-7 |
| `finish_reason` unknown value | unmapped-value | response → IR | maps to IR `other` |
| `stop_sequence` value | unmapped-value | IR → response | CC `finish_reason: "stop"` does not identify the matched stop sequence |
| `is_error` on tool result | unmapped-field | IR → request | CC tool messages have no `is_error` field |
| IR image both data+URL / bad media type / unusable URL | unsupported-semantic | IR → request | see N-CC-3 |
| IR `ImageBlock`/`ToolResultBlock` in assistant message; non-text blocks in tool result | unsupported-semantic | IR → request | see N-CC-6, N-CC-8 |
| interleaved tool results and ordinary content in one IR user turn | degraded | IR → request | N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved |
| unrecognized non-exempt property on a recognized Chat Completions object | unmapped-field | request/response → IR | one loss per present property; see spec/02 §4 |
| IR native request annotations with no CC representation | unmapped-field / degraded / unsupported-semantic | IR → request | reported on encode per spec/15 §4 |
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
