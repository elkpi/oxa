# Task 5 report — consumer APIs, architecture checks, and CI

## Summary

Implemented Task 5 in commit `b8aee3e` (`feat(ts): add async stream consumers and CI gates`).

- Added generic `decodeStream` and `encodeStream` AsyncIterable adapters plus
  face-specific helpers for Chat Completions, Responses, and Anthropic.
- Decoder adapters yield every `Feed` result immediately, invoke the state
  machine's final `Flush` only after normal source completion, and expose a
  copied cumulative loss snapshot.
- Encoder adapters yield every `Apply` result immediately and retain losses in
  encounter order. The helpers do not collect the source or reinterpret native
  or IR events.
- Source iteration errors propagate at the point they occur. Already-yielded
  events remain observable and decoder `Flush` is not called after a source
  error, so it cannot replace the source error with a lifecycle error.
- Exported the helpers through the package-root `stream` namespace.
- Added architecture enforcement for direct imports between protocol faces and
  imports from the opaque SSE adapter into IR or a protocol face. The checker
  has controlled rejection cases and scans the real TypeScript source tree.
- Added runnable `test:architecture`, `test:node`, and `test:web` npm commands,
  matching Make targets, and a Node 20 CI job.
- The Web Runtime gate compiles production sources without Node types (excluding
  the Node-only vector harness), then exercises SSE Web primitives and async
  stream conversion against the emitted ESM build.

No runtime dependencies, CommonJS output, HTTP behavior, provider SDKs, npm
publishing, vectors, or specifications were added or changed.

## TDD evidence

- Final flush RED: `npm test -- --test-name-pattern='final flush'` failed with
  TS2307 because `src/stream/index.ts` did not exist. After the minimal decoder
  adapter and Chat Completions wrapper were added, the test passed and proved
  that `content_block_stop`, `message_delta`, and `message_done` come from the
  final `Flush`.
- Ordered output/loss and error timing RED: `npm run build:test` failed with
  TS2305/TS2724 because `decodeResponses` and `encodeChatCompletions` were not
  implemented. The focused green run passed all three async tests and proved
  output order, ordered encoder losses, and source-error identity/timing.
- Architecture RED: `npm run build:test` failed with TS2307 for the missing
  architecture checker. The green run passed controlled face-to-face and
  SSE-to-IR rejection cases plus the real-source scan.

## Verification

Fresh verification before the implementation commit exited zero:

```text
go test -count=1 ./...
go build ./...
go vet ./...
test -z "$(gofmt -l .)"
go run ./cmd/veccheck -root .. -check-manifest
  -> schemas OK; 125 vectors, 126 checks, OK

npm run generate:check
npm run fmt
npm run check
npm run build
npm run test:architecture
  -> 3 passed, 0 failed
npm run test:node
  -> 59 passed, 0 failed
npm run test:web
  -> Web Runtime smoke passed
```

Local TypeScript verification used the available Node 24.18.0 toolchain. CI is
explicitly pinned to Node 20.x and runs the same Node and Web commands after
`npm ci`.

## Files and scope

- `ts/src/stream/*`, `ts/src/index.ts`
- `ts/test/stream-consumer.test.ts`
- `ts/test/architecture.test.ts`, `ts/test/architecture-support.ts`
- `ts/tsconfig.web.json`, `ts/scripts/test-web-runtime.mjs`
- `ts/package.json`, TypeScript build-output ignore files
- root `Makefile` and `.github/workflows/ci.yml`

## Residual concerns

- The Web Runtime smoke intentionally runs emitted Web-compatible ESM under
  Node's implementation of standard Web globals; the Node-free compilation is
  the static portability gate. Browser-bundle/package-install validation
  remains Task 6 scope.
