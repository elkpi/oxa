# 01 — Intermediate Representation

Status: normative. The current spec version is declared in [README.md](README.md).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119 and RFC 8174 when,
and only when, they appear in all capitals, as shown here.

## 1. Role of the IR

The intermediate representation (IR) is the hub of the conversion
architecture (document [00](00-scope-and-architecture.md)). Every converter
either produces IR (face → IR) or consumes it (IR → face); no converter
between two faces exists. This document defines the IR types, their JSON
encoding, and the invariants every implementation MUST enforce and every
vector set MUST respect.

The JSON shape of every type in this document is defined normatively by
[`spec/schema/ir.schema.json`](schema/ir.schema.json). The document and the
schema MUST agree (INV-9).

规范 2.2 的 Custom Tools 扩展由 [13](13-custom-tools.md) 定义：新增
`custom_tool_use`、`custom_tool_result` Block 和 `custom_tool_input_delta`
Delta，扩展 Tool/ToolChoice 与 INV-1、INV-3、INV-4、INV-5。
下文既有类型保持有效；IR 文档 contract 保持 `0.2.0`（兼容读取 `0.1.0`）。

规范 2.3 的专用 Apply Patch 扩展见 [14](14-apply-patch.md)：新增
`apply_patch_tool_use` / `apply_patch_tool_result`、三变体 Operation，以及无名称的专用
Tool/ToolChoice。两类新块都可作为零 Delta 原子响应块；INV-3/4 包含其请求关联。

## 2. Conventions

- **Type and field names.** Types are named in the Go reference style
  (e.g. `ToolUseBlock`, `ToolUseID`); JSON property names are shown in the
  tables and are the cross-language norm. Non-Go implementations use
  idiomatic local names for the same shapes.
- **Optionality.** Pointer types (`*float64`) mean absence is meaningful:
  absent and zero are different states. Optional JSON properties are
  omitted, not set to `null`; `null` MUST NOT appear for any property
  defined in this document.
- **Sealed unions.** Types marked *sealed* have an exhaustive variant set.
  新增变体要求消费者更新穷尽匹配，但依项目版本政策，同一框架下的能力补充使用 minor；
  只有大的架构重构升级 major。版本与兼容范围见 [README](README.md#versioning-policy)。
- **Enums** are closed sets. Converters only ever emit in-set values.
  Inbound face values with no IR equivalent are mapped by the mapping
  documents (10–12), never by extending the set; the general escape hatch
  for stop reasons is `other` plus a loss (see [02](02-loss-policy.md)).
- **Raw JSON as string.** Two fields carry JSON *text* rather than JSON
  values: `tool_use.input` and `input_json_delta.partial_json`. They are
  opaque strings (INV-1). A `ThinkingBlock`'s `signature` is carried
  verbatim with the same discipline: never validated or rewritten. By contrast,
  `tool.input_schema` is carried as a JSON object, verbatim; implementations
  MUST NOT analyze or rewrite it.
  (Rationale: tool arguments arrive from all three faces as text fragments
  or strings and must concatenate without parsing; signatures are provider-specific
  opaque tokens; tool schemas arrive from all three faces as objects.)

## 3. Request-side types

### 3.1 Request

A conversation to be sent to a model, face-neutral.

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| — | `specVersion` | enum `"0.1.0"` \| `"0.2.0"` | yes | document-layer field (§6); not part of in-memory types |
| `Model` | `model` | string | yes | non-empty; opaque, passed through verbatim (document [03](03-model-handling.md)) |
| `System` | `system` | `[]SystemBlock` | no | absent means no system prompt |
| `Messages` | `messages` | `[]Message` | yes, ≥ 1 | INV-2 through INV-4 |
| `Tools` | `tools` | `[]Tool` | no | absent means no tools |
| `ToolChoice` | `tool_choice` | `ToolChoice` | no | absent defers to the target's default |
| `Params` | `params` | `Params` | no | absent means all fields unset |
| `Metadata` | `metadata` | `map[string]string` | no | string-to-string map |

### 3.2 SystemBlock

System prompt content. Sealed; exactly one variant in v1:

| Variant | JSON `type` |
|---------|-------------|
| `TextBlock` | `text` |

### 3.3 Message

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Role` | `role` | enum `user` \| `assistant` | yes | |
| `Content` | `content` | `[]Block` | yes, ≥ 1 | an empty message is not representable; use a `TextBlock` with empty `text` instead |

### 3.4 Block

A content block. Sealed; discriminated on the JSON `type` property:

| Variant | JSON `type` | Purpose |
|---------|-------------|---------|
| `TextBlock` | `text` | a run of text |
| `ImageBlock` | `image` | an image input |
| `DocumentBlock` | `document` | a document input (2.4.8, request-side only; mapping in spec/12 N-AN-16) |
| `ToolUseBlock` | `tool_use` | a tool invocation produced by the model |
| `ToolResultBlock` | `tool_result` | the outcome of a tool invocation, supplied by the caller |
| `ThinkingBlock` | `thinking` | the model's reasoning content, with optional opaque provider signature (since 2.0) |
| `RedactedThinkingBlock` | `redacted_thinking` | Anthropic encrypted thinking replay content block (since 2.4.11) |
| `CustomToolUseBlock` | `custom_tool_use` | 任意文本输入的自定义工具调用（2.2，见 spec/13） |
| `CustomToolResultBlock` | `custom_tool_result` | 自定义工具结果：字符串或受限 text/image 列表（2.2） |
| `ApplyPatchToolUseBlock` | `apply_patch_tool_use` | 专用补丁调用：id、调用状态与强类型 Operation（2.3，spec/14） |
| `ApplyPatchToolResultBlock` | `apply_patch_tool_result` | 专用补丁结果：tool_use_id、结果状态与可选 output（2.3） |

Adding `DocumentBlock` (2.4.8) and `RedactedThinkingBlock` (2.4.11) extends the sealed variant set. Consumers that exhaustively match on block variants MUST extend their matches to handle the new variants; per the versioning policy in [README](README.md) this additive change does not by itself require a major version bump, but the match extension is a REQUIRED migration for exhaustive consumers.

#### TextBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Text` | `text` | string | yes | text content |
| `Citations` | `citations` | `[]TextCitation` | no | optional text citations (since 2.4.11); omitted when empty |

#### TextCitation

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Source` | `source` | string, enum `"anthropic"` | yes | citation provider dialect identifier |
| `Value` | `value` | string — raw JSON text | yes | audited JSON representation of provider citation location |

#### ThinkingBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Thinking` | `thinking` | string | yes | the model's reasoning text; non-empty in nonstream responses, empty in streaming block start |
| `Signature` | `signature` | string | no, non-empty | optional opaque provider signature; carried verbatim (INV-1) |

#### RedactedThinkingBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Data` | `data` | string | yes | opaque provider encrypted thinking replay state (INV-1); never decoded or inspected |

#### ImageBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `MediaType` | `media_type` | string, `image/*` MIME type | iff `Data` present | e.g. `image/png`; MAY be absent when only `URL` is present |
| `Data` | `data` | string, base64 (annotation) | XOR | exactly one of `Data` / `URL` MUST be present |
| `URL` | `url` | string | XOR | image URL |

The `Data` XOR `URL` rule is enforced by the schema: a document carrying
both or neither is invalid. Base64 validity is the producer's
responsibility; consumers treat `data` as an opaque string.

#### DocumentBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `MediaType` | `media_type` | string | iff `Data` present | e.g. `application/pdf` |
| `Data` | `data` | string, base64 (annotation) | XOR | exactly one of `Data` / `URL` / `FileID` MUST be present |
| `URL` | `url` | string | XOR | document URL |
| `FileID` | `file_id` | string | XOR | provider Files-API document reference |

The exactly-one-of-three rule is enforced by the schema: a document carrying none or more than one source form is invalid. Base64 validity is the producer's responsibility; consumers treat `data` as an opaque string (INV-1). Only the base64 form carries `media_type`; `URL` and `FileID` documents MUST NOT carry it. `DocumentBlock` is request-side only: Anthropic Messages responses never contain documents, and the response-side block union excludes this variant. Document-level provider metadata (`title`, `context`, `citations`) travels as `NativeAnnotation`s per [spec/15](15-native-request-options.md) §3.5.

#### ToolUseBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `ID` | `id` | string | yes, non-empty | provider-assigned tool-use identifier |
| `Name` | `name` | string | yes, non-empty | tool name |
| `Input` | `input` | string — raw JSON text | yes | INV-1 |

#### ToolResultBlock

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `ToolUseID` | `tool_use_id` | string | yes, non-empty | MUST match the `id` of the answered `ToolUseBlock` (INV-3) |
| `Content` | `content` | `[]Block` | yes | MAY be empty (a result with no content) |
| `IsError` | `is_error` | bool | no, default `false` | marks a failed tool execution |

### 3.5 Tool

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Name` | `name` | string | iff function/custom/native，非空 | apply_patch MUST 缺失 |
| `Description` | `description` | string | no，限 function/custom | apply_patch MUST 缺失 |
| `Type` | `type` | enum `function` \| `custom` \| `apply_patch` \| `native` | no | 缺省 function；旧 function 编码维持省略；native 见 spec/16 |
| `InputSchema` | `input_schema` | JSON object | iff function | a JSON-Schema-shaped object, carried verbatim; MUST NOT be analyzed or rewritten；custom/apply_patch MUST NOT 携带 |
| `Format` | `format` | `CustomToolFormat` | no，限 custom | text 或 grammar（syntax 为 lark/regex，definition 为不透明字符串）；见 spec/13 |
| `Source` | `source` | string | iff native，非空 | 声明来源；目前恢复 anthropic，见 spec/16 |
| `Definition` | `definition` | string，内容为原始 JSON 对象文本 | iff native | 完整 provider 原生声明，包含与外层一致的 name；native 禁止其他工具配置及 native annotation |

### 3.6 ToolChoice

| Go field | JSON property | Type | Required |
|----------|---------------|------|----------|
| `Mode` | `mode` | enum `auto` \| `any` \| `tool` \| `none` | yes |
| `Name` | `name` | string | named function/custom 的 mode=tool 要求非空；其他情况 MUST 缺失 |
| `ToolType` | `tool_type` | enum `function` \| `custom` \| `apply_patch` \| `native` | no；仅 Mode 为 tool 时允许，缺省 function；apply_patch 禁止 name；native 要求 name，见 spec/16 |

`tool` with `name` selects exactly one tool; the other modes carry no
`name`.

### 3.7 Params

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `Temperature` | `temperature` | `*float64` / number | no | ranges are face concerns (documents 10–12) |
| `TopP` | `top_p` | `*float64` / number | no | |
| `MaxTokens` | `max_tokens` | `*int64` / integer ≥ 1 | no | |
| `StopSequences` | `stop_sequences` | `[]string` | no | |
| `ReasoningEffort` | `reasoning_effort` | enum `minimal` \| `low` \| `medium` \| `high` \| `xhigh` \| `max` | no | absent means unset; unknown inbound values are dropped with unmapped-value (since 2.0; xhigh and max since 2.4) |
| `ResponseFormat` | `response_format` | `*ResponseFormat` | no | structured output formatting preference (since 2.1) |

### 3.7.1 ResponseFormat

Structured output generation preference. Closed union of three variants discriminated by `type`:

| Variant | JSON `type` | Fields | Notes |
|---------|-------------|--------|-------|
| `text` | `text` | `type` | explicit unstructured text generation |
| `json_object` | `json_object` | `type` | unconstrained JSON object output (JSON mode) |
| `json_schema` | `json_schema` | `type`, `name`, `description`, `schema`, `strict` | constrained JSON schema output |

`json_schema` fields:
- `name` (string, required): schema identifier, non-empty.
- `description` (string, optional): description of the schema's purpose.
- `schema` (JSON object, required): JSON Schema object carried verbatim per INV-1; MUST NOT be mutated or re-ordered.
- `strict` (boolean, optional): whether to strictly enforce schema constraints.

When unspecified by caller, `response_format` is omitted from `Params`.

### 3.8 Native Request Options

Request, SystemBlock, Tool, and content blocks in Request may carry optional `native: NativeAnnotation[]` to preserve dialect-specific options across conversion (spec/15, since 2.4). Native annotations MUST NOT appear in Response or EventStream documents.

## 4. Response-side types

### 4.1 Response

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| — | `specVersion` | enum `"0.1.0"` \| `"0.2.0"` | yes | document-layer field (§6) |
| `ID` | `id` | string | yes, non-empty | provider-assigned response identifier |
| `Model` | `model` | string | yes, non-empty | |
| `Content` | `content` | `[]Block` | yes | MAY be empty (an event stream with zero blocks aggregates to this) |
| `StopReason` | `stop_reason` | enum, see below | yes | |
| `StopSequence` | `stop_sequence` | string, non-empty | conditional | MUST be absent unless `StopReason == "stop_sequence"`; then permitted but not required (see note below). Both rules are schema-enforced |
| `Usage` | `usage` | `Usage` | yes | |

`StopReason` enum: `end_turn` | `max_tokens` | `stop_sequence` | `tool_use`
| `refusal` | `other`. This is the union over the three faces; `other` is
the escape hatch for face-native stop reasons with no IR equivalent, and
mapping a value to `other` MUST record a loss (document [02](02-loss-policy.md)).

`stop_sequence` is permitted but not required when `stop_reason` is
`stop_sequence`: Anthropic Messages names the matched sequence when it
stops on one, but Chat Completions reports only `finish_reason: "stop"`
without identifying which stop sequence matched, so a face → IR converter
can know the stop reason without being able to name the sequence. The
same conditional rule applies to `MessageDelta` (§5.1).

In v1, responses contain `TextBlock` and `ToolUseBlock` content; since
2.0, responses may also contain `ThinkingBlock` content, and request
assistant messages MAY carry `ThinkingBlock`s for replay. The type
system permits all block variants, and unused combinations are harmless.

### 4.2 Usage

| Go field | JSON property | Type | Required | Notes |
|----------|---------------|------|----------|-------|
| `InputTokens` | `input_tokens` | int64 / integer ≥ 0 | yes | |
| `OutputTokens` | `output_tokens` | int64 / integer ≥ 0 | yes | |
| `CacheReadInputTokens` | `cache_read_input_tokens` | `*int64` / integer ≥ 0 | no | absent ≠ 0 (since 2.0) |
| `CacheCreationInputTokens` | `cache_creation_input_tokens` | `*int64` / integer ≥ 0 | no | absent ≠ 0 (since 2.0) |
| `InputTokensDetails` | `input_tokens_details` | `*InputTokensDetails` | no | `{cached_tokens: int64 ≥ 0}`; absent ≠ 0 (since 2.0) |
| `OutputTokensDetails` | `output_tokens_details` | `*OutputTokensDetails` | no | `{reasoning_tokens: int64 ≥ 0}`; absent ≠ 0 (since 2.0) |

## 5. Event types

### 5.1 Event

Streaming output. Sealed; exactly six variants in v1, discriminated on the
JSON `type` property:

| Variant | JSON `type` | Fields |
|---------|-------------|--------|
| `MessageStart` | `message_start` | `ID`, `Model` |
| `ContentBlockStart` | `content_block_start` | `Index`, `Block` |
| `ContentBlockDelta` | `content_block_delta` | `Index`, `Delta` |
| `ContentBlockStop` | `content_block_stop` | `Index` |
| `MessageDelta` | `message_delta` | `StopReason`, `StopSequence`, `Usage` |
| `MessageDone` | `message_done` | — |

| Go field | JSON property | Type | Notes |
|----------|---------------|------|-------|
| `ID` | `id` | string | non-empty |
| `Model` | `model` | string | non-empty |
| `Index` | `index` | integer ≥ 0 | block position (INV-6) |
| `Block` | `block` | `Block` | for `ContentBlockStart`; `tool_result` never occurs in responses (see §4.1) |
| `Delta` | `delta` | `Delta` | for `ContentBlockDelta` |
| `StopReason` | `stop_reason` | `Response` stop-reason enum | required on `MessageDelta` |
| `StopSequence` | `stop_sequence` | string, non-empty | conditional on `MessageDelta`; same rule as §4.1, schema-enforced |
| `Usage` | `usage` | `Usage` | required on `MessageDelta`; carries the final totals |

`MessageDelta` occurs exactly once, immediately before `MessageDone`
(INV-5); its `StopReason` and `Usage` are the final values of the stream.

### 5.2 Delta

The delta payload of `ContentBlockDelta`. Sealed; discriminated on the JSON `type` property:

| Variant | JSON `type` | Fields |
|---------|-------------|--------|
| `TextDelta` | `text_delta` | `Text` |
| `InputJSONDelta` | `input_json_delta` | `PartialJSON` |
| `ThinkingDelta` | `thinking_delta` | `Text` |
| `SignatureDelta` | `signature_delta` | `Signature` |
| `CustomToolInputDelta` | `custom_tool_input_delta` | `Text`（JSON text 字段，任意字符串；仅 CustomToolUseBlock 接受） |
| `CitationDelta` | `citation_delta` | `Citation`（TextCitation 对象，自 2.4.11 起） |

| Go field | JSON property | Type | Notes |
|----------|---------------|------|-------|
| `Text` | `text` | string | a text fragment (for `TextDelta` and `ThinkingDelta`) |
| `PartialJSON` | `partial_json` | string — raw JSON text | a fragment of the tool-argument string; MAY be empty; concatenation of all fragments of a block is the block's `input` |
| `Signature` | `signature` | string | provider signature token; at most one per block (for `SignatureDelta`) |
| `Citation` | `citation` | `TextCitation` | provider citation reference item (for `CitationDelta`) |

Delta/block correspondence is fixed: a `TextBlock` admits zero or more
`TextDelta` and `CitationDelta` in order of encounter; a `ToolUseBlock` admits only `InputJSONDelta`; a `ThinkingBlock`
admits `thinking_delta*` followed by at most one `signature_delta`; an
`ImageBlock`, `DocumentBlock`, and `RedactedThinkingBlock` admit no deltas (INV-5)。自 2.2 起，`CustomToolUseBlock` 仅接受 `CustomToolInputDelta`，
其 `text` 片段拼接 MUST 等于该块完整 `input`；不得复用 InputJSONDelta。

### 5.3 EventStream

The JSON document form of a streamed response. In memory, streaming
converters produce and consume sequences of `Event`; in JSON (and in
vectors), the sequence is wrapped once:

| JSON property | Type | Required | Notes |
|---------------|------|----------|-------|
| `specVersion` | enum `"0.1.0"` \| `"0.2.0"` | yes | document-layer field (§6) |
| `events` | `[]Event` | yes, ≥ 3 | the minimum valid stream is `message_start`, `message_delta`, `message_done` |

## 6. JSON documents

An IR **document** is one of: a `Request`, a `Response`, or an
`EventStream`. Every IR document carries a top-level `specVersion`
property, pinned by `const` in the schema to the spec version that defines
it. `specVersion` is a property of the serialized document, not of the
in-memory types. Validating an IR document against
[`spec/schema/ir.schema.json`](schema/ir.schema.json) checks per-document
structure only: the sequence rules INV-2 through INV-6 and INV-8 involve
relationships between elements and are beyond JSON Schema; they are
enforced by converter implementations and by vector checking (INV-9).

The schema's `$id`
(`https://github.com/elkpi/oxa/spec/schema/ir.schema.json`) is an
identifier, not a fetchable location.

## 7. Invariants

All invariants are normative. A converter or document that violates any of
them is invalid.

**INV-1 — Tool input is raw text.** The `input` of a `ToolUseBlock` is raw
JSON text. Implementations MUST NOT parse and re-serialize it on any
conversion path; it is copied as an opaque string. Comparison of
`tool_use.input` is by exact string equality; the JSON inside it is never
structurally compared (INV-7 applies to the IR tree, and `input` is a
string leaf). The same rule applies to `input_json_delta.partial_json` and
to `thinking.signature` / `signature_delta.signature`。自 2.2 起，同样的不透明字符串纪律
适用于 `custom_tool_use.input` 和 `custom_tool_input_delta.text`；这两个字段的内容
是任意文本，不要求为 JSON，也不得尝试按 JSON 解析。

**INV-2 — First message is user.** In a `Request`, `messages[0].role` MUST
be `user`.

**INV-3 — Tool use is answered.** For every assistant message containing
one or more `ToolUseBlock`s, the immediately following message MUST exist,
MUST have role `user`, and MUST contain exactly one `ToolResultBlock` per
`ToolUseBlock`, with `tool_use_id` values matching one-to-one, in the same
relative order. That user message MAY also contain other block types.
Conversely, every `ToolResultBlock` MUST appear in the user message
immediately following the assistant message whose `ToolUseBlock` it
answers; orphan tool results are invalid.

**INV-4 — Tool results merge into one message.** All `ToolResultBlock`s
answering one assistant message MUST be carried in a single user message.
When a source face presents them as separate messages (for example,
consecutive Chat Completions `tool` messages), the face → IR converter
MUST merge them into one IR user message; IR → face converters MUST NOT
expect them split。

自 2.2 起，INV-3/4 同时包含 CustomToolUseBlock/CustomToolResultBlock；function 与 custom
可以混合，但 call_id、调用类别及相对顺序必须一一匹配。详见 spec/13 §2。

**INV-5 — Event grammar.** An IR event stream MUST have exactly this
shape:

```
message_start
( content_block_start content_block_delta* content_block_stop )*
message_delta
message_done
```

Exactly one `message_start` first, exactly one `message_delta` immediately
followed by exactly one `message_done` last. Each block: exactly one
`content_block_start`, zero or more `content_block_delta`, exactly one
`content_block_stop`. While a block is open, only `content_block_delta`
events for that block MAY occur, and the delta type MUST match the block
type (§5.2). Events outside this grammar (gap events) MUST NOT occur.

**INV-6 — Index discipline.** The first `content_block_start` of a stream
carries `index` 0; every subsequent `content_block_start` carries exactly
one more than its predecessor (block indexes are contiguous and strictly
increasing). Every `content_block_delta` and `content_block_stop` MUST
carry the index of the currently open block; an event referencing any
other index is invalid.

**INV-7 — Structural equality.** IR JSON documents are UTF-8. Two IR JSON
documents are structurally equal if and only if their value trees are
equal, ignoring object key order. Integer values MUST stay integers: `1`
and `1.0` are not structurally equal. String values compare by exact
code-point sequence — including `tool_use.input` and
`input_json_delta.partial_json` (INV-1). (Note: JSON Schema 2020-12
treats `1` and `1.0` as the same number, so both validate against
`integer`-typed properties; IR comparison is deliberately stricter, so a
converter that coerces an integer to a floating-point form fails vector
comparison even though its output remains schema-valid.)

**INV-8 — Total order, element-wise comparison.** An IR event stream is a
totally ordered sequence. Two event streams are equal if and only if they
have the same length and their events are pairwise structurally equal in
order. Golden vectors compare expected and actual event streams
element-wise in this order. (This is syntactic IR equality; the weaker
face-level notion is stream equivalence, document [00, §3](00-scope-and-architecture.md).)

**INV-9 — Schema agreement.** The JSON shape of every type in this
document is defined by `spec/schema/ir.schema.json`. This document and the
schema MUST agree; disagreement is a spec bug. CI checks the agreement
from milestone M2 onward.

## 8. Examples

All examples validate against `spec/schema/ir.schema.json`.

A request with a completed tool round-trip:

```json
{
  "specVersion": "0.1.0",
  "model": "claude-sonnet-4-5",
  "system": [{ "type": "text", "text": "You are a concise assistant." }],
  "messages": [
    { "role": "user", "content": [
      { "type": "text", "text": "What is the weather in Paris?" }
    ]},
    { "role": "assistant", "content": [
      { "type": "text", "text": "Let me check." },
      { "type": "tool_use", "id": "toolu_01", "name": "get_weather",
        "input": "{\"city\":\"Paris\"}" }
    ]},
    { "role": "user", "content": [
      { "type": "tool_result", "tool_use_id": "toolu_01", "content": [
        { "type": "text", "text": "18 C, clear" }
      ] }
    ]}
  ],
  "tools": [
    { "name": "get_weather", "description": "Current weather for a city",
      "input_schema": { "type": "object",
        "properties": { "city": { "type": "string" } },
        "required": ["city"] } }
  ],
  "tool_choice": { "mode": "auto" },
  "params": { "temperature": 0.7, "max_tokens": 1024 }
}
```

A response:

```json
{
  "specVersion": "0.1.0",
  "id": "msg_017Y2hvcv",
  "model": "claude-sonnet-4-5",
  "content": [
    { "type": "text", "text": "It is 18 C and clear in Paris." }
  ],
  "stop_reason": "end_turn",
  "usage": { "input_tokens": 120, "output_tokens": 12 }
}
```

An event stream for the same response (illustrative chunking):

```json
{
  "specVersion": "0.1.0",
  "events": [
    { "type": "message_start", "id": "msg_017Y2hvcv", "model": "claude-sonnet-4-5" },
    { "type": "content_block_start", "index": 0,
      "block": { "type": "text", "text": "" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": "It is 18 C" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": " and clear in Paris." } },
    { "type": "content_block_stop", "index": 0 },
    { "type": "message_delta", "stop_reason": "end_turn",
      "usage": { "input_tokens": 120, "output_tokens": 12 } },
    { "type": "message_done" }
  ]
}
```
