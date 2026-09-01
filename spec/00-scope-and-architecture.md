# 00 — Scope and Architecture

Status: normative. The current spec version is declared in [README.md](README.md).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119 and RFC 8174 when,
and only when, they appear in all capitals, as shown here.

## 1. Scope

oxa converts structured request, response, and streaming payloads between
three protocol faces:

| Face | Protocol | Mapping document |
|------|----------|------------------|
| CC | OpenAI Chat Completions | [10](10-mapping-openai-chat-completions.md) |
| Responses | OpenAI Responses | [11](11-mapping-openai-responses.md) |
| Anthropic | Anthropic Messages | [12](12-mapping-anthropic-messages.md) |

Version 1 covers:

- **Basic chat**: system prompts, multi-turn conversations, sampling
  parameters, non-streaming requests and responses.
- **Streaming**: conversion of event streams in both directions
  (semantics in document 20).
- **Tool use**: tool definitions, tool choice, tool_use / tool_result
  round-trips, including aggregation of streamed tool arguments.
- **Image inputs**: base64-encoded image data and image URLs.
- **The Responses API** as a first-class face, not as an emulation of CC.

## 2. Non-goals

oxa is none of the following, and no implementation MAY grow these features:

- **No HTTP forwarding or proxying.** No server, no listening socket, no
  re-issuing of requests to any upstream provider. Callers own transport.
- **No routing.** No provider selection, load balancing, failover, or
  retry logic.
- **No auth.** oxa never handles credentials, keys, or tokens.
- **No model knowledge.** Libraries contain no model tables or capability
  data; see document [03](03-model-handling.md).
- **No byte-exact streaming round-trips.** See §3.

## 3. Stream equivalence

Conversions between streaming forms are judged by the stream-equivalence
principle, not by byte equality:

> Two event streams are equivalent if and only if their normalized block
> sequences, concatenated texts, aggregated tool-argument strings, final
> stop reason, and final usage all match.

Concretely, equivalence compares five artifacts, in block order:

1. the **normalized block sequence**: each block's `type`, plus `id` and
   `name` for tool_use blocks, after applying the normalization rules of
   document [20](20-streaming-semantics.md);
2. the **concatenated texts**: for each text block, the concatenation of
   all its text deltas;
3. the **aggregated tool-argument strings**: for each tool_use block, the
   concatenation of all its partial-JSON deltas;
4. the **final stop reason** (the `message_delta` stop reason);
5. the **final usage** (the `message_delta` usage).

Every other difference — delta chunk boundaries, delta counts, empty
deltas, block splitting within a text run — is out of scope for
equivalence. Converting a stream through the IR and back MUST produce an
equivalent stream, but MUST NOT be expected to reproduce the original byte
sequence, chunking, or event count.

## 4. Architecture: hub and spoke

Every conversion between two faces is the composition of exactly two
one-way converters through the IR defined in [01](01-intermediate-representation.md):

- **face → IR** (decode + normalize): parse a face payload and normalize
  it into IR;
- **IR → face** (encode): serialize IR into the target face's payload.

Each face implements exactly these two converters and nothing else.

```
+---------------+   face -> IR   +-----------+   face -> IR   +---------------+
|     OpenAI    |--------------->|           |<---------------|    OpenAI     |
|     Chat      |                |    IR     |                |   Responses   |
|  Completions  |<---------------|   (hub)   |--------------->|               |
+---------------+   IR -> face   +-----------+   IR -> face   +---------------+
                                   ^    |
                        face -> IR |    | IR -> face
                                   |    v
                              +---------------+
                              |   Anthropic   |
                              |    Messages   |
                              +---------------+
```

Rationale:

- **Converter count.** With N faces, direct pairwise conversion needs one
  converter per ordered pair of faces — N(N-1) one-way converters, the
  usual "N²" explosion. The hub-and-spoke design costs exactly 2N one-way
  converters, each written once against the single contract in document 01
  instead of against N-1 foreign payload shapes. At the current N = 3 the
  raw count is equal (6 either way); the win is that every added face costs
  2 converters instead of N, and that correctness only ever has to be
  argued against the IR.
- **Why a block model.** The IR is a normalized block model: a message is a
  role plus an ordered list of content blocks. CC is flat — message content
  is a plain string or a flat array of parts, with tool calls hoisted out
  of content into `tool_calls` — so converting CC in any direction requires
  aggregation into an ordered sequence anyway. Conversely, Anthropic
  Messages and Responses outputs natively consist of ordered content
  blocks, so block boundaries are required by two of the three faces. A
  role + ordered-blocks model is therefore the least common denominator
  that every face can express without structural loss, and flattening
  blocks back into CC's flat shapes is a trivial, well-defined projection.

The IR is not the wire format of any face. It is owned by oxa and defined
entirely by document 01 plus `spec/schema/ir.schema.json`.

## 5. The three locking deliverables

oxa ships as three mutually reinforcing layers:

1. **`spec/`** — this written contract (structure, semantics, rationale).
2. **`vectors/`** — the golden vectors, the behavioral source of truth.
3. **Implementations** — `go/` first, then `rust/`, `python/`, `cpp/`.

The layers lock each other: CI fails if an implementation drifts from the
vectors, if the vectors contradict the schemas, or if the spec is changed
without the corresponding vector update. Precedence between the layers is
fixed in the [README](README.md#source-of-truth-precedence); a conflict is
a bug in the upstream artifact.
