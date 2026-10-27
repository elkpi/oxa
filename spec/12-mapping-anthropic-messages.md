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
array (`text`, `image`, `document`, `tool_use`, `tool_result`). Mid-conversation
system messages (`role: "system"` in `messages[]`) are supported and
folded into the request-level `system` list with a `degraded` loss (N-AN-15).
Conversion is near-identity for the supported subset; Anthropic-only
annotations (`cache_control`, `disable_parallel_tool_use`, `metadata.user_id`)
are dropped with losses rather than errors (spec/02). Errors are reserved
for structural violations: a missing or non-positive `max_tokens`,
unknown `role` values, invalid `role: "system"` placement or content,
missing or malformed content, missing `tool_use` id/name/input, missing
`tool_result.tool_use_id`, a `tool_use.input`/`input_schema` that is not
a JSON object, a missing `stop_reason`, or a request with no messages.

## 2. Wire Objects

Reference snapshot (retrieved 2026-08-28):

- API reference: <https://docs.claude.com/en/api/messages>
- Documentation ("Set max_tokens", which recommends 4096 and uses it in
  the docs' own examples): <https://docs.claude.com/en/docs>

The wire objects in scope are:

- **Request**: `model`, `system`, `messages[]`, `max_tokens`
  (required), `temperature`, `top_p`, `top_k`, `stop_sequences`, `metadata`,
  `tools[]`, `tool_choice`.
- **Message**: `role` (`user` | `assistant` | mid-conversation `system`),
  `content` (string or block array).
- **Block**: `text` (`text`, `citations`), `image` (`source`), `document`
  (`source`, `title`, `context`, `citations`; N-AN-16), `thinking`
  (`thinking`, `signature`), `redacted_thinking` (`data`; N-AN-17), `tool_use`
  (`id`, `name`, `input`), `tool_result` (`tool_use_id`, `content`,
  `is_error`); every block may carry a `cache_control` annotation.
- **Source**: `base64` (`media_type`, `data`) or `url` (`url`);
  document sources additionally `file` (`file_id`, N-AN-16).
- **Tool**: 普通工具的 `name`、`description`、`input_schema`（JSON Schema）；带非空且非 function/custom 的 `type` 的命名原生声明按 [spec/16](16-native-tool-declarations.md) 整体保留，不要求 `input_schema`。
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
| `messages[role=system]` | `Request.System[]` | mid-conversation system message appended to `Request.System` with `degraded` loss; text-only (N-AN-15) |
| `messages[].content` | `Message.Content` blocks (or one `TextBlock` for string content) | N-AN-5 |
| `max_tokens` | `Params.MaxTokens` | required and MUST be positive on decode; defaulted on encode (N-AN-2) |
| `temperature` | `Params.Temperature` | 1:1 |
| `top_p` | `Params.TopP` | 1:1 |
| `top_k` | `Request.Native` (`kind: "top_k"`) | N-AN-19; non-negative safe integer; preserved as native option in IR (spec/15) |
| `stop_sequences` | `Params.StopSequences` | 1:1 (N-AN-7) |
| `thinking` (`type: "enabled"`, `budget_tokens`) | `Params.ReasoningEffort` + `NativeAnnotation` | mapped via budget table when no explicit effort; preserved as native options in IR (N-AN-11, N-AN-14) |
| `thinking` (`type: "adaptive"` / `"disabled"`) | `NativeAnnotation` | retained in IR; no loss on decode (N-AN-14) |
| `thinking` (`type: "between_tools"`) | `NativeAnnotation` | retained in IR; no loss on decode (N-AN-19, spec/15) |
| `thinking.display` | `NativeAnnotation` | retained in IR; no loss on decode (N-AN-14) |
| `output_config.effort` | `Params.ReasoningEffort` + `NativeAnnotation` | 1:1 effort mapping (xhigh/max supported); preserved as native option in IR (N-AN-14) |
| `cache_control` (top-level, system, content, tools, tool_result) | `NativeAnnotation` | retained on respective IR host; no loss on decode (N-AN-14) |
| `context_management` | `NativeAnnotation` | retained on IR Request; no loss on decode (N-AN-14) |
| `tools[].{name,description,input_schema,strict}` | `Tools[].{Name,Description,InputSchema,Strict}` | 普通 function 工具双向映射；strict 区分缺省、false、true；schema bytes carried verbatim (INV-1); MUST be a JSON object |
| `tools[].defer_loading` | `Tools[].Native` (`kind: "defer_loading"`) | true 保留为 NativeAnnotation，无 decode loss；跨面降级为 degraded 损失（N-NAT-2/3） |
| `tools[].eager_input_streaming` | `Tools[].Native` (`kind: "eager_input_streaming"`) | true/false 保留为 NativeAnnotation，无 decode loss；跨面 true 降级为 degraded 损失，false 零损（N-NAT-2/3，规范 2.4.10） |
| `tools[]` 原生命名声明 | `Tools[type=native].{Name,Source,Definition}` | N-NT-1/2；完整原始对象文本；含 Advisor 及客户端内置工具；不推断执行位置 |
| `tool_choice` | `ToolChoice` (modes `auto`/`any`/`none`/`tool` map 1:1 by name) | N-AN-6 |
| `tool_choice.disable_parallel_tool_use` | `Request.Native` (`kind: "disable_parallel_tool_use"`) | N-AN-19; boolean; preserved as native option in IR (spec/15) |
| `metadata` | — | single unmapped-field loss, both directions (N-AN-8) |
| — | `Params.ResponseFormat` | N-AN-12; dropped on encode with unmapped-field loss (since 2.1) |

For ordinary function tools, a present `strict` MUST be a boolean; `null` and
other types are structural errors, including when the input schema matches a
custom wrapper. Native tool declarations remain opaque: their provider-specific
`strict` field is not subject to this rule.

原生声明及其 named tool_choice 的保留和跨面省略遵守 [spec/16](16-native-tool-declarations.md)。本扩展不增加 Advisor 响应/流式/历史块支持。

On encode (IR → AN), `Request.System` renders as a system block array
of `type: "text"` blocks; message content renders as block arrays,
with the single exception of the exact string shorthand (N-AN-3);
`max_tokens` is always emitted, defaulting to 4096 with a `degraded`
loss when the IR carries no `Params.MaxTokens` (N-AN-2); other params
render 1:1 with their omitempty semantics.

## 4. Content Block Mapping

| AN block | IR block | Notes |
|---|---|---|
| `{type: "text", text, citations}` | `TextBlock{Text, Citations}` | N-AN-18 (citations since 2.4.11) |
| `{type: "thinking", thinking, signature}` | `ThinkingBlock{Thinking, Signature}` | 1:1; signature carried verbatim (N-AN-11, since 2.0) |
| `{type: "redacted_thinking", data}` | `RedactedThinkingBlock{Data}` | 1:1; data carried verbatim (N-AN-17, since 2.4.11) |
| `{type: "image", source: {type: "base64", media_type, data}}` | `ImageBlock{MediaType, Data}` | |
| `{type: "image", source: {type: "url", url}}` | `ImageBlock{URL}` | |
| `{type: "document", source: {type: "base64", media_type, data}}` | `DocumentBlock{MediaType, Data}` | N-AN-16 |
| `{type: "document", source: {type: "url", url}}` | `DocumentBlock{URL}` | N-AN-16 |
| `{type: "document", source: {type: "file", file_id}}` | `DocumentBlock{FileID}` | N-AN-16 |
| `{type: "tool_use", id, name, input}` | `ToolUseBlock{ID, Name, Input}` | raw input, N-AN-4 |
| `{type: "tool_result", tool_use_id, content, is_error}` | `ToolResultBlock{ToolUseID, Content, IsError}` | content is string or block array, decoded recursively by N-AN-5 |
| other block types | — | one whole-block unsupported-semantic loss (N-AN-9) |
| `source.type` ∉ {`base64`, `url`} | — | unsupported-semantic loss |
| document `source.type` ∉ {`base64`, `url`, `file`} | — | one whole-block unsupported-semantic loss (N-AN-16) |

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
Document blocks MUST carry exactly one of data, URL, or file_id; data
requires `media_type`, and URL/file_id documents MUST NOT carry
`media_type` (N-AN-16). Violations are unsupported-semantic losses.
Tool-result content renders as an array of `text`/`image`/`document`
blocks; other IR block types inside a tool result are
unsupported-semantic losses.

## 5. Response Field Mapping

| AN field | IR destination | Notes |
|---|---|---|
| `content[]` blocks | `Response.Content` blocks | N-AN-5, same block table as §4 |
| `stop_reason` | `Response.StopReason` | `end_turn`→`end_turn`, `max_tokens`→`max_tokens`, `stop_sequence`→`stop_sequence`, `tool_use`→`tool_use`, `refusal`→`refusal`; unknown → `other` + `unmapped-value` loss; missing → error |
| `stop_sequence` | `Response.StopSequence` | carried verbatim; emitted on encode when the stop reason is `stop_sequence` |
| `id` | `Response.ID` | |
| `model` | `Response.Model` | via model map |
| `usage.input_tokens` / `usage.output_tokens` | `Usage.InputTokens` / `Usage.OutputTokens` | 1:1 |
| `usage.cache_creation_input_tokens` | `Usage.CacheCreationInputTokens` | 1:1 (since 2.0) |
| `usage.cache_read_input_tokens` | `Usage.CacheReadInputTokens` | 1:1 (since 2.0) |
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
  unknown image/document source types are dropped as
  unsupported-semantic losses (N-AN-9).
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
- **N-AN-11 (thinking blocks, signatures, and budget mapping).**
  `thinking` content blocks map 1:1 including `signature` verbatim;
  `thinking_delta` / `signature_delta` map directly. Request
  `thinking.budget_tokens` ↔ `Params.ReasoningEffort` via:
  minimal → 1024, low → 2048, medium → 8192, high → 16384 (encode),
  and decode budget ≤ 2048 → low, ≤ 8192 → medium, else → high, each with
  a `degraded` loss `{path:"params"/"thinking", field:"reasoning_effort"|"budget_tokens", reason:"degraded", detail:"budget approximated"}`.
  An unsigned ThinkingBlock encoded to either a request or response renders as
  a thinking block WITHOUT signature plus a `degraded` loss
  `{path:"content[i]", field:"signature", reason:"degraded", detail:"unsigned thinking block; Anthropic may reject on replay"}`. The replay risk is especially relevant to requests.
- **N-AN-12 (response_format dropped on encode).** Anthropic Messages API has
  no native request-level `response_format` parameter. When encoding an IR
  request carrying `Params.ResponseFormat`, converters MUST NOT synthesize
  tools or mutate `tools`/`tool_choice`; instead, exactly one `unmapped-field`
  loss is recorded:
  `{path: "params.response_format", field: "response_format", reason: "unmapped-field", detail: "Anthropic Messages has no native response_format request parameter; structured output preference is dropped."}`.
- **N-AN-13 (custom tool structured wrapper mapping).** Anthropic Messages supports
  Custom Tools via a canonical single-property wrapper (spec/13, since 2.3.1).
  On encode, an IR `Tool{Type: "custom", Name, Description, Format}` renders as a
  tool with `input_schema: {"type":"object","properties":{"input":{"type":"string"}},"required":["input"],"additionalProperties":false}`.
  When the IR tool carries a `format` with `type: "grammar"`, an `unsupported-semantic`
  loss is recorded on `tools[i].format` because the wrapper cannot enforce grammar constraints.
  An IR `CustomToolUseBlock` renders as a `tool_use` content block with `input: {"input": "<raw_text>"}`.
  An IR `CustomToolResultBlock` renders as a `tool_result` content block supporting string content
  or arrays of text and image content blocks.
  A declaration carrying `strict` MUST decode as a function tool even when its schema matches the custom wrapper; otherwise the explicit function-only field would be lost. This rule does not alter or validate the schema.
  On decode, a `tool_use` is decoded to `CustomToolUseBlock` when its declaration in the request matches
  a custom tool, or when its input matches the single-field `{"input": "<string>"}` shape and the tool
  was NOT declared as a standard function tool in the same request. If the tool was declared as a function
  tool, it MUST NOT be promoted to `CustomToolUseBlock`.
  In streaming (IR → Messages), `CustomToolInputDelta` fragments are accumulated, and at `content_block_stop`
  a single canonical `input_json_delta` containing the complete wrapped JSON `{"input":"<concatenated>"}`
  is emitted, guaranteeing that the stream's accumulated `partial_json` remains a single valid JSON document.
- **N-AN-14 (native request options round-trip, spec/15).** Requests preserve recognized Anthropic
  request-level and block-level options (`thinking.type`, `thinking.budget_tokens`, `thinking.display`,
  `output_config.effort`, `cache_control`, `context_management.edits`) as `NativeAnnotation`s in the IR.
  No loss is emitted on Decode when these options are preserved. On Encode (IR → Messages), these options
  are restored to their original wire locations with full fidelity, subject to the explicit-edit priority
  rules of spec/15 §5.
- **N-AN-15 (mid-conversation system messages).** Anthropic Messages allows
  `role: "system"` messages inside `messages[]` as a mid-conversation operator channel.
  On decode (AN → IR):
  - **Placement validation.** A `role: "system"` message MUST NOT appear at `messages[0]`
    (top-level `system` MUST be used for the initial prompt; violation is a structural error).
    A mid-conversation system message (or run of consecutive system messages) MUST follow
    a `user` turn, and MUST either be the last entry in `messages` or be immediately followed
    by an `assistant` turn. A system message followed directly by a `user` turn is a
    structural error. The documented server-tool exception (an `assistant` turn ending in a
    server tool result) is NOT recognized in this version: any `assistant` predecessor is
    rejected as a structural error; recognizing it is deferred to a follow-up release.
  - **Content validation.** Content MUST be a string or an array of blocks where every block
    is `type: "text"`. Non-text block types (e.g. `image`, `tool_use`, `tool_result`) are
    structural errors. An empty block array carries no text: no IR system blocks are
    produced and only the degraded role loss is recorded (the API's effort-only message
    shape; any extra fields on such a message are audited as unmapped-field losses per
    spec/02).
  - **Destination.** Extracted text blocks are appended in encounter order to `Request.System[]`,
    following any top-level `system` blocks and preceding mid-conversation system messages.
    If a text block carries a valid `cache_control` annotation, it is preserved as a
    `NativeAnnotation` on that `SystemBlock` per spec/15.
  - **Loss reporting.** Because IR `Request.System` is request-level and cannot represent
    the mid-conversation position or activation point, each decoded mid-conversation
    system message records exactly one `degraded` loss:
    `{ "path": "messages[i].role", "field": "role", "reason": "degraded", "detail": "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved." }`.
    The `degraded` loss is recorded before any losses derived from that message's
    `content` (spec/02 encounter order: `role` precedes `content` in the wire object;
    for typed in-memory input this defines the deterministic order).
  - **Encode (IR → Messages).** `Request.System` renders in top-level `system` (N-AN-1);
    IR conversation messages retain only `user` and `assistant` roles.

- **N-AN-16 (document blocks, since 2.4.8).** Anthropic `document`
  content blocks map to `DocumentBlock` (request-side only; a
  `document` block inside a response position is out of contract).
  - **Sources.** `source.type` `base64` maps to `MediaType`+`Data`;
    `url` maps to `URL`; `file` maps to `FileID`. The source
    discriminator is implicit in the IR: the field combination IS the
    source form. Other `source.type` values (e.g. plain-text/content
    sources) are out of contract for this version and drop the whole
    block as one unsupported-semantic loss (N-AN-9 matrix); they are
    candidates for a future additive extension.
  - **Validation.** Structural errors (conversion fails): missing
    `source` or missing/empty `source.type`; base64 document missing `media_type` or `data`; url
    document missing `url`; file document missing `file_id`; fields from
    another source form mixed into the selected source; a non-string
    source discriminator; `title`
    or `context` not a string; `citations` present but not a JSON
    object or carrying a non-boolean `enabled`. Malformed values are
    never silently repaired.
  - **Metadata (spec/15 §3.5).** `title` and `context` (strings) and
    `citations` (object with optional boolean `enabled`, carried as
    canonical compact JSON per spec/15 §3.5) are preserved
    as `NativeAnnotation`s (`document_title`, `document_context`,
    `document_citations`) on the block; decode emits no loss for them.
    `cache_control` on a document follows the common block annotation
    path (N-AN-1/N-AN-14).
  - **Encode.** Restores the document block byte-faithfully for the
    supported forms, including the metadata annotations, with zero
    loss when the block is unchanged. An IR document carrying none or
    more than one of data/URL/file_id, data without `media_type`, or
    URL/file_id with `media_type` is an unsupported-semantic loss
    (field `document`).
  - **Cross-face.** Faces without a document contract (OpenAI Chat
    Completions, OpenAI Responses) drop the whole block as exactly one
    unsupported-semantic loss; annotations inside the dropped block
    are part of it and MUST NOT add further losses (N-AN-9 symmetry).

- **N-AN-17 (redacted thinking blocks, since 2.4.11).** Anthropic assistant-history and
  response blocks of type `redacted_thinking` map 1:1 to `RedactedThinkingBlock{Data}`.
  - **Payload preservation.** `data` is opaque provider encrypted thinking replay state (INV-1).
    Decoders and encoders MUST copy it without parsing, decoding, normalizing, logging, or
    inspecting its contents. Empty string is preserved as an explicit string.
  - **Validation.** Missing `data`, null, or non-string `data` is a structural error.
    In request history, `redacted_thinking` is permitted ONLY directly under assistant messages;
    placement in `user` messages or `tool_result.content` is a structural error.
  - **Cross-face.** Target faces without an encrypted reasoning replay representation
    (OpenAI Chat Completions, OpenAI Responses) omit the block and emit exactly one
    `unsupported-semantic` loss with detail `anthropic encrypted thinking replay state; the target face cannot preserve this block`.
    The opaque payload MUST NOT be disclosed in loss details or mapped to text.

- **N-AN-18 (text citations, since 2.4.11).** Anthropic text content blocks in responses and
  assistant request history carry an optional `citations` array mapping to `TextBlock.Citations`.
  - **Location variants.** Five location types are supported: `char_location`, `page_location`,
    `content_block_location`, `web_search_result_location`, and `search_result_location`.
    Each valid citation is audited and retained in `TextCitation{Source: "anthropic", Value: "<compact JSON>"}`.
  - **Validation.** Missing, null, or empty citations are normalized to an empty citation list (omitted in canonical IR).
    In user messages or tool results, a non-empty citations array is a structural error.
    An unknown citation type in wire input drops that citation with an `unsupported-semantic` loss at its index;
    unknown properties on a known citation type produce an `unmapped-field` loss.
    In canonical IR or typed IR inputs, an unknown citation source or malformed citation value is a structural error.
  - **History replay.** When response IR carrying `file_id` is replayed as assistant history,
    request encode strips `file_id`; a non-null `file_id` records an `unmapped-field` loss.
  - **Cross-face.** Target faces without citation replay representation omit the citation list,
    preserve the text, and emit exactly one `unsupported-semantic` loss per text block with detail
    `anthropic text citations; the target face cannot preserve source locations and replay metadata`.

- **N-AN-19 (thinking between_tools, top_k, and tool choice parallel control, since 2.4.11).**
  - **between_tools.** `thinking.type = "between_tools"` is preserved as `NativeAnnotation`
    (`kind: "thinking_mode"`, `value: "\"between_tools\""`). It carries no budget or display.
    Specifying `budget_tokens` or `display` alongside `between_tools` is a structural error.
  - **top_k.** Top-level `top_k` is preserved as `NativeAnnotation` (`kind: "top_k"`).
    It MUST be a non-negative safe integer (`0` is preserved). Negative, non-integer, or out-of-range values are structural errors.
  - **disable_parallel_tool_use.** On `tool_choice` (`auto`, `any`, `tool`), `disable_parallel_tool_use` is
    preserved as `NativeAnnotation` (`kind: "disable_parallel_tool_use"`, `value: "true"` or `"false"`).
    Presence on `tool_choice: "none"` is a structural error.
  - **Cross-face.** These native request options are omitted at OpenAI faces and report `unmapped-field` losses
    per spec/15 §5.

## 8. Loss Catalog

Buckets follow [spec/02 §9](02-loss-policy.md#9-loss-conventions--derived-and-envelope-fields): DERIVED and
ENVELOPE fields are exempt; everything else MUST record a loss.

| Field (path context) | Bucket / reason | Direction | Detail |
|---|---|---|---|
| response `type`, `role` | exempt (envelope) | both | N-AN-10 |
| `cache_control` on an unsupported/dropped block | unsupported-semantic | request/response → IR | part of the whole dropped block (N-AN-9) |
| `params.response_format` | unmapped-field | IR → request | Anthropic Messages has no native response_format request parameter (N-AN-12) |
| `thinking.budget_tokens` / `params.reasoning_effort` | degraded | both | effort mapped to/from discrete budget values with approximation when no explicit effort is present (N-AN-11, N-AN-14) |
| `content[i].signature` (unsigned thinking block) | degraded | IR → request/response | unsigned thinking block has no provider signature; request replay may be rejected (N-AN-11) |
| unknown block type or unknown image/document source type (with any annotations it carries) | unsupported-semantic | both | one whole-block loss, N-AN-9/N-AN-16 |
| unknown document `source.type` | unsupported-semantic | request → IR | one whole-block loss, N-AN-16 |
| IR document with none/multiple of data/url/file_id, data without `media_type`, url or file_id with `media_type` | unsupported-semantic | IR → request | §4, N-AN-16 |
| `metadata` | unmapped-field | request, both directions (single loss each way) | N-AN-8 |
| `messages[i].role` (mid-conversation system message) | degraded | request → IR | Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved. (N-AN-15) |
| `redacted_thinking` block (cross-face) | unsupported-semantic | IR → target face | anthropic encrypted thinking replay state; the target face cannot preserve this block (N-AN-17) |
| `text.citations` (cross-face) | unsupported-semantic | IR → target face | anthropic text citations; the target face cannot preserve source locations and replay metadata (N-AN-18) |
| `text.citations[k].file_id` (request encode replay) | unmapped-field | IR → request | response citation file_id has no request-side representation (N-AN-18) |
| `unknown citation type` | unsupported-semantic | request/response → IR | unsupported citation type; dropped at leaf index (N-AN-18) |
| `max_tokens` default on encode | degraded | IR → request | N-AN-2: default 4096 applied when IR lacks `Params.MaxTokens` |
| IR image with both/neither of data and URL, data without `media_type`, URL with `media_type` | unsupported-semantic | IR → request | §4 |
| IR block type with no Messages equivalent in its position | unsupported-semantic | IR → request | N-AN-5 |
| IR tool-choice mode with no Messages equivalent | unsupported-semantic | IR → request | N-AN-6 |
| unknown `stop_reason` value | unmapped-value | response → IR | maps to IR `other` |
| unrecognized non-exempt property on a recognized Messages object | unmapped-field | request/response → IR | one loss per present property; see spec/02 §4; unknown block types remain one whole-block loss (N-AN-9) |
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
