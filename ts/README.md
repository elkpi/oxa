# @elkpi/oxa

Pure, in-process TypeScript protocol conversion for OpenAI Chat Completions,
OpenAI Responses, and Anthropic Messages. The package does not make network
requests, choose providers, retry calls, or manage authentication.

## Install

```sh
npm install @elkpi/oxa
```

oxa is ESM-only, supports Node.js 20 and later and Web runtimes, and has no
runtime dependencies.

## Convert through the IR

Each protocol face converts only to or from the face-neutral intermediate
representation. To convert between providers, compose two operations:

```ts
import { anthropic, chatcompletions, type Loss } from "@elkpi/oxa";

const decoded = chatcompletions.decodeRequest(chatCompletionsRequest);
const encoded = anthropic.encodeRequest(decoded.value);
const losses: Loss[] = [...decoded.losses, ...encoded.losses];

const anthropicRequest = encoded.value;
```

Structural or lifecycle errors throw `OxaError`. Semantic gaps are returned
as ordered `Loss[]` values; they are never silently discarded.

## Streaming

The face classes expose incremental `Feed`, `Flush`, `Losses`, and
`Apply` operations. Async-iterable adapters are available under the `stream`
namespace:

```ts
import { stream } from "@elkpi/oxa";

const decoded = stream.decodeChatCompletions(nativeChunks);

for await (const event of decoded) {
  // Forward each IR event without buffering the complete stream.
}

const losses = decoded.losses();
```

Tool-call argument fragments and Anthropic `input_json_delta.partial_json`
remain opaque raw JSON text. Their spelling and fragment order are preserved.

## Public entry points

The root exports common errors, losses, model mapping, and namespaces for every
completed module. Equivalent focused ESM entry points are also available:

- `@elkpi/oxa/error`
- `@elkpi/oxa/loss`
- `@elkpi/oxa/modelmap`
- `@elkpi/oxa/json`
- `@elkpi/oxa/ir`
- `@elkpi/oxa/sse`
- `@elkpi/oxa/stream`
- `@elkpi/oxa/openai/chatcompletions`
- `@elkpi/oxa/openai/responses`
- `@elkpi/oxa/anthropic/messages`

The package includes declarations for the root and every public subpath.

## Development and release verification

From this directory:

```sh
npm install
npm run release:check
```

`release:check` validates vectors and their manifest, generated declarations,
formatting, type safety, Node and Web behavior, tarball contents, a clean
installed consumer, and the Go reference implementation. It only verifies
release readiness and never publishes.

## Contract

Conversion behavior is defined by the repository's golden vectors, structural
shapes by its JSON schemas, and semantics by its specifications, in that order:

1. `vectors/`
2. `spec/schema/`
3. `spec/*.md`

The TypeScript implementation validates all 154 golden vectors for Spec 2.0.0,
including M7 opaque tool data, M9 thinking/reasoning streams, signatures,
reasoning effort, and granular usage accounting.

Apache-2.0. See `LICENSE` and `NOTICE`.
