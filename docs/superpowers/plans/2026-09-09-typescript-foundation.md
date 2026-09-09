# TypeScript Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the testable, zero-runtime-dependency TypeScript foundation required for all oxa protocol faces: package tooling, lossless JSON, errors/losses, IR, model mapping, vector utilities, and byte-level SSE.

**Architecture:** `ts/` is one ESM npm package compiled by `tsc`. The foundation is deliberately face-neutral: only later spokes may depend on `ir` and `modelmap`; `sse` stays independent. JSON is parsed into a lossless AST so comparisons and opaque JSON fields never depend on JavaScript `number` or native JSON serialization.

**Tech Stack:** Node.js 20+, TypeScript 5.9.3, Prettier 3.8.3, Node built-in `node:test` and `node:assert/strict`, npm.

**Spec:** `docs/superpowers/specs/2026-09-09-typescript-support-design.md`

## Global Constraints

- `vectors/` defines behavior; schemas define structure; Markdown defines remaining semantics.
- ESM-only; publish no CJS output and use no runtime dependencies.
- Do not mutate caller inputs, use global state, perform I/O from library code, or call native `JSON.parse`/`JSON.stringify` on fidelity-critical paths.
- `JsonText`, tool arguments, tool schemas, and JSON deltas are opaque strings unless an explicit mapping rule requires a lossless tree.
- Structural and lifecycle failures throw `OxaError`; semantic conversion gaps return ordered `Loss` records.
- Foundation package APIs use `readonly` data where exposed. All production additions are preceded by a failing `node:test` test.
- Keep foundation, each face, streams, and release integration in separate commits.

---

### Task 1: ESM package scaffold and deterministic test command

**Files:**
- Modify: `.gitignore`
- Modify: `Makefile`
- Create: `ts/package.json`
- Create: `ts/tsconfig.json`
- Create: `ts/tsconfig.test.json`
- Create: `ts/src/index.ts`
- Create: `ts/test/package.test.ts`

**Interfaces:**
- Produces `npm run build`, `npm test`, `npm run check`, and an ESM package root import for every later task.
- Produces `make test-ts`, `make lint-ts`, and `make fmt-ts` wrappers used by later CI work.

- [ ] **Step 1: Add the first failing package-root test**

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { packageName } from "../src/index.js";

test("exports its npm package name", () => {
  assert.equal(packageName, "@elkpi/oxa");
});
```

- [ ] **Step 2: Add build configuration, then verify RED**

Create an ESM `package.json` with `name: "@elkpi/oxa"`, `version: "0.1.0"`, `type: "module"`, `engines.node: ">=20"`, `files: ["dist", "README.md", "LICENSE", "NOTICE"]`, and scripts:

```json
{
  "build": "tsc -p tsconfig.json",
  "build:test": "tsc -p tsconfig.test.json",
  "test": "npm run build:test && node --test dist-test/test",
  "check": "tsc --noEmit -p tsconfig.json",
  "fmt": "prettier . --check"
}
```
Install exact dev dependencies before testing: `npm install --save-dev --save-exact typescript@5.9.3 prettier@3.8.3`.

Set both TypeScript configs to `strict: true`, `module` and `moduleResolution` `NodeNext`, `target: ES2022`, `declaration: true`, `sourceMap: true`, `noUncheckedIndexedAccess: true`, and `exactOptionalPropertyTypes: true`. The test config emits `src/` and `test/` to `dist-test/` and sets `declaration: false`.

Run: `cd ts && npm test`

Expected: compilation fails because `src/index.ts` does not exist; the failure proves the test exercises the public root module.

- [ ] **Step 3: Implement the minimal root module and verify GREEN**

```ts
/** The npm package coordinate, exposed for installation smoke tests. */
export const packageName = "@elkpi/oxa";
```

Add `ts/node_modules/`, `ts/dist/`, and `ts/dist-test/` to `.gitignore`. Add `test-ts`, `lint-ts`, and `fmt-ts` Makefile targets that run the matching npm scripts only when `ts/package.json` exists.

Run: `cd ts && npm test && npm run check`

Expected: one passing test and no TypeScript diagnostics.

- [ ] **Step 4: Commit the scaffold**

```bash
git add .gitignore Makefile ts/package.json ts/tsconfig.json ts/tsconfig.test.json ts/src/index.ts ts/test/package.test.ts
git commit -m "feat(ts): scaffold ESM package"
```

---

### Task 2: Stable errors, losses, and model mapping

**Files:**
- Create: `ts/src/error.ts`
- Create: `ts/src/loss.ts`
- Create: `ts/src/modelmap.ts`
- Create: `ts/test/error.test.ts`
- Create: `ts/test/loss.test.ts`
- Create: `ts/test/modelmap.test.ts`
- Modify: `ts/src/index.ts`

**Interfaces:**
- Produces `OxaError`, `OxaErrorCode`, `Loss`, `LossReason`, `ConversionResult<T>`, `ModelMapper`, and `mapModel`.
- Later faces return `ConversionResult<T>` and accept `{ modelMapper?: ModelMapper }` options.

- [ ] **Step 1: Write failing behavior tests**

```ts
test("preserves OxaError code and cause", () => {
  const cause = new Error("bad event");
  const error = new OxaError("stream-grammar", "expected message_start", { cause });
  assert.equal(error.name, "OxaError");
  assert.equal(error.code, "stream-grammar");
  assert.equal(error.cause, cause);
});

test("uses identity mapping when no mapper matches", () => {
  assert.equal(mapModel(undefined, "gpt-test"), "gpt-test");
  assert.equal(mapModel((model) => model === "a" ? "b" : undefined, "a"), "b");
  assert.equal(mapModel((model) => model === "a" ? "b" : undefined, "other"), "other");
});
```

Run: `cd ts && npm test`

Expected: compilation fails because `OxaError` and `mapModel` do not exist.

- [ ] **Step 2: Implement only the tested contracts**

Define `OxaErrorCode` as the closed union `"invalid-json" | "type-violation" | "stream-grammar" | "stream-lifecycle" | "ir-invariant"`. Define `LossReason` from `loss.schema.json`; define readonly loss/result interfaces. `ModelMapper` is `(model: string) => string | undefined`; `mapModel` applies identity fallback.

Run: `cd ts && npm test && npm run check`

Expected: all foundation tests pass and public types remain strict.

- [ ] **Step 3: Commit errors, losses, and model mapping**

```bash
git add ts/src/error.ts ts/src/loss.ts ts/src/modelmap.ts ts/src/index.ts ts/test/error.test.ts ts/test/loss.test.ts ts/test/modelmap.test.ts
git commit -m "feat(ts): add core errors losses and model mapping"
```

---

### Task 3: Lossless JSON parser, serializer, and explicit construction

**Files:**
- Create: `ts/src/json/types.ts`
- Create: `ts/src/json/parse.ts`
- Create: `ts/src/json/stringify.ts`
- Create: `ts/src/json/value.ts`
- Create: `ts/src/json/index.ts`
- Create: `ts/test/json.test.ts`
- Modify: `ts/src/index.ts`

**Interfaces:**
- Produces `JsonValue`, `JsonObject`, `JsonArray`, `JsonNumber`, `JsonText`, `parseJson`, `stringifyJson`, `integer`, and `fromValue`.
- `JsonNumber` retains its source token and exposes exact integer classification; serializers emit the retained token.

- [ ] **Step 1: Write failing fidelity tests**

```ts
test("preserves non-integer spelling and large integers", () => {
  const value = parseJson('{"a":1.0,"b":9007199254740993}') as JsonObject;
  assert.equal(stringifyJson(value), '{"a":1.0,"b":9007199254740993}');
  assert.equal(value.a.kind, "number");
  assert.equal(value.a.token, "1.0");
  assert.equal(value.b.kind, "number");
  assert.equal(value.b.isInteger, true);
});

test("rejects malformed JSON with an OxaError code", () => {
  assert.throws(() => parseJson('{'), (error: unknown) =>
    error instanceof OxaError && error.code === "invalid-json",
  );
});
```

Run: `cd ts && npm test`

Expected: compilation fails because `parseJson` and `JsonObject` do not exist.

- [ ] **Step 2: Implement recursive-descent JSON without native JSON conversion**

Represent null, boolean, string, array, object, and number as discriminated immutable values. Parse string escapes and UTF-16 surrogate pairs; retain number lexemes after RFC 8259 syntax validation. `integer(value: bigint)` creates a number token with `value.toString()`. `fromValue` accepts only `null | boolean | string | number | bigint | readonly JsonValue[] | Readonly<Record<string, JsonValue>>`; reject non-finite numbers and convert finite JavaScript numbers to their explicit canonical token.

Run: `cd ts && npm test && npm run check`

Expected: parser tests pass; no implementation calls `JSON.parse` or `JSON.stringify`.

- [ ] **Step 3: Add exact parser edge tests before any refactor**

Add failing tests for `0.50`, `1e+01`, escaped slash/Unicode strings, nested arrays, duplicate object keys (reject with `invalid-json`), and trailing bytes. Implement only the parser branches needed for each test and rerun `npm test` after each addition.

- [ ] **Step 4: Commit lossless JSON**

```bash
git add ts/src/json ts/test/json.test.ts ts/src/index.ts
git commit -m "feat(ts): add lossless JSON codec"
```

---

### Task 4: IR values, schema types, invariants, and JSON codec

**Files:**
- Create: `ts/scripts/generate-schema-types.mjs`
- Create: `ts/src/generated/ir-schema.ts`
- Create: `ts/src/generated/loss-schema.ts`
- Create: `ts/src/generated/vector-schema.ts`
- Create: `ts/src/ir/types.ts`
- Create: `ts/src/ir/checker.ts`
- Create: `ts/src/ir/codec.ts`
- Create: `ts/src/ir/index.ts`
- Create: `ts/test/ir.test.ts`
- Modify: `ts/package.json`

**Interfaces:**
- Produces typed IR request, response, blocks, events, `assertEventSequence`, `encodeRequest`, `decodeRequest`, `encodeResponse`, and `decodeResponse`.
- Later face decoders construct only checked IR; face encoders call the checker before accepting IR input.

- [ ] **Step 1: Write failing IR grammar tests**

```ts
test("rejects a delta before its block start", () => {
  assert.throws(() => assertEventSequence([
    { type: "message_start", id: "m", model: "model" },
    { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "x" } },
  ]), (error: unknown) => error instanceof OxaError && error.code === "ir-invariant");
});

test("accepts contiguous text block events", () => {
  assert.doesNotThrow(() => assertEventSequence([
    { type: "message_start", id: "m", model: "model" },
    { type: "content_block_start", index: 0, block: { type: "text", text: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "x" } },
    { type: "content_block_stop", index: 0 },
    { type: "message_delta", stop_reason: "end_turn", usage: { input_tokens: 0n, output_tokens: 0n } },
    { type: "message_done" },
  ]));
});
```

Run: `cd ts && npm test`

Expected: compilation fails because the IR module does not exist.

- [ ] **Step 2: Generate only schema-backed structural declarations**

Implement `generate-schema-types.mjs` as a deterministic Node script that reads the three schema files, emits the three generated source files with a `// Code generated ... DO NOT EDIT.` header, and exits nonzero when `npm run generate:check` finds a diff. Add `generate` and `generate:check` package scripts. Do not generate provider wire shapes; they have no schemas.

- [ ] **Step 3: Implement IR checker and codec**

Use discriminated unions for blocks and events. Enforce INV-5 event grammar and INV-6 contiguous indexes in `assertEventSequence`; make all violations `OxaError("ir-invariant", ...)`. Encode/decode IR JSON with the lossless JSON module, preserve `JsonText`, enforce the schema `specVersion`, and represent integer token counts as `bigint` internally.

Run: `cd ts && npm run generate && npm test && npm run generate:check`

Expected: IR grammar/codec tests pass and generated source is clean.

- [ ] **Step 4: Commit IR foundation**

```bash
git add ts/scripts ts/src/generated ts/src/ir ts/test/ir.test.ts ts/package.json
git commit -m "feat(ts): add checked intermediate representation"
```

---

### Task 5: Vector loader and semantic comparator

**Files:**
- Create: `ts/src/vectest/load.ts`
- Create: `ts/src/vectest/compare.ts`
- Create: `ts/src/vectest/stream.ts`
- Create: `ts/src/vectest/index.ts`
- Create: `ts/test/vectest.test.ts`

**Interfaces:**
- Produces `findRepoRoot`, `loadVectors`, `compareJson`, `compareLosses`, and `compareStreams` for later per-face test bindings.
- Comparisons follow `vectors/README.md`: object-key-order independent, numeric exactness aware, loss set comparison by `(path, field, reason)`, and stream normalization aware.

- [ ] **Step 1: Write failing vector comparison tests**

```ts
test("distinguishes 1 from 1.0 while normalizing decimal spelling", () => {
  assert.notEqual(compareJson(parseJson("1"), parseJson("1.0")), undefined);
  assert.equal(compareJson(parseJson("0.50"), parseJson("0.5")), undefined);
});

test("compares losses independent of encounter order", () => {
  const left = [{ path: "a", field: "x", reason: "unmapped-field" }] as const;
  const right = [...left].reverse();
  assert.equal(compareLosses(left, right), undefined);
});
```

Run: `cd ts && npm test`

Expected: compilation fails because `compareJson` and `compareLosses` do not exist.

- [ ] **Step 2: Implement comparator and isolated vector discovery**

Walk `JsonValue` recursively; compare integer tokens as `bigint` and non-integer tokens as exact decimal rationals, never as `number`. Sort only copies of loss records by `(path, field, reason, detail)` for comparison. `findRepoRoot` walks upward until `.git` and `vectors/` exist; callers skip rather than fail when installed outside a repository.

Run: `cd ts && npm test`

Expected: numeric and loss comparison tests pass.

- [ ] **Step 3: Add stream normalization tests and implementation**

Write a failing test with two chunkings of the same text/tool-argument blocks, then implement normalization that compares block sequence, concatenated text, aggregated argument strings, final stop reason, and final usage per `spec/00`.

Run: `cd ts && npm test && npm run check`

Expected: equivalent chunking passes; changed block order fails.

- [ ] **Step 4: Commit vector utilities**

```bash
git add ts/src/vectest ts/test/vectest.test.ts
git commit -m "test(ts): add vector comparison utilities"
```

---

### Task 6: Opaque byte-level SSE framing

**Files:**
- Create: `ts/src/sse/decoder.ts`
- Create: `ts/src/sse/encoder.ts`
- Create: `ts/src/sse/index.ts`
- Create: `ts/test/sse.test.ts`
- Modify: `ts/src/index.ts`

**Interfaces:**
- Produces `SseDecoder.feed(chunk: Uint8Array): readonly SseEvent[]`, `SseDecoder.flush(): readonly SseEvent[]`, and `encodeSse(event): Uint8Array`.
- `SseEvent` contains only framed field text; no provider JSON/IR/loss/[DONE] behavior exists in this module.

- [ ] **Step 1: Write failing UTF-8 boundary and opaque-data tests**

```ts
test("frames a UTF-8 data field split inside a code point", () => {
  const bytes = new TextEncoder().encode("data: 你\\n\\n");
  const decoder = new SseDecoder();
  assert.deepEqual(decoder.feed(bytes.slice(0, 7)), []);
  assert.deepEqual(decoder.feed(bytes.slice(7)), [{ data: "你" }]);
});

test("does not interpret DONE as a sentinel", () => {
  const decoder = new SseDecoder();
  assert.deepEqual(decoder.feed(new TextEncoder().encode("data: [DONE]\\n\\n")), [{ data: "[DONE]" }]);
});
```

Run: `cd ts && npm test`

Expected: compilation fails because `SseDecoder` does not exist.

- [ ] **Step 2: Implement byte buffering and framing**

Buffer `Uint8Array` until complete CRLF/LF records are available; decode only complete field lines with a fatal UTF-8 decoder; preserve `data`, `event`, `id`, and `retry` framing rules. `flush` rejects incomplete UTF-8 or incomplete final frames with `OxaError("invalid-json", ...)` only if the framing contract requires malformed input rejection; it never emits IR or a loss.

Run: `cd ts && npm test && npm run check`

Expected: split UTF-8, CRLF, multi-line data, comments, and `[DONE]` tests pass.

- [ ] **Step 3: Commit SSE**

```bash
git add ts/src/sse ts/test/sse.test.ts ts/src/index.ts
git commit -m "feat(ts): add opaque SSE framing"
```

---

### Task 7: Foundation integration and handoff boundary

**Files:**
- Create: `ts/README.md`
- Modify: `ts/package.json`
- Modify: `Makefile`
- Modify: `README.md`

**Interfaces:**
- Produces documented installation/build/testing instructions for the foundation branch.
- Explicitly does not claim protocol conversion support until the three face plans are complete.

- [ ] **Step 1: Write the failing packaging-content test**

Create `ts/test/package-files.test.ts` that runs `npm pack --json --dry-run`, parses its output through `parseJson`, and asserts the candidate file list excludes `src/`, `test/`, `vectors/`, and `spec/` while including `dist/index.js`, `dist/index.d.ts`, `README.md`, and `LICENSE`.

Run: `cd ts && npm test`

Expected: it fails before `files`, README, and copied license assets are configured.

- [ ] **Step 2: Configure package files and write accurate docs**

Set `exports` for the implemented foundation subpaths only (`.`, `./json`, `./ir`, `./modelmap`, `./sse`), add `types`, and add a `prepack` build hook. Copy/link repository license notices in a packaging-safe way. `ts/README.md` must state that this branch supplies foundation APIs only and must not imply face conversion is ready. Root README must not add TypeScript to the usable language matrix until all vectors pass.

Run: `cd ts && npm test && npm pack --dry-run && cd .. && make test-ts`

Expected: all foundation tests pass and the tarball contains only declared runtime artifacts/docs/licenses.

- [ ] **Step 3: Run full foundation verification**

Run:

```bash
cd ts && npm run generate:check && npm run check && npm test && npm pack --dry-run
cd .. && make test
```

Expected: TypeScript foundation checks and existing Go suite both pass.

- [ ] **Step 4: Commit the integration boundary**

```bash
git add ts/README.md ts/package.json Makefile README.md ts/test/package-files.test.ts
git commit -m "docs(ts): document foundation package"
```

## Follow-on Plans

After this plan is green, create separate plans in this order: (1) Chat Completions non-streaming and stream/M7; (2) Responses non-streaming and stream/M7; (3) Anthropic non-streaming and stream/M7; (4) cross-vector bindings, async stream helpers, architecture checks, CI, clean consumer, reliability replay, package release workflow, and final docs. Each face plan must read its mapping document before its first test and must bind every corresponding shared vector.
