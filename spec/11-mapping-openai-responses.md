# 11 — Mapping: OpenAI Responses

This document is the normative mapping between the OpenAI Responses (RE)
face and the oxa intermediate representation (spec/01). It covers the
non-streaming request and response conversion implemented by the Go spoke
`go/openai/responses`. Streaming is out of scope here (see §6).

规范 2.2 新增的 custom 工具声明、选择、调用与结果映射见
[13 — Custom Tools](13-custom-tools.md)（N-C-1 至 N-C-5）；这些扩展与现有
function 映射并存，不得通过名称或输入内容推断类型。

规范 2.3 的专用 Apply Patch 声明、选择、请求/响应调用与结果见
[14 — Apply Patch](14-apply-patch.md)。它与名为 apply_patch 的 custom 工具严格分离。

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
required, a request with neither conversation input nor system content, an unknown response
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
- **Tool**: flat `type: "function"` with `name`, `description`,
  `parameters` (JSON Schema), `strict`; container types (`type: "namespace"`, `type: "toolbox"`)
  with `name`, optional `description`, and `tools[]`; or native tool declarations
  (e.g. `web_search`, `file_search`, `code_interpreter`, `computer`, `mcp`, `tool_search`).
  Tools may carry `"defer_loading": true` (N-R-15).
- **InputItem**: a message item (`type` absent or `"message"`, `role`
  `system` | `developer` | `user` | `assistant`, `content` string or parts array), a
  `function_call` item (`call_id`, `name`, optional `namespace`, `arguments`), a
  `function_call_output` item (`call_id`, `output`), an `additional_tools` item
  (`tools[]`), a `reasoning` item (`summary[]` text parts), or an unknown type.
- **ContentPart**: `type` (`input_text` | `output_text` | `input_image`),
  `text`, `image_url` (a plain https or `data:` URL string, unlike the
  Chat Completions object form).
- **Response**: `id`, `object`, `status` (`completed` | `incomplete` |
  `failed`), `model`, `output[]`, `usage` (`input_tokens`,
  `output_tokens`, `total_tokens`), `incomplete_details.reason`, `error`.
- **OutputItem**: `type` (`message` | `function_call` | `reasoning`),
  `id`, `status`, `role`, `content[]` (`output_text` parts with
  `annotations`), `call_id`, `name`, optional `namespace`, `arguments`.

来源限定的 `Tool[type=native]`：若 `source == "openai"`，在 Responses 编码时原样保真恢复其原始声明文本；若来源非 `openai`（如 `anthropic`）及指向其的 named choice，不映射为 Responses 工具，按 [spec/16](16-native-tool-declarations.md) 逐声明省略并记录 `unsupported-semantic` loss。

## 3. Request Field Mapping (RE → IR / IR → RE)

| RE field | IR destination | Notes |
|---|---|---|
| `model` | `Request.Model` | passed through the model map (spec/03) |
| `instructions` | `Request.System[]` (first block) | N-R-1 |
| `input` (string) | one user message, one text block | N-R-2 |
| `input[role=system/developer]` | `Request.System[]`, after instructions, in shared wire order | N-R-1; one `degraded` loss per developer item because IR cannot retain the role distinction |
| `input[role=user/assistant].content` | `Messages[].Content` blocks | N-R-3 |
| `input[type=function_call]` (consecutive run, incl. adjacent assistant message items) | one assistant message; `ToolUseBlock`s after any text blocks | N-R-5 |
| `input[type=function_call_output]` (consecutive run) | one user message of `ToolResultBlock`s | N-R-6 |
| `input[type=reasoning].summary[]` | assistant `ThinkingBlock`s | N-R-13; accepts `summary_text` and legacy `output_text` in encounter order |
| `tools[].{name,description,parameters}` | `Tools[].{Name,Description,InputSchema}` | flat form; schema bytes carried verbatim (INV-1) |
| `tools[type=namespace/toolbox]` | `Tools[]` | flattened with `<name>.` prefix; container preserved in native annotation (N-R-15) |
| `tools[type=web_search/...]` | `Tools[]` (`type: "native"`) | source="openai", definition verbatim (N-R-15, spec/16) |
| `tools[].defer_loading` | `Tools[].Native` (`kind: "defer_loading"`) | 1:1 round-trip loss-free (N-R-15, spec/15) |
| `input[type=additional_tools]` | `Tools[]` | hoisted to request Tools (N-R-15) |
| `tool_choice` | `ToolChoice` (`required` ⇄ mode `any`; `{type:"function",name}` ⇄ mode `tool`) | N-R-9; namespace split/join (N-R-15) |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `max_output_tokens` | `Params.MaxTokens` | name mapping only; loss-free both directions (N-R-7) |
| `image_url` (part field, https or `data:image/...;base64,...`) | `ImageBlock.URL` or `ImageBlock.{MediaType,Data}` | N-R-4 |
| `metadata` | — | unmapped-field loss, both directions as a single loss each way |
| `text.format` | `Params.ResponseFormat` | flat format object (since 2.1) |
| `text.verbosity` | — | unmapped-field loss |
| `reasoning.effort` | `Params.ReasoningEffort` | 1:1 (N-R-13, since 2.0; xhigh and max since 2.4); other reasoning fields (e.g. summary preference) dropped with unmapped-field |
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
| `output[type=reasoning].summary[]` | `ThinkingBlock.Thinking` | N-R-13 (since 2.0); one block per summary text part; encrypted_content dropped with unmapped-field |
| `output[type=function_call]` | `ToolUseBlock`s after the text blocks | N-R-5 |
| `status` + `incomplete_details.reason` + `error` | `Response.StopReason` | N-R-11 |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.input_tokens` | `Usage.InputTokens` | |
| `usage.output_tokens` | `Usage.OutputTokens` | |
| `usage.input_token_details.cached_tokens` | `Usage.InputTokensDetails.CachedTokens` | 1:1 (since 2.0) |
| `usage.output_token_details.reasoning_tokens` | `Usage.OutputTokensDetails.ReasoningTokens` | 1:1 (since 2.0) |
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

- **N-R-1 (instruction extraction, hierarchy, and ordering).** `instructions`
  converts to the first IR system block. Every message item with
  `role: "system"` or `role: "developer"` appends further system blocks after
  it, preserving their shared wire order and the order of text parts within
  each item. Text content is carried verbatim; non-text parts are
  unsupported-semantic losses. Both roles remain in the high-priority IR
  system field, never ordinary user/assistant messages, so their instructions
  retain precedence over user instructions regardless of their position in
  `input`. This rule does not infer an additional precedence relation between
  system and developer instructions.

  The current IR cannot distinguish developer from system instructions.
  Each developer item MUST therefore record exactly one `degraded` loss at
  `input[i].role`, field `role`, even when all its text is carried. This loss
  MUST explain that the developer/system distinction is not preserved; it
  remains part of the composed loss list for every target face (spec/02 §6).
  On encode, all IR system blocks MUST be concatenated, in order, into the
  `instructions` string. Conversion through IR is not a role-preserving
  round-trip, including when the target face itself supports developer.

  When instruction extraction leaves no conversation messages but at least
  one system block, the decoder MUST synthesize one user message containing
  a single empty `TextBlock`, satisfying the existing IR request shape. It
  MUST record an additional `degraded` loss at `input`, field `input`, naming
  the added empty user message. With neither system blocks nor conversation
  messages, decoding remains a structural error.
- **N-R-2 (input string shorthand).** A string `input` converts to one
  IR user message with one text block. On encode, the string shorthand is
  rendered exactly when the IR conversation is one user message whose
  content is exactly one text block AND there is no system content
  (`instructions` would force the array form); every other conversation,
  including one whose single user message has multiple text blocks, renders
  as an item array. A user or assistant message whose decoded content is empty
  MUST carry a single empty `TextBlock` (spec/01 §3.3).
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
- **N-R-13 (reasoning items and effort).** On non-streaming decode, both
  request `input[type=reasoning].summary[]` and response
  `output[type=reasoning].summary[]` accept standard `summary_text` parts and
  legacy `output_text` parts. Their text converts to `ThinkingBlock`s in
  encounter order; raw JSON and typed entry points MUST preserve the same
  supported summary text. An unknown or absent part `type` MUST skip that
  part and record exactly one `unsupported-semantic` loss at the summary
  part path with field `type`, rather than synthesize an empty thinking block
  or silently discard the text. That whole-part loss covers its children.
  Non-empty `annotations` on a recognized summary part are dropped with one
  `unmapped-field` loss at the part's `.annotations` path; empty annotations
  remain exempt. Other unrecognized properties on recognized summary parts
  follow the raw-field audit policy. An absent or empty request summary adds
  no thinking block; an absent or empty response summary records the existing
  item-level `unsupported-semantic` loss. A non-empty array of unsupported
  parts records only the per-part losses, not an additional empty-summary
  loss. `encrypted_content` is dropped with an `unmapped-field` loss. On
  encode, each IR `ThinkingBlock` renders as a `reasoning` output item with
  a `summary` text part; this clarification does not change the existing
  encoder representation or streaming profile. Request `reasoning.effort`
  maps 1:1 to `Params.ReasoningEffort` (`minimal`, `low`, `medium`, `high`,
  `xhigh`, `max`); other request `reasoning` properties (such as `summary`
  preference) are dropped with `unmapped-field` losses.
- **N-R-14 (text format mapping).** `text.format` maps 1:1 to
  `Params.ResponseFormat` (since 2.1). `type: "text"` maps to text variant;
  `type: "json_object"` maps to json_object variant; `type: "json_schema"`
  maps to json_schema variant (`name`, `description`, `schema`, `strict`).
  On encode, IR `ResponseFormat` renders into request `text.format`.
- **N-R-15 (tool containers, namespacing, and native declarations).** (Since 2.4.9).
  Responses supports hierarchical tool containers (`namespace`, `toolbox`), dynamic tool injection items (`additional_tools`), deferred tool loading (`defer_loading`), and OpenAI native tool declarations (`web_search`, `file_search`, `code_interpreter`, `computer`, `mcp`, `tool_search`, etc.).

  1. Container decoding:
     - When `tools[]` contains an item with `type: "namespace"` or `type: "toolbox"`:
       - `name` MUST be a non-empty string. An absent, non-string, or empty `name`, or a name starting/ending with `.` or containing consecutive `..`, is a structural error.
       - `tools` MUST be a non-empty array of tool declarations. Missing, non-array, or empty `tools` is a structural error.
       - Child declarations are recursively flattened into `req.Tools`.
       - For each child declaration, if its name $C$ does not already begin with `<name>.`, its flattened name becomes `<name>.<C>`.
       - Flattening MUST NOT produce duplicate tool names across the request. Duplicate tool names are structural errors.
       - Each flattened child tool records a `NativeAnnotation{Source: "openai", Kind: "container", Value: "<container_type>"}` and `NativeAnnotation{Source: "openai", Kind: "namespace", Value: "\"<name>\""}`.
       - A child tool with `defer_loading: true` retains `NativeAnnotation{Kind: "defer_loading", Value: "true"}` (per spec/15).
     - When `input[]` contains an item with `type: "additional_tools"`:
       - `tools` MUST be a non-empty array. Missing, non-array, or empty `tools` is a structural error.
       - The child tools are recursively flattened and hoisted to `req.Tools` at request level.

  2. Calling and choice mapping:
     - An `input` or `output` item with `type: "function_call"` or `type: "custom_tool_call"` may carry an optional `namespace` string.
     - On decode, if `namespace` is present and non-empty, and `name` does not start with `<namespace>.`, the IR tool use block name is normalized to `<namespace>.<name>`.
     - In `tool_choice`, a named choice `{type: "function"|"custom", name, namespace?}` or dotted `name` maps to `ToolChoice{Mode: "tool", Name: "<namespace>.<name>", ToolType: ...}`.

  3. OpenAI Native Tools:
     - Any `tools[]` declaration with a recognized or provider-defined native tool type (`web_search`, `file_search`, `code_interpreter`, `computer`, `mcp`, `tool_search`, etc.) that is not `function`, `custom`, `apply_patch`, `namespace`, or `toolbox` decodes to `Tool{Type: "native", Name: tool.Type, Source: "openai", Definition: <raw JSON object text>}` per spec/16. If the wire object contains `name`, outer `name` matches that `name`; otherwise outer `name` defaults to wire `type`.

  4. Encoding (IR → RE):
     - All tools in `req.Tools` render in the request top-level `tools[]`.
     - Tools with `Source == "openai"` and `Type == "native"` are restored verbatim from their `definition` text.
     - Ordinary and custom tools:
       - If consecutive tools share a container `NativeAnnotation` or a dotted prefix `<prefix>.<subname>`, they are grouped into a container object `{"type": "<container_type>", "name": "<prefix>", "tools": [...]}` where child names are stripped of the prefix. If no container annotation is present, dotted names default to `type: "namespace"`.
       - Standalone tools without container prefix render as flat tool declarations.
       - Tools with `defer_loading: true` annotation render `"defer_loading": true`.
     - For `function_call` and `custom_tool_call` items, if the tool name contains a dot (`<ns>.<subname>`) and corresponds to a grouped container or dotted declaration, the encoder renders `namespace: "<ns>"` and `name: "<subname>"`.
     - `tool_choice` with a dotted name splits into `namespace` and `name` on encode when appropriate.

  5. Cross-face degradation:
     - When an IR request containing container tools is encoded to a face that does not support tool containers (such as Chat Completions or Anthropic Messages), the flattened tools remain available and callable under their flattened names; the dropped container hierarchy records a `degraded` loss per container (Path: `tools[i]`, Field: `type`, Reason: `degraded`, Detail: `tool container grouping was flattened into dotted tool names`).
     - Native tools with `source: "openai"` sent to other faces record an `unsupported-semantic` loss and are omitted per spec/16.

## 8. Loss Catalog

Buckets follow [spec/02 §9](02-loss-policy.md#9-loss-conventions--derived-and-envelope-fields): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| `usage.total_tokens` | exempt (derived) | both | recomputed as input + output on encode |
| `usage` absent | exempt (envelope) | response → IR | IR usage carries zeros |
| `object`, `status` on encode, `output[].id`, `output[].status`, `output[].role`, regenerated response `id` | exempt (envelope) | both | rendering defaults / transport structure |
| empty `annotations` | exempt (envelope) | both | structural part of output_text |
| `metadata` | unmapped-field | request, both directions (single loss each way) | Responses metadata is string-valued with no IR equivalent; the IR metadata map has no Responses field. Dropped symmetrically as one loss per direction. |
| `text.verbosity` | unmapped-field | request → IR | no IR equivalent in v1 |
| `text.format.type` unknown value | unmapped-value | request → IR | unknown text.format type dropped (N-R-14) |
| `reasoning.summary` | unmapped-field | request → IR | summary preference is dropped (N-R-13) |
| `output[i].encrypted_content` | unmapped-field | response → IR | encrypted reasoning content has no IR equivalent (N-R-13) |
| `parallel_tool_calls` | unmapped-field | request → IR | no IR equivalent in v1 |
| `tools[i].strict` | unmapped-field | request → IR | strict mode has no IR equivalent in v1 |
| `tools[i].type` ≠ `function` | unsupported-semantic | request → IR | tool variant has no IR equivalent |
| unknown `input[i].type` | unsupported-semantic | request → IR | item type has no IR equivalent |
| `input[i].role` = `developer` | degraded | request → IR | text is retained in `Request.System` but the developer/system distinction is lost (N-R-1) |
| instruction-only `input` | degraded | request → IR | one empty user message is added to satisfy the IR request shape (N-R-1) |
| content part `type` ∉ {`input_text`,`output_text`,`input_image`} | unsupported-semantic | request → IR | part has no IR equivalent |
| malformed https/data `image_url`, non-image data URL | unsupported-semantic | request → IR | only valid https and `data:image/*;base64` URLs are supported |
| `tool_choice` unsupported forms | unsupported-semantic | both | see N-R-9 |
| `params.stop_sequences` | unmapped-field | IR → request | Responses has no stop-sequences parameter; one loss when non-empty |
| `output[i]` `type` other than `message`, `function_call`, `reasoning` | unsupported-semantic | response → IR | one loss per item |
| output content `type` ≠ `output_text` | unsupported-semantic | response → IR | part has no IR equivalent |
| non-empty message-content `annotations` | unmapped-field | response → IR | annotations have no IR equivalent in v1 |
| non-empty `summary[i].annotations` on a recognized reasoning part | unmapped-field | request/response → IR | one loss at the annotations path; empty annotations remain exempt (N-R-13) |
| reasoning `summary[i].type` unknown or absent | unsupported-semantic | request/response → IR | one whole-part loss at the part path with field `type`; no empty ThinkingBlock or additional empty-summary loss (N-R-13) |
| `incomplete_details.reason` ≠ `max_output_tokens` | unmapped-value | response → IR | maps to IR `other` |
| `error` / `status: "failed"` | unsupported-semantic | response → IR | maps to IR `other`; loss names the error code |
| `stop_sequence` value | unmapped-value | IR → response | Responses status carries no stop-sequence identity |
| `is_error` on tool result | unmapped-field | IR → request | function_call_output items have no error marker |
| IR image both data+URL / bad media type / unusable URL | unsupported-semantic | IR → request | see N-R-4 |
| IR `ImageBlock`/`ToolResultBlock` in assistant input; non-text blocks in tool result | unsupported-semantic | IR → request | see N-R-5, N-R-6 |
| interleaved tool results and ordinary content in one IR user turn | degraded | IR → request | N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved |
| unrecognized non-exempt property on a recognized Responses object | unmapped-field | request/response → IR | one loss per present property; see spec/02 §4 |
| IR native request annotations with no Responses representation | unmapped-field / degraded / unsupported-semantic | IR → request | reported on encode per spec/15 §4 |
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
- OpenAI SDK pinned role/hierarchy references:
  [openai-node EasyInputMessage](https://github.com/openai/openai-node/blob/5f218bf5d8770de5445012b4f1c3b150db983379/src/resources/responses/responses.ts#L969-L987),
  [openai-python EasyInputMessageParam](https://github.com/openai/openai-python/blob/70d473892f7c427a02a3b566add9c86072842e25/src/openai/types/responses/easy_input_message_param.py#L13-L31).
