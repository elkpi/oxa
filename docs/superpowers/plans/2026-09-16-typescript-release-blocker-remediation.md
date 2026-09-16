# TypeScript Release-Blocker Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the four TypeScript protocol-conformance findings and reopen the npm release gate only after focused, full, Windows CI, and branch-review evidence succeeds.

**Architecture:** Preserve raw Anthropic tool-input source text with an explicit typed-wire sidecar at the JSON boundary; do not alter the generic object fallback. Centralize native stream usage validation in a lossless int64 helper and centralize request-sequence validation in the IR layer. Model Responses skipped item/part lifecycles explicitly, so loss containment cannot bypass identity checks.

**Tech Stack:** TypeScript strict ESM, Node `node:test`, project lossless JSON tree, golden vectors, npm package-consumer test, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-16-typescript-release-blocker-remediation-design.md`

## Global Constraints

- Read `spec/README.md`, `spec/01-intermediate-representation.md`, `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md`, and `spec/20-streaming-semantics.md` before changing a converter.
- Source of truth order is specification, golden vectors, then implementation. Refresh `vectors/manifest.json` whenever a vector is added or changed.
- Faces may import only standard library, `ir`, `json`, `loss`, `modelmap`, and local face modules; no face-to-face conversion imports.
- Raw `ToolUseBlock.input` and `InputJsonDelta.partial_json` are opaque strings: never parse and re-serialize them on a source-preserving path.
- Structural input and lifecycle errors throw `OxaError`; semantic gaps append ordered `Loss` records and never become IR events.
- Native stream usage is a non-negative signed-int64 integer. It must enter and leave TypeScript as `bigint` or a lossless integer JSON token; never call `BigInt(number)` or `Number(bigint)` for usage.
- Keep commits narrow and cherry-pickable. Do not publish to npm, tag a release, or claim readiness until Task 6 passes.

---

## File Map

- `ts/src/json/parse.ts`, `ts/src/json/value.ts`, `ts/src/json/stringify.ts`, and `ts/src/json/index.ts` — attach, retrieve, and emit a nested value's exact raw object text without weakening `parseJson`.
- `ts/src/anthropic/messages/types.ts` — typed `tool_use` block sidecar for source-preserved `input` text and lossless stream usage fields.
- `ts/src/anthropic/messages/nonstream.ts` — use the sidecar for N-AN-4 and call the shared request validator.
- `ts/src/ir/nonstream.ts` — own `validateRequest(request)` and validate both decoded documents and values being encoded.
- `ts/src/ir/usage.ts` (new) — parse and render signed-int64 non-negative usage totals without JavaScript number coercion.
- `ts/src/openai/chatcompletions/stream.ts`, `ts/src/openai/responses/types.ts`, `ts/src/openai/responses/stream.ts`, `ts/src/anthropic/messages/stream.ts` — adopt the usage helper; Responses also receives explicit skip-unit identity state.
- `ts/src/openai/chatcompletions/nonstream.ts`, `ts/src/openai/responses/nonstream.ts`, `ts/src/anthropic/messages/nonstream.ts` — validate face decode output before return and IR encode input before projection; Responses normalizes an empty native assistant item.
- `ts/test/anthropic-nonstream.test.ts`, `ts/test/*-stream.test.ts`, `ts/test/ir.test.ts`, and new `ts/test/request-invariants.test.ts` — direct regression coverage.
- `vectors/anthropic/nonstream/raw-json-tool-input-to-ir.json`, `vectors/{chatcompletions,responses,anthropic}/stream/usage-int64-to-ir.json`, `vectors/responses/stream/skipped-part-loss.json`, and `vectors/manifest.json` — behavior fixtures and generated manifest.

### Task 1: Preserve typed Anthropic raw tool-input bytes

**Files:**
- Modify: `ts/src/json/parse.ts`, `ts/src/json/value.ts`, `ts/src/json/stringify.ts`, `ts/src/json/index.ts`, `ts/src/anthropic/messages/types.ts`, `ts/src/anthropic/messages/nonstream.ts`
- Modify: `ts/test/anthropic-nonstream.test.ts`
- Create: `vectors/anthropic/nonstream/raw-json-tool-input-to-ir.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: `parseJson(source): JsonValue`, `stringifyJson(value): string`, and N-AN-4.
- Produces: `sourceTextOf(value): JsonText | undefined`, `withSourceText(value, source): JsonObject`, and `AnthropicContentBlock.inputText?: JsonText`; `decodeRequest`/`decodeResponse` use source text only after confirming the parsed value is an object.

- [ ] **Step 1: Write the failing source-fidelity tests and golden vector**

  Add a direct typed-wire test that constructs a parsed Anthropic envelope from source where `input` is exactly ` { "b" : "\\u0041", "a" : 1e+01 } ` and asserts the following result is byte-identical:

  ```ts
  const decoded = decodeResponse(parseJson(source) as JsonObject);
  const encoded = encodeResponse(decoded.value);
  assert.equal(stringifyJson(encoded.value), source);
  assert.equal(decoded.value.content[0]!.input, jsonText(rawInput));
  ```

  Add a generic in-memory `input: { b: "A", a: integer(10n) }` test that expects compact canonical text and no loss. Add the vector with the same object semantic value and expected IR raw `input` string; it guards the IR mapping while the direct test guards source spelling.

- [ ] **Step 2: Run the new test and prove the current implementation fails**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- --test-name-pattern='raw tool input preserves source bytes' ts/test/anthropic-nonstream.test.ts`

  Expected: FAIL because `decodeContent` calls `stringifyJson(input)`, which removes whitespace and normalizes `\\u0041`.

- [ ] **Step 3: Implement a bounded source-span API and typed sidecar**

  Make normal `parseJson` record raw spans for object values and add explicit retrieval/attachment helpers:

  ```ts
  export function sourceTextOf(value: JsonValue): JsonText | undefined;
  export function withSourceText(value: JsonObject, source: JsonText): JsonObject;
  ```

  Make object parsing record `[start, end)` before and after `parseObject`, store spans in a module-private `WeakMap<object, JsonText>`, and have `stringifyJson` emit a registered raw object token before its canonical object branch. Add `inputText?: JsonText` to `AnthropicContentBlock` for typed callers. In `decodeContent`, require `object(block.input, path)` first, then use `block.inputText ?? sourceTextOf(input) ?? jsonText(stringifyJson(input))`; if a supplied `inputText` does not parse to an object, throw the existing face `type-violation`. In `encodeContent`, parse `block.input` to verify object-ness, then return `withSourceText(object(parsed, path), block.input)` so a later JSON serialization writes the original token directly rather than a re-serialized object.

- [ ] **Step 4: Run focused tests, vector check, and TypeScript build**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- ts/test/anthropic-nonstream.test.ts ts/test/vectest.test.ts`

  Run: `make vectors`

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm run build`

  Expected: all pass; `go run ./cmd/veccheck -root . -write-manifest` updates only the generated manifest entries for the new vector.

- [ ] **Step 5: Commit the source-preservation change**

  ```bash
  git add ts/src/json/parse.ts ts/src/json/value.ts ts/src/json/stringify.ts ts/src/json/index.ts ts/src/anthropic/messages/types.ts ts/src/anthropic/messages/nonstream.ts ts/test/anthropic-nonstream.test.ts vectors/anthropic/nonstream/raw-json-tool-input-to-ir.json vectors/manifest.json
  git commit -m "fix(ts): preserve anthropic raw tool input"
  ```

### Task 2: Enforce lossless non-negative int64 stream usage

**Files:**
- Create: `ts/src/ir/usage.ts`
- Modify: `ts/src/ir/index.ts`, `ts/src/openai/chatcompletions/stream.ts`, `ts/src/openai/responses/types.ts`, `ts/src/openai/responses/stream.ts`, `ts/src/anthropic/messages/types.ts`, `ts/src/anthropic/messages/stream.ts`
- Modify: `ts/test/chatcompletions-stream.test.ts`, `ts/test/responses-stream.test.ts`, `ts/test/anthropic-stream.test.ts`
- Create: `vectors/chatcompletions/stream/usage-int64-to-ir.json`, `vectors/responses/stream/usage-int64-to-ir.json`, `vectors/anthropic/stream/usage-int64-to-ir.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: native `bigint | JsonNumber` usage values and `Usage` from `ir/types.ts`.
- Produces: `parseUsageInteger(value: bigint | JsonNumber, path: string): bigint` and `encodeUsageInteger(value: bigint, path: string): bigint`; both throw `OxaError("invalid-input", ...)` outside `0n..9223372036854775807n`.

- [ ] **Step 1: Add failing boundary and invalid-value tests for every face**

  For each decoder feed a terminal event with `9007199254740993n` and assert emitted `message_delta.usage` keeps that exact bigint. Test `9223372036854775807n` passes; `9223372036854775808n`, `-1n`, and `{ kind: "number", token: "1.5", isInteger: false }` throw `OxaError` with code `invalid-input`. For each encoder apply an IR terminal event at the int64 maximum and assert native usage retains a bigint/token; applying one above max throws before returning a native event.

- [ ] **Step 2: Run focused tests and prove number-based paths fail**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- --test-name-pattern='usage.*int64|usage rejects' ts/test/chatcompletions-stream.test.ts ts/test/responses-stream.test.ts ts/test/anthropic-stream.test.ts`

  Expected: FAIL because public usage interfaces are `number` and encoder methods call `Number(usage.input_tokens)`.

- [ ] **Step 3: Implement and adopt the shared helper**

  Implement exactly these constants and guard:

  ```ts
  export const maxUsageTokens = 9_223_372_036_854_775_807n;
  export function parseUsageInteger(value: bigint | JsonNumber, path: string): bigint {
    const parsed = typeof value === "bigint" ? value : BigInt(value.token);
    if ((typeof value !== "bigint" && !value.isInteger) || parsed < 0n || parsed > maxUsageTokens)
      throw new OxaError("invalid-input", `${path} must be a non-negative signed int64 integer`);
    return parsed;
  }
  export function encodeUsageInteger(value: bigint, path: string): bigint {
    return parseUsageInteger(value, path);
  }
  ```

  Replace every stream-native usage field with `bigint | JsonNumber`; pass values through the helper in decoders and encoders. Preserve optional usage semantics: absent Chat chunk usage and absent Responses terminal usage map to zero IR totals; Anthropic `message_delta` retains its required usage object. Do not validate or use `total_tokens` as an input mapping field; it remains derived on output.

- [ ] **Step 4: Run focused suites and vector validation**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- ts/test/chatcompletions-stream.test.ts ts/test/responses-stream.test.ts ts/test/anthropic-stream.test.ts ts/test/vectest.test.ts`

  Run: `make vectors`

  Expected: all pass; no usage code contains `BigInt(event.response.usage.` or `Number(usage.`.

- [ ] **Step 5: Commit the stream usage contract**

  ```bash
  git add ts/src/ir/usage.ts ts/src/ir/index.ts ts/src/openai/chatcompletions/stream.ts ts/src/openai/responses/types.ts ts/src/openai/responses/stream.ts ts/src/anthropic/messages/types.ts ts/src/anthropic/messages/stream.ts ts/test/chatcompletions-stream.test.ts ts/test/responses-stream.test.ts ts/test/anthropic-stream.test.ts vectors/chatcompletions/stream/usage-int64-to-ir.json vectors/responses/stream/usage-int64-to-ir.json vectors/anthropic/stream/usage-int64-to-ir.json vectors/manifest.json
  git commit -m "fix(ts): preserve stream usage integers"
  ```

### Task 3: Centralize INV-2 through INV-4 request validation

**Files:**
- Modify: `ts/src/ir/nonstream.ts`, `ts/src/ir/index.ts`
- Modify: `ts/src/openai/chatcompletions/nonstream.ts`, `ts/src/openai/responses/nonstream.ts`, `ts/src/anthropic/messages/nonstream.ts`
- Create: `ts/test/request-invariants.test.ts`
- Modify: `ts/test/responses-nonstream.test.ts`, `ts/test/ir.test.ts`

**Interfaces:**
- Consumes: `Request` and `Block` from `ir/types.ts`.
- Produces: `validateRequest(request: Request): void`, throwing `OxaError("invalid-input", ...)` for INV-2 through INV-4; every nonstream face calls it at decode-return and encode-entry.

- [ ] **Step 1: Write failing cross-face invariant tests**

  Table-drive invalid requests through IR codec and each face encoder: empty messages, assistant first, empty content, orphan `tool_result`, assistant tool calls with a missing next user message, mismatched result IDs, reversed result order, and tool results split over two following user messages. Assert `OxaError` and zero losses. Add a Responses request test where a native assistant item has `content: []` and asserts decoded IR contains `{ type: "text", text: "" }`.

- [ ] **Step 2: Run the invariant test and verify the missing checks**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- ts/test/request-invariants.test.ts ts/test/responses-nonstream.test.ts`

  Expected: FAIL because valid-looking invalid requests currently reach individual face projection paths.

- [ ] **Step 3: Implement `validateRequest` at the IR boundary**

  Implement the validator in `ir/nonstream.ts` with the following traversal:

  ```ts
  export function validateRequest(request: Request): void {
    if (request.messages.length === 0) fail("request.messages must not be empty");
    if (request.messages[0]!.role !== "user") fail("INV-2: first message must be user");
    for (let index = 0; index < request.messages.length; index += 1) {
      const message = request.messages[index]!;
      if (message.content.length === 0) fail(`messages[${index}].content must not be empty`);
      validateToolTurn(request.messages, index);
    }
  }
  ```

  `validateToolTurn` collects every assistant `tool_use`; it requires the next message to be user, compares its `tool_result` IDs in encounter order to the calls, and rejects any result in a message not immediately following the owning assistant turn. Call it after IR `decodeRequest`, immediately before IR `encodeRequest`, immediately before every face `encodeRequest`, and immediately before every face decoder returns a `Request`. In Responses `decodeRequest`, synthesize one empty TextBlock for every empty non-system message before the validator runs, as required by N-R-2; raw IR and other face requests with empty content remain structural errors.

- [ ] **Step 4: Run invariant, nonstream, architecture, and vector gates**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- ts/test/request-invariants.test.ts ts/test/ir.test.ts ts/test/nonstream-vectors.test.ts ts/test/responses-nonstream.test.ts ts/test/anthropic-nonstream.test.ts`

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm run test:architecture && make vectors`

  Expected: all valid request vectors stay green and invalid inputs are structural errors.

- [ ] **Step 5: Commit request validation**

  ```bash
  git add ts/src/ir/nonstream.ts ts/src/ir/index.ts ts/src/openai/chatcompletions/nonstream.ts ts/src/openai/responses/nonstream.ts ts/src/anthropic/messages/nonstream.ts ts/test/request-invariants.test.ts ts/test/responses-nonstream.test.ts ts/test/ir.test.ts
  git commit -m "fix(ts): enforce IR request invariants"
  ```

### Task 4: Fix Responses skipped-item and skipped-part containment

**Files:**
- Modify: `ts/src/openai/responses/types.ts`, `ts/src/openai/responses/stream.ts`
- Modify: `ts/test/responses-stream.test.ts`
- Create: `vectors/responses/stream/skipped-part-loss.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: `ResponsesStreamEvent` descendant fields `output_index`, `item_id`, `content_index`, `part`, and supported/unknown native part types.
- Produces: discriminated `SkippedUnit` state and `requireSkippedDescendant(event, unit)` that validates identity before absorbing an event; `ResponsesOutputTextPart.type` is `string` rather than a falsely closed literal.

- [ ] **Step 1: Write failing lifecycle-containment tests and vector**

  Add a stream with a supported assistant message and one `{ type: "output_image" }` content part. Feed added, unknown descendant event, done, and item done; assert exactly one unsupported-semantic loss and no IR content events for that part. Add variants where the unknown descendant has a wrong `item_id`, `output_index`, or `content_index`; each must throw `OxaError("stream-lifecycle")`. Add the same unknown-child sequence under an unsupported item and assert exactly one item loss, not one loss per descendant.

- [ ] **Step 2: Run the focused test and show duplicate-loss or identity bypass**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- --test-name-pattern='skipped.*part|unknown descendant' ts/test/responses-stream.test.ts`

  Expected: FAIL because `#isSkippedDescendant` only recognizes skipped items and the fallback switch records new losses before validating supported-unit identities.

- [ ] **Step 3: Replace booleans with explicit skip-unit state**

  Add a local state shape:

  ```ts
  type SkippedUnit =
    | { readonly kind: "item"; readonly outputIndex: number; readonly itemId: string }
    | { readonly kind: "part"; readonly outputIndex: number; readonly itemId: string; readonly contentIndex: number };
  ```

  Store `#skipped: SkippedUnit | undefined`. On an unsupported item or part, record one loss and populate this state. Before handling every known descendant and before the unknown-event fallback, call a helper that checks active item identity and, for part state, `content_index`; it returns `true` only for the exact matching skipped unit. Matching descendants/done events clear state only at their matching done boundary and emit no new losses; all mismatches throw before losses are appended. Broaden `ResponsesOutputTextPart` into an honest content-part interface with `type: string`, optional `text`, and optional annotations, then retain `type === "output_text"` as the supported branch.

- [ ] **Step 4: Run focused stream, vector, and TypeScript checks**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm test -- ts/test/responses-stream.test.ts ts/test/vectest.test.ts`

  Run: `make vectors && PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm run build`

  Expected: one loss per skipped unit, mismatches are `stream-lifecycle`, vectors and strict types pass.

- [ ] **Step 5: Commit lifecycle containment**

  ```bash
  git add ts/src/openai/responses/types.ts ts/src/openai/responses/stream.ts ts/test/responses-stream.test.ts vectors/responses/stream/skipped-part-loss.json vectors/manifest.json
  git commit -m "fix(ts): contain responses skipped stream units"
  ```

### Task 5: Independent remediation review and release candidate verification

**Files:**
- Modify: `docs/superpowers/specs/2026-09-16-typescript-release-blocker-remediation-design.md` (set status to `Approved and implemented` only after Tasks 1-4 pass)
- Create: `docs/superpowers/sdd/2026-09-16-typescript-release-blocker-remediation/review-report.md`

**Interfaces:**
- Consumes: commits from Tasks 1-4 and the approved design.
- Produces: a review report with P0/P1/P2 findings, test evidence, and an explicit publish decision.

- [ ] **Step 1: Run all TypeScript and vector gates from a clean build**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm run release:check`

  Run: `make vectors && make test && make lint && make fmt`

  Expected: package build/consumer test, all TypeScript tests, all vectors, Go tests, vet, and formatting checks pass.

- [ ] **Step 2: Perform a fixed-point standards and specification review**

  Review `3b7d18f..HEAD` against N-AN-4, INV-1 through INV-4, N-S-3, int64 bounds, package exports, and repository style. Record each finding with severity, file/line, reproducible command, and disposition. A P0 or P1 requires a new failing regression test and a return to the affected task; P2 findings are recorded but do not authorize npm publication unless they affect documented release gates.

- [ ] **Step 3: Commit review evidence**

  ```bash
  git add docs/superpowers/specs/2026-09-16-typescript-release-blocker-remediation-design.md docs/superpowers/sdd/2026-09-16-typescript-release-blocker-remediation/review-report.md
  git commit -m "docs(ts): record release blocker remediation review"
  ```

### Task 6: Push, Windows CI, and npm publication decision

**Files:**
- No source change unless CI reveals a reproducible platform defect; then add a failing regression test and return to its owning task.

**Interfaces:**
- Consumes: clean Task 5 report and `npm run release:check` success.
- Produces: GitHub Actions evidence for Linux and Windows and either a blocked publish report or the authorized `npm publish` result.

- [ ] **Step 1: Push the reviewed branch and trigger the CI workflow**

  ```bash
  git push origin typescript-support
  gh workflow run ci.yml --ref typescript-support
  gh run list --branch typescript-support --limit 5
  ```

  Expected: the selected run contains successful TypeScript Linux and `typescript-windows` jobs; inspect the Windows job log rather than inferring path semantics locally.

- [ ] **Step 2: Inspect the completed CI evidence**

  Run: `gh run view <run-id> --json status,conclusion,jobs,url`

  Expected: `conclusion` is `success`; every required job is `success`. If any job fails, reproduce locally when possible, add the regression test, commit/push the minimal repair, and repeat Step 1.

- [ ] **Step 3: Re-run the package release gate against the pushed commit**

  Run: `PATH=/home/ping/.nvm/versions/node/v24.18.0/bin:$PATH npm run release:check`

  Expected: success. Confirm `git status --short` is empty and `git log origin/typescript-support..HEAD` has no commits before publication.

- [ ] **Step 4: Publish only after all evidence is clean**

  Run: `npm publish --access public`

  Expected: npm returns the published package name/version. Immediately verify with `npm view @elkpi/oxa version` (use the package name from `ts/package.json`), then record the command output and release version in the review report with a final documentation commit.

