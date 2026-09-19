# 90 — Glossary

Status: normative for terminology. These definitions fix how documents 00–20
and `vectors/README.md` use these terms; where a term has a normative home,
the entry links to it.

**AN / CC / RE** — The three protocol faces: Anthropic Messages, OpenAI Chat
Completions, OpenAI Responses ([00, §1](00-scope-and-architecture.md)).

**Absorb (skip-and-absorb)** — Decoder behavior for an unsupported native
unit: record one loss, consume the unit's matching descendants and
completion events without duplicate losses, and keep the valid surrounding
lifecycle ([20, N-S-3](20-streaming-semantics.md)).

**Additive change** — A spec change that only adds optional semantics
without altering existing rules, IR shapes, or loss semantics; the only
evolution allowed within a frozen series ([README](README.md)).

**Block** — One typed element of message or response content (`text`,
`image`, `tool_use`, `tool_result` in v1); a sealed union
([01, §3.4](01-intermediate-representation.md)).

**Cross vector** — A `protocol-to-protocol` vector: a source wire document
decoded to IR and re-encoded to a target face in one composition; nonstream
only ([vectors/README](../vectors/README.md)).

**Decoder** — A face → IR converter (parse + normalize). Streaming decoders
accept native events incrementally via `Feed` and report accumulated losses
via `Losses()`.

**Derived field** — A field recomputable from carried data (for example
`usage.total_tokens`); exempt from loss reporting ([02, §9](02-loss-policy.md)).

**Document (IR document)** — The serialized JSON form of a Request,
Response, or EventStream, carrying a top-level `specVersion`
([01, §6](01-intermediate-representation.md)).

**Encoder** — An IR → face converter (serialize). Streaming encoders consume
IR events one at a time via `Apply` and may synthesize documented envelope
values.

**Envelope field** — A per-face structural or transport field with no
conversational semantics (for example Chat Completions `object` and
`created`); exempt from loss reporting ([02, §9](02-loss-policy.md)).

**Error vs loss** — An error means the conversion cannot proceed
(malformed JSON, type violation, broken stream invariants); everything
semantically unmappable is a loss, never an error ([02, §4](02-loss-policy.md)).

**Event** — One element of a streaming IR sequence: `message_start`,
`content_block_start`, `content_block_delta`, `content_block_stop`,
`message_delta`, `message_done` ([01, §5](01-intermediate-representation.md)).

**Face** — One of the three supported protocols viewed as a conversion
endpoint. Each face implements exactly face → IR and IR → face, and nothing
else ([00, §4](00-scope-and-architecture.md)).

**Feed / Flush / Apply / Losses()** — Streaming converter interface names
from the Go reference, illustrative for all implementations: `Feed`
consumes one native (decoder) or IR (encoder) event; `Flush` finalizes a
decoder exactly once after the native stream ends; `Apply` consumes one IR
event; `Losses()` returns accumulated losses ([20, §2.1](20-streaming-semantics.md)).

**From-IR rendering defaults** — Fixed values synthesized by an IR → face
converter when the IR document lacks a face envelope field; exempt from
losses because nothing is being dropped ([02, §9](02-loss-policy.md)).

**Golden vector** — A committed JSON file under `vectors/` pairing input
with expected output and expected losses; the behavioral source of truth
([vectors/README](../vectors/README.md)).

**Hub-and-spoke** — The architecture in which every conversion is the
composition of two one-way converters through the IR; no face-to-face
converter exists ([00, §4](00-scope-and-architecture.md)).

**IR** — Intermediate representation: the face-neutral request, response,
block, event, and loss types owned by oxa ([01](01-intermediate-representation.md)).

**IR contract version (specVersion)** — The version pinned by `const` in
`spec/schema/ir.schema.json` and echoed by every IR document and vector; it
tracks the IR document shapes and may lag the spec version ([README](README.md)).

**Invariant (INV-1 … INV-9)** — The normative structural rules of the IR:
raw-JSON opacity, first message user, tool use answered, tool results
merged, event grammar, index discipline, structural equality, total order,
schema agreement ([01, §7](01-intermediate-representation.md)).

**Loss** — A record describing something a conversion could not carry:
`path`, `field`, `reason`, optional `detail`. Losses are first-class output,
never silent, never injected into the IR event stream ([02](02-loss-policy.md)).

**Major / minor / patch (spec)** — Spec-version increments: a patch
clarifies without changing any rule; a minor adds semantics additively; a
major is required once sealed unions or enums are extended after 1.0
([README](README.md)).

**Manifest** — `vectors/manifest.json`: the generated, byte-stable index of
all vectors (name, file, tags, sha256); CI fails on any drift
([vectors/README](../vectors/README.md)).

**M6 / M7 (profile)** — Streaming profiles: M6 is the text-only stream
contract; M7 adds streamed function-tool argument aggregation. Later
profiles extend the same contract ([20](20-streaming-semantics.md)).

**Modelmap** — The single optional model-name transformation point: a
caller-supplied exact-match table with identity fallback; libraries contain
no built-in model knowledge ([03](03-model-handling.md)).

**Native unit / lifecycle** — A provider-side streaming unit (output item,
content part, content block, tool call) with its own start/delta/stop
events; decoders validate native lifecycles without leaking native indexes
into IR ([20, N-S-2](20-streaming-semantics.md)).

**Nonstream / stream (mode)** — The two vector modes: one-shot request or
response conversion versus incremental event-stream conversion.

**Opaque (raw JSON)** — Values carried without parsing or re-serialization:
`tool_use.input`, `input_json_delta.partial_json` (INV-1), verbatim tool
`input_schema` objects ([01, §2](01-intermediate-representation.md)).

**Profile** — A named, bounded extension of the streaming contract (M6, M7,
…), pinned by N-S rules and vector tags ([20](20-streaming-semantics.md)).

**Reason code** — The loss classification enum: `unmapped-field`,
`unmapped-value`, `unsupported-semantic`, `degraded` ([02, §3](02-loss-policy.md)).

**Sealed union** — A type with an exhaustive variant set; extending one
after spec 1.0 is a major version bump ([01, §2](01-intermediate-representation.md)).

**Spec version** — The version of the written specification itself,
declared in [spec/README.md](README.md) and recorded in
[CHANGELOG.md](CHANGELOG.md); independent of the IR contract version and of
implementation releases.

**Stream equivalence** — The face-level notion that two streams are
equivalent when normalized block sequences, concatenated texts, aggregated
tool-argument strings, final stop reason, and final usage all match;
weaker than IR equality and tolerant of chunking differences
([00, §3](00-scope-and-architecture.md)).

**SSE (framing)** — Server-Sent Events treated as an opaque byte-framing
adapter with no JSON, model, loss, or IR knowledge ([20, §6](20-streaming-semantics.md)).

**Vector path grammar** — `vectors/<face>/<mode>/<area>-<case>.json`, with
the dotted `name` derived from the relative path
([vectors/README](../vectors/README.md)).
