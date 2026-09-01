# 20 — Streaming Semantics

Status: normative. Spec version 0.1.0.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119 and RFC 8174 when,
and only when, they appear in all capitals, as shown here.

## 1. Scope

This document defines the common semantics for **parsed provider event
objects** and the face-to-IR streaming converters. It applies to the M6
text-stream profile of OpenAI Chat Completions (CC), OpenAI Responses (RE),
and Anthropic Messages (AN). The IR event types, JSON form, and invariants
are defined by [01](01-intermediate-representation.md), especially INV-1 and
INV-5 through INV-8; loss records are defined by [02](02-loss-policy.md).

This is not a replacement wire snapshot. The non-streaming snapshots and
field mappings remain in [10](10-mapping-openai-chat-completions.md),
[11](11-mapping-openai-responses.md), and
[12](12-mapping-anthropic-messages.md). A provider-specific streaming adapter
MUST first decode its native payload into that face's typed event object, then
apply this document's mapping. It MUST NOT treat a raw SSE byte stream as an
IR event stream.

The M6 profile supports assistant text output only. Streaming tool arguments,
`input_json_delta`, function-call output items, and tool-call aggregation are
defined by the M7 profile (§10). M6 does not loosen the IR type system in
order to represent those semantics.

## 2. Shared stream contract

### 2.1 Event-object interface

A face → IR decoder accepts one typed native event at a time with `Feed` and
returns zero or more completed IR events. An IR → face encoder accepts one IR
event at a time with `Apply` and returns zero or more typed native events plus
losses. The public API names in the Go reference are illustrative; every
implementation MUST expose equivalent incremental behavior.

A decoder's `Losses()` result is cumulative in source encounter order. It
MUST NOT inject loss records into the IR event sequence (N-S-4). Model
mapping at native message/response start and on emitted wire envelopes follows
[03](03-model-handling.md).

### 2.2 IR grammar and terminal completion — N-S-1

Every emitted or accepted IR stream MUST satisfy INV-5 exactly:

```text
message_start
( content_block_start content_block_delta* content_block_stop )*
message_delta
message_done
```

`MessageDelta` occurs once and carries the final stop reason and final usage;
`MessageDone` follows it immediately. A decoder MUST NOT invent a terminal
stop reason or usage value when the native terminal state is absent. An
encoder MUST reject an IR event that is out of this order or that uses a
block/delta combination not allowed by [01, §5.2](01-intermediate-representation.md#52-delta).

The native terminal boundary is face-specific:

| Face | Native completion observation | IR terminal emission |
|------|-------------------------------|----------------------|
| CC | a `finish_reason` is seen; a usage-only chunk MAY follow | `Flush` emits any open block stop, then `MessageDelta` and `MessageDone` |
| RE | `response.completed`, `response.incomplete`, or `response.failed` | that terminal `Feed` call emits `MessageDelta` and `MessageDone`; `Flush` confirms it |
| AN | `message_delta` is followed by `message_stop` | the `message_stop` `Feed` call emits `MessageDelta` and `MessageDone`; `Flush` confirms it |

`Flush` MUST be called exactly once after the native stream ends. A second
`Flush`, a feed after flush, or a native stream that ends without its
face-specific terminal condition is a structural error. For CC, final usage
may arrive before or with the finish chunk; the last observed usage is final.
Absent final usage is the zero `Usage` envelope default and records no loss.

### 2.3 Index ownership and compaction — N-S-2

IR block indexes are owned by the IR, not by a provider. Every emitted IR
`ContentBlockStart` MUST receive the next contiguous index starting at zero;
its `ContentBlockDelta` and `ContentBlockStop` MUST reuse that same index
(INV-6).

A native index MAY instead identify an output item, content part, or native
block lifecycle. Decoders MUST retain native indexes and identifiers long
enough to validate native ordering and match descendants, but MUST NOT leak a
skipped native unit's index into the emitted IR stream. Thus, if native block
0 is unsupported and dropped while native block 1 is retained, the retained
IR block has index 0. If a retained second native block follows it, that IR
block has index 1.

An encoder receiving an IR stream MUST validate INV-6 before assigning native
indexes. It MAY use a face-specific synthesized native index or item ID only
where the native protocol lacks an IR field; generated envelope values are
exempt from losses.

### 2.4 Structural errors and loss containment — N-S-3

The following are structural errors, rather than losses: a nil required event
or payload; duplicate start/terminal events; a known lifecycle event before
native start or after terminal completion; nested native lifecycle units; a
native lifecycle unit that remains unclosed at a boundary where its face
requires closure; mismatched native indexes or item identifiers; and any IR
input that violates N-S-1 or N-S-2. A CC `finish_reason` is not such a
boundary: its single flattened text block remains open until `Flush` closes
it under N-S-5.

An unsupported native semantic is not an error. A decoder MUST record an
`unsupported-semantic` loss for the unsupported native unit or unknown event
and otherwise preserve the surrounding valid lifecycle. If the unsupported
unit has a native start/delta/stop or item/part lifecycle, its matching
native descendants and completion events MUST be absorbed without recording
duplicate losses. Their native identities and indexes still MUST be checked.
A descendant whose supplied identity or relevant index does not match the
active lifecycle is structural.

This rule applies per unsupported unit, not globally: two distinct unsupported
items or blocks produce two losses. An unknown standalone event whose native
protocol gives no unit identity produces one loss per event. Losses remain
outside the IR stream as required by [02, §5](02-loss-policy.md#5-streaming).

### 2.5 Text profile and wire JSON — N-S-4

M6 emits and accepts `TextBlock` / `TextDelta` only. An M6 encoder MUST
reject `ToolUseBlock`, `InputJSONDelta`, and other non-text stream forms as
outside the profile; M6 decoders report the corresponding unsupported native
semantics as losses and keep the valid text lifecycle intact.

Typed native event JSON MUST preserve each protocol-required zero index. In
particular, an index value of `0` MUST be serialized whenever a native event
requires that index; `omitempty`-style omission is not valid for such an
event. Index properties MUST NOT be added to native event types that do not
use them. Protocol envelope defaults have their native form: AN
`message_start` renders `stop_reason: null`, while RE and CC use their own
response/chunk envelopes. Optional native sequence numbers are envelope data
and have no IR equivalent.

## 3. Chat Completions profile — N-S-5

CC chunks describe one flattened assistant text channel, not native content
block lifecycles. M6 therefore has a single-text-block profile:

1. The first choice-bearing chunk's first choice element (`choices[0]`)
   emits `MessageStart` and
   `ContentBlockStart{index: 0, TextBlock{Text: ""}}`. A usage-only chunk
   with no choices does not start the IR stream.
2. Each `choices[0].delta.content` value emits a `TextDelta` on index 0.
3. `choices[0].finish_reason` supplies the final stop reason but emits no IR
   terminal event until `Flush`.
4. `Flush` emits `ContentBlockStop{index: 0}`, `MessageDelta`, and
   `MessageDone` after a finish reason is present.

Only the first choice element participates in M6; additional choice elements,
repeated chunk metadata, and chunk log-probability data are envelope data. A
role annotation after stream start, or a missing finish reason at `Flush`, is
structural.

The inverse encoder emits an assistant-role chunk for `MessageStart`; consumes
the one text block's start/stop without native events; emits a CC content
chunk for every `TextDelta`; and emits a finish/usage chunk for
`MessageDelta`. `MessageDone` emits no chunk. A valid general IR stream with
multiple text blocks is outside this flattened CC M6 profile and MUST be
rejected by this encoder rather than given fictitious native boundaries.

CC finish mapping follows [10, §4](10-mapping-openai-chat-completions.md#4-response-field-mapping): `stop` → `end_turn`, `length` → `max_tokens`,
`content_filter` → `refusal`, and `tool_calls` → `tool_use`. An unknown
native finish value maps to `other` with an `unmapped-value` loss. Encoding
IR `stop_sequence` as `finish_reason: "stop"` records an `unmapped-value`
loss because CC does not identify the matched sequence.

A CC `delta.tool_calls` payload is a structural error; it violates the M6
text-only profile and the encoder rules in this document. M7 defines tool
call aggregation (N-S-10).

## 4. Responses profile — N-S-6

RE distinguishes native output-item lifecycle from content-part lifecycle.
Only an assistant `message` output item with `output_text` parts is supported
in M6. **Content parts, not output items, define IR block boundaries.**

The supported native sequence is:

```text
response.created
response.output_item.added
( response.content_part.added
  response.output_text.delta*
  response.output_text.done
  response.content_part.done )*
response.output_item.done
response.completed | response.incomplete | response.failed
```

`response.created` emits `MessageStart`. `output_item.added` and
`output_item.done` open and close native item state but emit no IR block
event. An `output_text` `content_part.added` emits `ContentBlockStart`, each
`output_text.delta` emits `TextDelta`, and `content_part.done` emits the
matching `ContentBlockStop`; `output_text.done` validates the native
lifecycle but emits no IR event.

`output_index` is contiguous across native items and `content_index` is
contiguous within one item, beginning at zero in each scope. Neither is an IR
index. A retained part receives the next stream-wide IR block index under
N-S-2, even when a later native item restarts `content_index` at zero.

A non-message item or non-text part records one `unsupported-semantic` loss.
Its matching descendants and done events are absorbed according to N-S-3, so
that later supported items and parts can continue. Function-call argument
events are M7 semantics, not text deltas.

A terminal RE event uses the non-stream response status policy
([11, §4](11-mapping-openai-responses.md#4-response-field-mapping)) with no
M6 tool-use completion. Its terminal `Response.usage` becomes final IR usage.
`completed` therefore maps to `end_turn` in this profile; `incomplete` with
`max_output_tokens` maps to `max_tokens`; other incomplete or failed states
follow the established `other` plus loss policy.

The inverse encoder emits `response.created` at `MessageStart`. The first
text block start emits both a synthesized assistant
`response.output_item.added` and `response.content_part.added`; later text
blocks emit only another content-part start on that item. A block stop emits
`response.output_text.done` followed by `response.content_part.done`, with
accumulated native text. `MessageDelta` emits `response.output_item.done` if
an item was opened, then the terminal response event; `MessageDone` emits no
native event.

On encode, `end_turn` and `tool_use` render `response.completed`,
`max_tokens` renders `response.incomplete` with reason `max_output_tokens`,
`refusal` renders `response.failed` with error code `refusal`, and
`stop_sequence` renders `response.completed` plus an `unmapped-value` loss
for the unrepresentable matched sequence. The `tool_use` terminal rendering
does not establish M6 tool streaming; decoding it remains subject to the
text-only terminal rule above.

## 5. Anthropic Messages profile — N-S-7

AN provides native content-block lifecycle events. The M6 supported sequence
is:

```text
message_start
( content_block_start(text)
  content_block_delta(text_delta)*
  content_block_stop )*
message_delta
message_stop
```

`message_start` emits `MessageStart`; a supported text block start, delta,
and stop emit their corresponding IR block events; `message_delta` buffers
the stop reason and final usage; and `message_stop` emits `MessageDelta` then
`MessageDone`. The subsequent `Flush` only confirms completion and emits no
additional IR events.

AN native block indexes validate its native lifecycle, but emitted block
indexes follow N-S-2. An unsupported AN block at native index 0 is absorbed
through its native stop, and a following supported native text block at index
1 emits IR block index 0. No next native block, `message_delta`, or
`message_stop` may arrive before an open skipped native block receives its
stop event.

A non-text content block produces one `unsupported-semantic` loss and is
absorbed as a native lifecycle unit. An unknown event type or unknown delta
type produces one `unsupported-semantic` loss without changing valid state.
`input_json_delta` is specifically deferred to M7.

The inverse encoder is near-identity: each M6 IR event emits the corresponding
AN event — `MessageStart` → `message_start`, content-block events → their
named counterparts, `MessageDelta` → `message_delta`, and `MessageDone` →
`message_stop`. The encoder preserves contiguous IR indexes as AN wire
indexes. AN stop reasons map directly for `end_turn`, `max_tokens`,
`stop_sequence`, `tool_use`, and `refusal`; unknown values are structural.
`stop_sequence` is emitted only when its IR stop reason is `stop_sequence`,
as required by [01, §4.1](01-intermediate-representation.md#41-response).

## 6. SSE boundary — N-S-8

SSE is a byte framing layer, not a fourth protocol face. The optional
`go/sse` reference adapter reads and writes opaque frames with `event` and
`data` fields; it has no JSON, model, loss, or IR knowledge. Its adapter
contract is:

- LF, CRLF, and mixed line endings are accepted;
- multiple `data:` lines join with `\n`;
- comments and unknown SSE fields are ignored;
- a trailing frame started by an `event` or `data` field dispatches on clean EOF; and
- literal data `[DONE]` is ordinary opaque data, not a library sentinel.

A provider integration MAY pair an SSE frame decoder with a provider JSON
decoder, but it MUST report framing errors at the SSE layer and protocol/event
errors at the face layer. It MUST NOT convert SSE frames directly into IR
without provider-specific JSON decoding.

## 7. Stream equivalence and vectors — N-S-9

IR event streams compare as the total, element-wise sequence required by
INV-8. Native streams do not require byte-for-byte round trips: provider
chunk boundaries, synthesized envelope fields, and native lifecycle detail
may differ after an IR round trip. Conformance vectors MUST assert the
ordered IR event sequence and its separate ordered loss list; any native
output comparison MUST use the face-specific typed event shape.

Text fragments are preserved in their received order. This does not authorize
re-parsing or re-serializing tool argument fragments: when M7 admits those
fragments, INV-1 exact string preservation applies.

## 8. Normalization rules

Each rule has a stable ID usable as a streaming vector tag.

| ID | Rule |
|----|------|
| N-S-1 | IR stream grammar and face-specific terminal/Flush behavior |
| N-S-2 | emitted IR index ownership, native lifecycle validation, and compact indexes after skipped units |
| N-S-3 | structural errors, loss containment, and skip-and-absorb behavior |
| N-S-4 | M6 text-only profile and typed native JSON/envelope rendering |
| N-S-5 | Chat Completions flattened text stream mapping |
| N-S-6 | Responses item/part lifecycle and terminal mapping |
| N-S-7 | Anthropic Messages block lifecycle and terminal mapping |
| N-S-8 | SSE framing boundary |
| N-S-9 | stream comparison and vector expectations |
| N-S-10 | M7 streaming tool-argument aggregation |

## 9. Loss catalog

| Source construct | Direction | Required result |
|------------------|-----------|-----------------|
| unknown standalone native event | face → IR | one `unsupported-semantic` loss per event; valid state unchanged |
| unsupported native block/item/part | face → IR | one `unsupported-semantic` loss per native unit; matching descendants/done events absorbed |
| non-function tool call | CC → IR | one `unsupported-semantic` loss; descendants absorbed |
| RE `function_call_output` item | RE → IR | one `unsupported-semantic` loss; descendants absorbed |
| AN `server_tool_use`, thinking, provider-hosted tool | AN → IR | one `unsupported-semantic` loss per unit; descendants absorbed |
| unknown native stop value | face → IR | mapping-document `unmapped-value` loss and IR `other`, where the face status policy permits it |
| IR `stop_sequence` for CC or RE | IR → face | native completed/stop form plus `unmapped-value` loss for matched-sequence identity |
| malformed lifecycle or IR grammar | both | structural error, never a loss |

## 10. M7 tool-argument profile — N-S-10

The M7 profile extends the text-only stream contract with complete support
for streamed function-tool input across all three faces. It uses the
already-defined IR types `ToolUseBlock`, `InputJSONDelta`, and
`MessageDone`; it does not loosen INV-1 or any other IR invariant.

### 10.1 Shared aggregation rules — N-S-10

A decoder MUST buffer each native function-tool unit until its native
closure. A decoder MUST replay `ContentBlockStart`, all
`InputJSONDelta` fragments, and `ContentBlockStop` together only after
that closure. The `ToolUseBlock` input string MUST equal the exact
concatenation of its fragments. A decoder MUST preserve empty fragments
and MUST NOT parse the payload as JSON. A native lifecycle or identity
disagreement is a structural error; an unsupported native unit is one
`unsupported-semantic` loss plus descendant absorption.

Face-specific closure boundaries:

- CC: `Flush` after `finish_reason` is observed.
- RE: `response.output_item.done`.
- AN: `content_block_stop`.

CC index ordering / interleaving: native `tool_calls[].index` values may
interleave in chunk encounter order; the decoder replays retained calls
in increasing index order at `Flush`. RE argument-done is optional for
acceptance but serves as a validation event when present. AN allows a
no-delta fallback: if no `input_json_delta` is supplied, the native
`input` at block start becomes the final input via one synthetic
`InputJSONDelta`.

Encoder synthesis / validation rules: the CC encoder normalizes text
before tool calls and emits one `degraded` loss when a tool block is
followed by a text block in IR; it synthesizes a full-arguments delta
when no input delta was supplied for a retained call. The RE encoder
opens one synthesized `function_call` item per `ToolUseBlock`,
synthesizes one full-arguments `response.function_call_arguments.delta`
when no argument deltas were supplied for that block, and emits
`response.function_call_arguments.done` on block stop. The AN encoder
emits a canonical streaming placeholder
`input: {}` at `content_block_start(type:"tool_use")` and synthesizes a
full input-json delta when no delta was supplied.

### 10.2 Loss catalog additions — N-S-10

Existing M6 losses for CC `delta.tool_calls`, RE function-call argument
streams, and AN `input_json_delta` are replaced by full conversion rules.
`function_call_output` streaming and other non-function tool semantics
remain unsupported and record one `unsupported-semantic` loss per native
unit with descendant absorption.

## 11. References

- [00 — Scope and Architecture](00-scope-and-architecture.md)
- [01 — Intermediate Representation](01-intermediate-representation.md)
- [02 — Loss Policy](02-loss-policy.md)
- [03 — Model Handling](03-model-handling.md)
- [10 — Mapping: OpenAI Chat Completions](10-mapping-openai-chat-completions.md)
- [11 — Mapping: OpenAI Responses](11-mapping-openai-responses.md)
- [12 — Mapping: Anthropic Messages](12-mapping-anthropic-messages.md)
- [vectors/README.md](../vectors/README.md)
- [go/sse](../go/sse)
