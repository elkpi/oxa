# Spec 2.0.0 Wave 2 — TypeScript Reasoning and Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TypeScript package implement the complete locked Spec 2.0.0 contract and validate all 154 golden vectors, including M9 reasoning streams and granular usage.

**Architecture:** Keep the hub-and-spoke layout intact. First extend the face-neutral TypeScript IR and its codecs for the 2.0 contract; then implement each protocol face only as `face ↔ IR`; finally activate the existing 0.2.0 vectors through a typed stream-vector harness. No new vector or specification behavior is introduced in this wave.

**Tech Stack:** TypeScript 5.9, Node.js 20+, ESM, the repository JSON Schema generator, `node:test`, golden vectors, and the existing Go `veccheck` manifest validator.

**Spec:** `docs/superpowers/specs/2026-09-19-spec-v2-reasoning-and-usage-design.md`; normative behavior is locked in `spec/01-intermediate-representation.md`, `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md`, `spec/20-streaming-semantics.md`, and `vectors/`.

## File structure and ownership

| Area | Files | Responsibility |
| --- | --- | --- |
| IR contract | `ts/src/ir/{types,constants,nonstream,codec,checker,usage,index}.ts`, `ts/src/vectest/stream.ts`, `ts/src/generated/ir-schema.ts` | Model the 2.0 sealed variants, dual-read document version, usage detail fidelity, and M9 grammar. |
| Chat Completions spoke | `ts/src/openai/chatcompletions/{nonstream,stream,index}.ts` | Map `reasoning_content`, `reasoning_effort`, and OpenAI usage detail fields without introducing face-to-face dependencies. |
| Responses spoke | `ts/src/openai/responses/{types,nonstream,stream,index}.ts` | Map reasoning summary items and their stream lifecycle, request effort, encrypted-content loss, and usage details. |
| Anthropic spoke | `ts/src/anthropic/messages/{types,nonstream,stream,index}.ts` | Map native thinking/signatures, documented budget approximation, cache usage, and M9 stream placeholders. |
| Verification | `ts/src/vectest/load.ts`, `ts/test/{ir,constants,chatcompletions-stream,responses-stream,anthropic-stream,nonstream-vector-runner,spec-v2-stream-vectors,vectest}.test.ts` | Enable 0.2.0 vectors and verify typed stream conversion against their expected output/losses. |
| Documentation | `ts/README.md`, `spec/README.md`, root `README.md` only if its current TypeScript capability/count wording differs | State the real Wave 2 coverage, without announcing or creating a package release. |

## Global constraints

- Precedence is already locked: vectors define behavior, schema defines shape, Markdown defines semantics. This wave changes implementation and tests only; do not edit vectors, schema, or mapping rules to accommodate implementation gaps.
- Maintain hub-and-spoke imports: each face may import only local code, IR, JSON, loss, and `modelmap`; never import another protocol face.
- `ThinkingBlock.signature` is opaque: carry it verbatim, do not validate, parse, normalize, or re-sign it. Tool JSON and `input_json_delta.partial_json` retain their existing opaque-byte guarantees.
- `specVersion` readers accept exactly `"0.1.0"` and `"0.2.0"`; all TypeScript IR encoders emit `"0.2.0"`.
- `ThinkingBlock` accepts `thinking_delta*`, then at most one `signature_delta`; a signature delta cannot precede or be followed by a thinking delta. The block closes with `content_block_stop`.
- All new token values are non-negative signed int64 `bigint` values. Optional usage/detail members preserve **absent ≠ zero**.
- Unknown inbound `reasoning_effort` values are omitted from IR and generate one `unmapped-value` loss. Mapping a signed thinking block to Chat Completions generates an `unmapped-field` signature loss; Responses `encrypted_content` generates an `unmapped-field` loss; Anthropic budget mappings generate the specified `degraded` loss.
- Preserve public package coordinates and `ts/package.json` version `1.0.1`; a `2.0.0` package bump/tag belongs only to the later coordinated release-preparation PR after Waves 3–5.
- Each task is a work unit. Its input scope, output artifact, acceptance test, 10-minute execution timeout, cancellation condition, and cleanup rule are stated below. Do not leave background processes or temporary fixtures; `dist/`, `dist-test/`, and `node_modules/` stay ignored and are not committed.
- Run commands from `ts/` unless a command explicitly names the repository root. Use `npm install`, not `npm ci`, because the TypeScript package intentionally has no lockfile.

---

### Task 1: Extend the TypeScript IR to Spec 2.0

**Work unit:** `W2-TS-IR`.

- **Input scope:** The locked IR schema and all files in `ts/src/ir/`, `ts/src/vectest/stream.ts`, `ts/src/generated/ir-schema.ts`, `ts/test/ir.test.ts`, and `ts/test/constants.test.ts`.
- **Output artifact:** A TypeScript IR that serializes 0.2.0, reads 0.1.0/0.2.0, represents thinking/signature deltas and optional usage details, and rejects invalid M9 ordering.
- **Completion condition:** Targeted IR tests, generation check, and TypeScript type checking pass; the generated declaration matches the committed schema output.
- **Verification:** `npm run generate:check && npm run check && node --test dist-test/test/ir.test.js dist-test/test/constants.test.js` after `npm run build:test`.
- **Timeout/cancel:** Stop after 10 minutes without a compiling focused test run; diagnose the first type/grammar failure before changing another surface.
- **Cleanup:** Do not commit `dist/`, `dist-test/`, or `node_modules/`; remove any one-off diagnostic fixture before the commit.

**Files:**
- Modify: `ts/src/ir/types.ts`
- Modify: `ts/src/ir/constants.ts`
- Modify: `ts/src/ir/nonstream.ts`
- Modify: `ts/src/ir/codec.ts`
- Modify: `ts/src/ir/checker.ts`
- Modify: `ts/src/ir/index.ts`
- Modify: `ts/src/vectest/stream.ts`
- Regenerate: `ts/src/generated/ir-schema.ts`
- Modify: `ts/test/ir.test.ts`
- Modify: `ts/test/constants.test.ts`

**Interfaces:**
- Produces `ThinkingBlock`, `ThinkingDelta`, `SignatureDelta`, `InputTokensDetails`, `OutputTokensDetails`, `ReasoningEffort`, and the expanded `Usage`/`Params` types through `@elkpi/oxa/ir`.
- Produces `SPEC_VERSION === "0.2.0"`, `BLOCK_TYPE_THINKING`, `DELTA_TYPE_THINKING_DELTA`, and `DELTA_TYPE_SIGNATURE_DELTA`.
- Produces `assertEventSequence(events)` that accepts only a thinking block’s legal M9 sequence.
- Consumed by every spoke task and the stream-vector test task.

- [ ] **Step 1: Write failing IR contract tests**

Add tests that pin 2.0 emission, 1.0 dual-read, thinking/signature round-tripping, detail presence, and invalid signature ordering:

```ts
test("emits 0.2.0 while accepting a 0.1.0 IR request", () => {
  const request = {
    model: "m",
    messages: [{ role: "user" as const, content: [{ type: "text" as const, text: "hi" }] }],
  };
  assert.deepEqual(decodeRequest({ specVersion: "0.1.0", ...request }), request);
  assert.equal(encodeRequest(request).specVersion, "0.2.0");
});

test("round-trips a signed thinking stream and usage details", () => {
  const stream = { events: [
    { type: "message_start", id: "m", model: "model" },
    { type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "reason" } },
    { type: "content_block_delta", index: 0, delta: { type: "signature_delta", signature: "sig" } },
    { type: "content_block_stop", index: 0 },
    { type: "message_delta", stop_reason: "end_turn", usage: {
      input_tokens: 2n, output_tokens: 3n,
      input_tokens_details: { cached_tokens: 1n },
      output_tokens_details: { reasoning_tokens: 2n },
    } },
    { type: "message_done" },
  ] } as const;
  assert.deepEqual(decodeEventStream(encodeEventStream(stream)), stream);
});

test("rejects a thinking delta after its signature", () => {
  const events = [
    { type: "message_start", id: "m", model: "model" },
    { type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "signature_delta", signature: "sig" } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "late" } },
  ] as const;
  assert.throws(() => assertEventSequence(events), { code: "ir-invariant" });
});
```

Extend `constants.test.ts` to assert the new three constants and `SPEC_VERSION === "0.2.0"` both from the direct constants module and root `ir` namespace.

- [ ] **Step 2: Run the focused tests to prove the baseline fails**

Run:

```bash
npm run build:test && node --test dist-test/test/ir.test.js dist-test/test/constants.test.js
```

Expected: compilation errors for missing 2.0 types/constants or assertion failures because codecs still emit/reject only `0.1.0`.

- [ ] **Step 3: Implement the 2.0 IR types and constants**

In `types.ts`, make the public types exact and readonly:

```ts
export const specVersion = "0.2.0";
export type ReasoningEffort = "minimal" | "low" | "medium" | "high";
export interface InputTokensDetails { readonly cached_tokens: bigint; }
export interface OutputTokensDetails { readonly reasoning_tokens: bigint; }
export interface Usage {
  readonly input_tokens: bigint;
  readonly output_tokens: bigint;
  readonly cache_read_input_tokens?: bigint;
  readonly cache_creation_input_tokens?: bigint;
  readonly input_tokens_details?: InputTokensDetails;
  readonly output_tokens_details?: OutputTokensDetails;
}
export interface ThinkingBlock {
  readonly type: "thinking";
  readonly thinking: string;
  readonly signature?: string;
}
export interface ThinkingDelta { readonly type: "thinking_delta"; readonly text: string; }
export interface SignatureDelta { readonly type: "signature_delta"; readonly signature: string; }
```

Add the thinking block and two delta variants to the corresponding sealed unions; add `reasoning_effort?: ReasoningEffort` to `Params`. Add matching constants and export both types and constants from `ir/index.ts`.

- [ ] **Step 4: Update the IR codecs, validator, and stream normalizer**

Implement the following behavior without coercing optional members to zero:

```ts
function supportedSpecVersion(value: unknown): value is "0.1.0" | "0.2.0" {
  return value === "0.1.0" || value === "0.2.0";
}
```

Use that predicate in `documentRoot()` and `decodeEventStream()`; keep encoder output bound to `specVersion` (`"0.2.0"`). Add encode/decode branches for thinking blocks and deltas, preserving a non-empty signature only when present. Add optional usage detail encode/decode helpers that call `token()` for every supplied value.

Extend the checker’s open-block state with `sawSignature: boolean`. For an open thinking block, allow `thinking_delta` only before `sawSignature`, accept exactly one `signature_delta`, and reject all other deltas. Keep existing tool-fragment validation unchanged.

Update `compareStreams()` so normalized thinking blocks concatenate their thinking deltas, capture at most one signature, and compare all optional usage members/detail values instead of comparing only `input_tokens` and `output_tokens`.

- [ ] **Step 5: Regenerate schema declarations and re-run focused checks**

Run:

```bash
npm run generate
npm run generate:check
npm run build:test
node --test dist-test/test/ir.test.js dist-test/test/constants.test.js
npm run check
```

Expected: all commands succeed; `src/generated/ir-schema.ts` includes schema definitions and document unions for `thinking`, `thinking_delta`, `signature_delta`, and usage details.

- [ ] **Step 6: Commit the independently testable IR change**

```bash
git add ts/src/ir ts/src/vectest/stream.ts ts/src/generated/ir-schema.ts ts/test/ir.test.ts ts/test/constants.test.ts
git commit -m "feat(ts/ir): implement Spec 2.0 reasoning and usage contract"
```

---

### Task 2: Implement Chat Completions reasoning and usage mappings

**Work unit:** `W2-TS-CC`.

- **Input scope:** `spec/10-mapping-openai-chat-completions.md`, the locked Chat Completions vectors, Task 1’s IR API, and only the Chat Completions spoke/test files.
- **Output artifact:** N-CC-12 non-stream and stream mappings for `reasoning_content`, `reasoning_effort`, signature loss, and token details.
- **Completion condition:** Targeted CC tests pass with no regression to M7 tools or live text behavior.
- **Verification:** `npm run build:test && node --test dist-test/test/chatcompletions-stream.test.js dist-test/test/nonstream-vectors.test.js`.
- **Timeout/cancel:** Stop after 10 minutes if a lifecycle failure cannot be reproduced by one focused test; do not change IR or vectors to hide a CC mismatch.
- **Cleanup:** No generated/native capture files; only source and direct regression tests enter the commit.

**Files:**
- Modify: `ts/src/openai/chatcompletions/nonstream.ts`
- Modify: `ts/src/openai/chatcompletions/stream.ts`
- Modify: `ts/src/openai/chatcompletions/index.ts` only if a newly public wire type is introduced
- Modify: `ts/test/chatcompletions-stream.test.ts`
- Modify: `ts/test/nonstream-vectors.test.ts` only for an explicit 2.0 regression assertion

**Interfaces:**
- Consumes `ThinkingBlock`, `ThinkingDelta`, `SignatureDelta`, `ReasoningEffort`, and expanded `Usage` from Task 1.
- Adds optional `reasoning_content?: string` to native assistant message/chunk shapes and optional native detail wire fields.
- Produces one ordered signature loss when a signed IR thinking block cannot be represented by Chat Completions.

- [ ] **Step 1: Write failing mapping and stream tests**

Add regression tests that invoke the public spoke API directly:

```ts
test("decodes assistant reasoning_content and explicit zero cached tokens", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    messages: [
      { role: "user", content: "question" },
      { role: "assistant", reasoning_content: "plan", content: "answer" },
    ],
    reasoning_effort: "high",
  });
  assert.deepEqual(decoded.value.messages[1]?.content[0], {
    type: "thinking", thinking: "plan",
  });
  assert.equal(decoded.value.params?.reasoning_effort, "high");
});

test("encodes a signed thinking block with exactly one signature loss", () => {
  const encoded = encodeResponse({
    id: "chat_1", model: "o3-mini",
    content: [{ type: "thinking", thinking: "plan", signature: "sig" }],
    stop_reason: "end_turn",
    usage: { input_tokens: 1n, output_tokens: 2n },
  });
  const choice = encoded.value.choices as readonly JsonObject[];
  assert.equal(object(choice[0], "choice").message.reasoning_content, "plan");
  assert.deepEqual(encoded.losses.map(({ field, reason }) => ({ field, reason })), [
    { field: "signature", reason: "unmapped-field" },
  ]);
});

test("streams reasoning content before text and preserves usage details", () => {
  const decoder = new ChatCompletionsStreamDecoder();
  const actual = [
    decoder.Feed({
      id: "chat_1", model: "o3-mini",
      choices: [{ delta: { role: "assistant", reasoning_content: "plan" }, finish_reason: null }],
    }),
    decoder.Feed({
      id: "chat_1", model: "o3-mini",
      choices: [{ delta: { content: "answer" }, finish_reason: "stop" }],
      usage: {
        prompt_tokens: 5n, completion_tokens: 8n, total_tokens: 13n,
        prompt_tokens_details: { cached_tokens: 0n },
        completion_tokens_details: { reasoning_tokens: 3n },
      },
    }),
    decoder.Flush(),
  ].flat();
  assert.deepEqual(actual, [
    { type: "message_start", id: "chat_1", model: "o3-mini" },
    { type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "plan" } },
    { type: "content_block_stop", index: 0 },
    { type: "content_block_start", index: 1, block: { type: "text", text: "" } },
    { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "answer" } },
    { type: "content_block_stop", index: 1 },
    { type: "message_delta", stop_reason: "end_turn", usage: {
      input_tokens: 5n, output_tokens: 8n,
      input_tokens_details: { cached_tokens: 0n },
      output_tokens_details: { reasoning_tokens: 3n },
    } },
    { type: "message_done" },
  ]);
});
```

Also assert that unknown inbound `reasoning_effort: "ultra"` yields no IR param and exactly one `unmapped-value` loss.

- [ ] **Step 2: Run focused tests and confirm they fail**

Run:

```bash
npm run build:test && node --test dist-test/test/chatcompletions-stream.test.js dist-test/test/nonstream-vectors.test.js
```

Expected: the new fields are missing from native/IR output and the new M9 assertions fail.

- [ ] **Step 3: Implement non-stream N-CC-12 mapping**

In `nonstream.ts`:

1. Decode an assistant message’s non-empty `reasoning_content` into a `ThinkingBlock` before decoded text/tool blocks.
2. Decode a top-level `reasoning_effort` only for the exact four allowed values. For any other string, add `{ path: "reasoning_effort", field: "reasoning_effort", reason: "unmapped-value" }` and omit the parameter.
3. Encode an assistant/request or response `ThinkingBlock` as `reasoning_content`. Preserve its thinking text. If `signature` is present, emit `{ path: "…signature", field: "signature", reason: "unmapped-field" }` once for that source member.
4. Decode and encode `prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens` whenever their containing detail object is present, including explicit zero. Do not fabricate either detail object when it is absent.

Keep Chat Completions’ existing tool-result/text ordering behavior intact.

- [ ] **Step 4: Implement M9 stream handling without buffering text**

Extend the typed chunk interfaces:

```ts
export interface ChatCompletionsDelta {
  readonly role?: string;
  readonly content?: string;
  readonly reasoning_content?: string;
  readonly tool_calls?: readonly ChatCompletionsToolCallDelta[];
}
export interface ChatCompletionsUsage {
  readonly prompt_tokens: bigint | JsonNumber;
  readonly completion_tokens: bigint | JsonNumber;
  readonly total_tokens: bigint | JsonNumber;
  readonly prompt_tokens_details?: { readonly cached_tokens: bigint | JsonNumber };
  readonly completion_tokens_details?: { readonly reasoning_tokens: bigint | JsonNumber };
}
```

Add a `thinkingOpen`/`thinkingIndex` decoder state. A `reasoning_content` chunk opens a thinking block and emits `thinking_delta`. Before emitting the first text block, close an open thinking block; preserve the existing live text emission semantics. Copy all optional usage detail values after `parseUsageInteger`.

Extend the encoder block state with a `thinking` variant. It emits a chunk delta `{ reasoning_content: event.delta.text }`. A start-block signature and an IR `signature_delta` are each reported only once as standard signature losses; neither may create a malformed native chunk. Terminal chunks serialize optional detail objects exactly when supplied.

- [ ] **Step 5: Re-run focused verification**

Run:

```bash
npm run build:test
node --test dist-test/test/chatcompletions-stream.test.js dist-test/test/nonstream-vectors.test.js
npm run check
```

Expected: all existing CC tests and the new reasoning/usage regressions pass.

- [ ] **Step 6: Commit the Chat Completions spoke**

```bash
git add ts/src/openai/chatcompletions ts/test/chatcompletions-stream.test.ts ts/test/nonstream-vectors.test.ts
git commit -m "feat(ts/chatcompletions): map reasoning content and usage details"
```

---

### Task 3: Implement Responses reasoning items and M9 summary streams

**Work unit:** `W2-TS-RESPONSES`.

- **Input scope:** `spec/11-mapping-openai-responses.md`, all Responses 0.2.0 vectors, Task 1 IR, and only the Responses spoke/test files.
- **Output artifact:** N-R-13 support for request/response reasoning, summary stream lifecycle, `encrypted_content` loss, effort validation, and usage detail fidelity.
- **Completion condition:** Focused Responses tests prove reasoning event identities, `summary_text.done`, no duplicate signature loss, and exact terminal usage detail output.
- **Verification:** `npm run build:test && node --test dist-test/test/responses-nonstream.test.js dist-test/test/responses-stream.test.js`.
- **Timeout/cancel:** Stop after 10 minutes without a focused compile/test cycle; never absorb a supported reasoning event as an unknown event to get green tests.
- **Cleanup:** Keep native event fixtures in tests or vectors only; do not leave logs or temporary generated output.

**Files:**
- Modify: `ts/src/openai/responses/types.ts`
- Modify: `ts/src/openai/responses/nonstream.ts`
- Modify: `ts/src/openai/responses/stream.ts`
- Modify: `ts/src/openai/responses/index.ts`
- Modify: `ts/test/responses-nonstream.test.ts`
- Modify: `ts/test/responses-stream.test.ts`

**Interfaces:**
- Consumes Task 1’s 2.0 IR types.
- Adds `ResponsesReasoningSummaryPart`, optional `summary`/`encrypted_content` on `ResponsesOutputItem`, optional `reasoning` request shape, and optional input/output usage detail shapes.
- Produces typed support for `response.reasoning_summary_part.added`, `.done`, `.text.delta`, and `.text.done` events.

- [ ] **Step 1: Write failing Responses tests**

Replace the current “reasoning item is skipped” test with a supported lifecycle test and add direct non-stream request/response assertions:

```ts
test("decodes a reasoning output item and reports encrypted content loss", () => {
  const decoded = decodeResponse({
    id: "resp_1", model: "o3-mini", status: "completed",
    output: [{
      type: "reasoning", id: "rs_1", status: "completed",
      summary: [{ type: "output_text", text: "Analyze", annotations: [] }],
      encrypted_content: "opaque",
    }],
    usage: { input_tokens: 1n, output_tokens: 2n, total_tokens: 3n },
  });
  assert.deepEqual(decoded.value.content, [{ type: "thinking", thinking: "Analyze" }]);
  assert.deepEqual(decoded.losses.map(({ field, reason }) => ({ field, reason })), [
    { field: "encrypted_content", reason: "unmapped-field" },
  ]);
});

test("decodes reasoning summary text done as a valid terminal summary event", () => {
  const decoder = new ResponsesStreamDecoder();
  const actual = [
    created("resp_1", "o3-mini"),
    { type: "response.output_item.added", output_index: 0,
      item: { type: "reasoning", id: "rs_1", status: "in_progress" } },
    { type: "response.reasoning_summary_part.added", item_id: "rs_1", output_index: 0,
      content_index: 0, part: { type: "output_text", text: "", annotations: [] } },
    { type: "response.reasoning_summary_text.delta", item_id: "rs_1", output_index: 0,
      content_index: 0, delta: "Analyze" },
    { type: "response.reasoning_summary_text.done", item_id: "rs_1", output_index: 0,
      content_index: 0, text: "Analyze" },
    { type: "response.reasoning_summary_part.done", item_id: "rs_1", output_index: 0,
      content_index: 0, part: { type: "output_text", text: "Analyze", annotations: [] } },
    { type: "response.output_item.done", output_index: 0,
      item: { type: "reasoning", id: "rs_1", status: "completed",
        summary: [{ type: "output_text", text: "Analyze", annotations: [] }] } },
    { type: "response.completed", response: {
      id: "resp_1", object: "response", status: "completed", model: "o3-mini", output: [],
      usage: { input_tokens: 2n, output_tokens: 3n, total_tokens: 5n },
    } },
  ].flatMap((event) => decoder.Feed(event));
  assert.deepEqual(actual.slice(1, 4), [
    { type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "Analyze" } },
    { type: "content_block_stop", index: 0 },
  ]);
  assert.deepEqual(decoder.Losses(), []);
});

test("encodes a thinking stream as a reasoning summary item", () => {
  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({ type: "message_start", id: "resp_2", model: "o3-mini" });
  const added = encoder.Apply({
    type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" },
  }).value;
  const delta = encoder.Apply({
    type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "Analyze" },
  }).value;
  const closed = encoder.Apply({ type: "content_block_stop", index: 0 }).value;
  assert.equal(added[0]?.type, "response.output_item.added");
  assert.equal(added[1]?.type, "response.reasoning_summary_part.added");
  assert.equal(delta[0]?.type, "response.reasoning_summary_text.delta");
  assert.deepEqual(closed.map((event) => event.type), [
    "response.reasoning_summary_part.done", "response.output_item.done",
  ]);
});
```

Add an invalid effort test: input `reasoning: { effort: "ultra" }` must not leave a schema-invalid `Params` member and must report `unmapped-value`.

- [ ] **Step 2: Run Responses tests to prove expected failure**

Run:

```bash
npm run build:test && node --test dist-test/test/responses-nonstream.test.js dist-test/test/responses-stream.test.js
```

Expected: current tests show reasoning as unsupported and new typed event fields/types are absent.

- [ ] **Step 3: Extend typed wire representations and non-stream conversion**

In `types.ts`, define a strict `ResponsesReasoningSummaryPart` with `type: "output_text"`, `text`, and `annotations`, then allow a separate reasoning item shape carrying `summary?: readonly ResponsesReasoningSummaryPart[]` and `encrypted_content?: string`. Extend `ResponsesUsage` with optional `input_token_details.cached_tokens` and `output_token_details.reasoning_tokens` values.

In `nonstream.ts`:

1. Decode `reasoning.effort` using the same four-value predicate; convert it to `params.reasoning_effort`; report unknown effort as one `unmapped-value` loss and `reasoning.summary` preference as `unmapped-field`.
2. Decode assistant request `input` items of `type: "reasoning"` into a preceding `ThinkingBlock` from each summary text part.
3. Encode assistant `ThinkingBlock`s into typed `input` reasoning items with output-text summaries; record a signature loss because Responses request summaries have no signature field.
4. Decode output reasoning summary parts into `ThinkingBlock`s in native output order. For a present `encrypted_content`, emit the required `unmapped-field` loss. Encode response thinking blocks as completed reasoning output items; emit one signature loss for a supplied signature.
5. Decode/encode optional usage detail objects without treating zero as absence.

- [ ] **Step 4: Implement the Responses M9 stream state machines**

In `ResponsesStreamDecoder`, add a reasoning item state distinct from message and function-call state. It must:

- accept `response.output_item.added` for `item.type === "reasoning"`;
- accept `response.reasoning_summary_part.added` only for the active reasoning item, create `ThinkingBlock { thinking: part.text ?? "" }`, and allocate the next contiguous IR index;
- emit `thinking_delta` for `response.reasoning_summary_text.delta`;
- treat `response.reasoning_summary_text.done` as validation-only (no duplicate delta/loss), then close on `response.reasoning_summary_part.done`;
- validate item ID, output index, content index, and part lifecycle exactly as the existing message-part path does;
- reject any reasoning text delta after a completed part and retain current unknown-event containment for genuinely unsupported units.

In `ResponsesStreamEncoder`, add a `reasoning` item and a `thinking` active-block variant. A thinking block start emits `response.output_item.added` with a `reasoning` item followed by `response.reasoning_summary_part.added`; a thinking delta emits `response.reasoning_summary_text.delta`; stop emits summary-part done and output-item done. Include the completed summary in the terminal response output. If the source start or delta carries a signature, report the unmapped signature exactly once per source member and never emit a duplicate loss during stop. Preserve existing text/function-call item transition rules.

Use a shared local usage conversion helper so terminal `response.completed` includes input/output detail objects when and only when they exist.

- [ ] **Step 5: Verify the spoke and its lifecycle boundaries**

Run:

```bash
npm run build:test
node --test dist-test/test/responses-nonstream.test.js dist-test/test/responses-stream.test.js
npm run check
```

Expected: all Responses tests pass, including the native `.reasoning_summary_text.done` case and M7 tests.

- [ ] **Step 6: Commit the Responses spoke**

```bash
git add ts/src/openai/responses ts/test/responses-nonstream.test.ts ts/test/responses-stream.test.ts
git commit -m "feat(ts/responses): implement reasoning summaries and usage details"
```

---

### Task 4: Implement Anthropic thinking, budget mapping, and stream signatures

**Work unit:** `W2-TS-ANTHROPIC`.

- **Input scope:** `spec/12-mapping-anthropic-messages.md`, Anthropic 0.2.0 vectors, Task 1 IR, and only Anthropic spoke/test files.
- **Output artifact:** N-AN-11 mappings for native thinking/signatures, budget approximation, cache token fields, and canonical M9 stream start/delta/stop behavior.
- **Completion condition:** Focused tests prove signature opacity, all documented budget thresholds/losses, cache usage presence/zero behavior, and no duplicate synthetic stream delta.
- **Verification:** `npm run build:test && node --test dist-test/test/anthropic-nonstream.test.js dist-test/test/anthropic-stream.test.js`.
- **Timeout/cancel:** Stop after 10 minutes without a passing focused test cycle; do not parse a signature or reinterpret an opaque tool JSON token.
- **Cleanup:** Keep only source/test changes; discard generated test output and any manual fixture capture.

**Files:**
- Modify: `ts/src/anthropic/messages/types.ts`
- Modify: `ts/src/anthropic/messages/nonstream.ts`
- Modify: `ts/src/anthropic/messages/stream.ts`
- Modify: `ts/src/anthropic/messages/index.ts`
- Modify: `ts/test/anthropic-nonstream.test.ts`
- Modify: `ts/test/anthropic-stream.test.ts`

**Interfaces:**
- Consumes the 2.0 IR API from Task 1.
- Adds typed `thinking?: string`, `signature?: string`, native cache usage fields, request `thinking` configuration, and stream delta `signature?: string`.
- Produces native `thinking` blocks with unmodified signatures and a documented budget mapping helper local to the Anthropic spoke.

- [ ] **Step 1: Write failing Anthropic conversion tests**

Add tests for exact signature preservation, budget approximation, and canonical stream output:

```ts
test("maps an Anthropic thinking response block and cache usage", () => {
  const decoded = decodeResponse({
    id: "msg_1", type: "message", role: "assistant", model: "claude",
    content: [{ type: "thinking", thinking: "consider", signature: "opaque-sig" }],
    stop_reason: "end_turn",
    usage: { input_tokens: 4n, output_tokens: 2n, cache_read_input_tokens: 3n },
  });
  assert.deepEqual(decoded.value.content, [{
    type: "thinking", thinking: "consider", signature: "opaque-sig",
  }]);
  assert.equal(decoded.value.usage.cache_read_input_tokens, 3n);
});

test("maps reasoning effort to the documented Anthropic budget with degradation", () => {
  const encoded = encodeRequest({
    model: "claude", messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    params: { max_tokens: 64n, reasoning_effort: "medium" },
  });
  assert.equal(object(encoded.value.thinking, "thinking").budget_tokens, 8192n);
  assert.deepEqual(encoded.losses.map(({ reason }) => reason), ["degraded"]);
});

test("encodes a full thinking start as an empty native placeholder plus synthesized deltas", () => {
  const encoder = new AnthropicStreamEncoder();
  encoder.Apply({ type: "message_start", id: "msg_2", model: "claude" });
  const start = encoder.Apply({
    type: "content_block_start", index: 0,
    block: { type: "thinking", thinking: "consider", signature: "opaque-sig" },
  }).value;
  const stop = encoder.Apply({ type: "content_block_stop", index: 0 }).value;
  assert.deepEqual(start, [{
    type: "content_block_start", index: 0,
    content_block: { type: "thinking", thinking: "" },
  }]);
  assert.deepEqual(stop, [
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "consider" } },
    { type: "content_block_delta", index: 0, delta: { type: "signature_delta", signature: "opaque-sig" } },
    { type: "content_block_stop", index: 0 },
  ]);
});
```

Add assertions for decode thresholds `<= 2048 → low`, `<= 8192 → medium`, and `> 8192 → high`, each reporting the required degradation loss.

- [ ] **Step 2: Run focused tests and confirm failure**

Run:

```bash
npm run build:test && node --test dist-test/test/anthropic-nonstream.test.js dist-test/test/anthropic-stream.test.js
```

Expected: thinking is currently an unsupported content block and the new native fields/events are unavailable.

- [ ] **Step 3: Extend native types and non-stream N-AN-11 mappings**

Add optional `thinking` and `signature` fields to `AnthropicContentBlock`; add optional `cache_read_input_tokens` and `cache_creation_input_tokens` to `AnthropicUsage`; add optional `thinking` configuration to the request wire shape; and add `signature` to `AnthropicStreamDelta`.

In `nonstream.ts`:

1. Decode native `type: "thinking"` blocks directly to `ThinkingBlock`, preserving both strings exactly.
2. Encode `ThinkingBlock` directly to a native thinking block. When request-side replay lacks a signature, keep the native block unsigned and emit the documented `degraded` signature loss.
3. Implement local, total helpers for the exact mapping: encode `minimal → 1024`, `low → 2048`, `medium → 8192`, `high → 16384`; decode `budget_tokens <= 2048 → low`, `<= 8192 → medium`, otherwise `high`. Every conversion using this table emits exactly one `degraded` loss with the appropriate source path/field.
4. Decode and encode cache read/creation usage fields only if supplied. Do not manufacture OpenAI-style detail fields for Anthropic.

- [ ] **Step 4: Extend the M9 stream decoder and encoder**

Modify `AnthropicStreamDecoder` so `content_block_start` with `type: "thinking"` creates an IR `ThinkingBlock` preserving its optional start signature. For an open thinking block, accept `thinking_delta` as `ThinkingDelta` and `signature_delta` as `SignatureDelta`; enforce the Task 1 ordering through local lifecycle checks before emitting. Keep native indexes separate from compacted IR indexes exactly as existing skipped-block logic does.

Modify `AnthropicStreamEncoder` with a `thinking` block state that records full start text/signature plus received deltas. On start, emit the canonical placeholder:

```ts
{
  type: "content_block_start",
  index: event.index,
  content_block: { type: "thinking", thinking: "" },
}
```

Emit native `thinking_delta`/`signature_delta` for supplied IR deltas. On stop, synthesize one `thinking_delta` for non-empty start thinking and one `signature_delta` for start signature only if a corresponding delta was not emitted. Then emit the block stop. Terminal message usage carries cache fields whenever present.

- [ ] **Step 5: Re-run focused Anthropic validation**

Run:

```bash
npm run build:test
node --test dist-test/test/anthropic-nonstream.test.js dist-test/test/anthropic-stream.test.js
npm run check
```

Expected: all existing M7 raw-tool tests remain green and new M9/budget/cache tests pass.

- [ ] **Step 6: Commit the Anthropic spoke**

```bash
git add ts/src/anthropic/messages ts/test/anthropic-nonstream.test.ts ts/test/anthropic-stream.test.ts
git commit -m "feat(ts/anthropic): map thinking blocks, budgets, and cache usage"
```

---

### Task 5: Activate and verify every Spec 2.0 golden vector

**Work unit:** `W2-TS-VECTORS`.

- **Input scope:** The immutable `vectors/` tree and manifest, Task 1–4 implementations, `ts/src/vectest/`, and new TypeScript vector tests only.
- **Output artifact:** The TypeScript loader consumes both contract versions and Node tests validate all non-stream and stream vectors, including every M9 from-IR/to-IR vector.
- **Completion condition:** The new loader test asserts 154 shared vectors, all TypeScript vector tests pass, and Go `veccheck` reports no manifest drift.
- **Verification:** `npm test`, `cd ../go && go run ./cmd/veccheck -root .. -check-manifest`, then `python3 scripts/check-constants.py` from repository root.
- **Timeout/cancel:** Stop after 10 minutes if any failing vector cannot be reduced to its named fixture and corresponding spoke; never edit locked vectors/manifest as a workaround.
- **Cleanup:** Do not modify `vectors/manifest.json`; remove no vectors; test helper output remains in `dist-test/` only.

**Files:**
- Modify: `ts/src/vectest/load.ts`
- Create: `ts/test/spec-v2-stream-vectors.test.ts`
- Modify: `ts/test/vectest.test.ts`
- Modify: `ts/test/nonstream-vector-runner.ts` only if it needs type-safe 2.0 adapter widening

**Interfaces:**
- `loadVectors(root)` returns every vector file in deterministic order, including `spec_version: "0.1.0"` and `"0.2.0"`.
- The new test consumes public stream decoders/encoders and compares normalised IR streams plus exact native output/losses.
- Does not alter `vectors/` or the Go vector checker.

- [ ] **Step 1: Write failing full-vector coverage tests**

In `vectest.test.ts`, replace the broad `> 100` assertion with exact checks:

```ts
const vectors = loadVectors(root);
assert.equal(vectors.length, 154);
assert.equal(vectors.filter((v) => v.document.spec_version === "0.2.0").length, 24);
assert.ok(vectors.some((v) => v.name === "responses.stream.m9-reasoning-summary-from-ir"));
```

Create `spec-v2-stream-vectors.test.ts`. It must enumerate `document.mode === "stream"`, dispatch the named face to its public stream encoder/decoder, aggregate `Feed`/`Flush` or `Apply` output, and compare expected losses. The test must use `fromValue()` before `compareJson()` so native `bigint` token fields compare without precision loss.

For to-IR vectors, decode native event objects, wrap events as `{ events }`, and call `compareStreams(decodeEventStream(expected_ir), actual)`; for from-IR vectors, call `decodeEventStream(input)`, collect native events, and compare `{ events: fromValue(nativeEvents) }` to `expected_output` exactly.

- [ ] **Step 2: Run the vector tests to prove the intentional baseline filter fails**

Run:

```bash
npm run build:test && node --test dist-test/test/vectest.test.js dist-test/test/spec-v2-stream-vectors.test.js
```

Expected: count assertion fails at `130`; stream tests expose the missing 2.0 mappings until Tasks 1–4 are complete.

- [ ] **Step 3: Remove only the temporary 0.1.0 filter**

Change `loadVectors()` from:

```ts
.map((path) => loadVector(path, root))
.filter((v) => v.document.spec_version === "0.1.0");
```

to:

```ts
.map((path) => loadVector(path, root));
```

Keep deterministic path sorting and all vector-name/type validation unchanged. If the non-stream adapter types need a wider `Request`/`Response` surface, update only their imports/types; do not weaken JSON comparison or loss matching.

- [ ] **Step 4: Implement the stream-vector dispatcher**

In the new test file, define an explicit face table rather than cross-face imports:

```ts
const streams = {
  chatcompletions: { decode: ChatCompletionsStreamDecoder, encode: ChatCompletionsStreamEncoder },
  responses: { decode: ResponsesStreamDecoder, encode: ResponsesStreamEncoder },
  anthropic: { decode: AnthropicStreamDecoder, encode: AnthropicStreamEncoder },
} as const;
```

Use each constructor only in the test module. Preserve source event order, call `Flush()` exactly once after every decode input, and append `Losses()` after output collection. Assert vector names in failure messages. Add targeted M9 assertions covering a signature delta after thinking text and detailed terminal usage fields; the generic loop remains the proof that every stream vector is exercised.

- [ ] **Step 5: Run the complete vector and cross-language gate set**

Run from `ts/`:

```bash
npm test
```

Then run from the repository root:

```bash
cd ..
python3 scripts/check-constants.py
cd go && go run ./cmd/veccheck -root .. -check-manifest
```

Expected: TypeScript consumes all 154 vectors with no filter; constant convergence recognizes TypeScript’s active 0.2.0 tokens; manifest validation passes unchanged.

- [ ] **Step 6: Commit vector-harness activation separately**

```bash
git add ts/src/vectest/load.ts ts/test/vectest.test.ts ts/test/spec-v2-stream-vectors.test.ts ts/test/nonstream-vector-runner.ts
git commit -m "test(ts): validate complete Spec 2.0 golden vector set"
```

---

### Task 6: Synchronize TypeScript capability documentation and verify release readiness

**Work unit:** `W2-TS-DOCS-VERIFY`.

- **Input scope:** The committed Wave 2 code/tests, `ts/README.md`, `spec/README.md`, and only root README cells that claim TypeScript’s current implementation coverage.
- **Output artifact:** Accurate documentation that TypeScript joins Go on the 154-vector 2.0 contract, plus a recorded clean local verification result. It deliberately does not alter package versions, changelog release dates, tags, GitHub Releases, or registries.
- **Completion condition:** Documentation makes no false all-language claim; all local TypeScript quality/package checks and repository validation commands pass.
- **Verification:** `npm run generate:check`, `npm run fmt`, `npm run lint`, `npm run test:web`, `npm run test:package`, `npm run test:consumer`, `npm run release:check`, and root checks listed below.
- **Timeout/cancel:** Each verification command has a 10-minute maximum. Stop on first failure, preserve its output, and repair the relevant implementation/documentation before proceeding; do not tag or publish.
- **Cleanup:** Release/package test temporary directories are managed by existing scripts; verify `git status --short` is limited to intended documentation changes before committing.

**Files:**
- Modify: `ts/README.md`
- Modify: `spec/README.md`
- Modify: `README.md` only if it still says TypeScript lacks reasoning/M9 or gives an obsolete vector count
- Create: no release files

**Interfaces:**
- No runtime API changes.
- Documentation must say TypeScript and Go validate 154 vectors; Rust, Python, and C++ remain on the 130 baseline until Waves 3–5.

- [ ] **Step 1: Update capability wording without changing release state**

In `ts/README.md`, replace the obsolete claim:

```markdown
The TypeScript implementation passes the same 125 golden vectors as the Go reference implementation, including exact M7 raw tool-data handling.
```

with wording that names the actual scope:

```markdown
The TypeScript implementation validates all 154 golden vectors for Spec 2.0.0, including M7 opaque tool data, M9 thinking/reasoning streams, signatures, reasoning effort, and granular usage accounting.
```

In `spec/README.md`, update the current-status sentence to state that Go **and TypeScript** implement the full 154-vector set, while Rust/Python/C++ remain on 130 baseline vectors pending Waves 3–5. Update root `README.md` only if its TypeScript matrix/count is no longer true. Leave every package/repository version and release-status line unchanged.

- [ ] **Step 2: Check formatting, types, runtime targets, package contents, and consumers**

Run from `ts/`:

```bash
npm run generate:check
npm run fmt
npm run lint
npm test
npm run test:web
npm run test:package
npm run test:consumer
npm run release:check
```

Expected: all commands pass. `release:check` is verification only and must not publish anything.

- [ ] **Step 3: Run repository-level final gates**

Run from repository root:

```bash
make vectors
make lint
make fmt
make check-modulepath
make test
python3 scripts/check-constants.py
cd go && go run ./cmd/veccheck -root .. -check-manifest
```

Expected: all commands pass. If local `-race` is unavailable because CGO is disabled, record that environment limitation but do not treat it as a substitute for the normal CI race jobs.

- [ ] **Step 4: Commit documentation only**

```bash
git add ts/README.md spec/README.md README.md
git diff --cached --check
git commit -m "docs: mark TypeScript Spec 2.0 vector support complete"
```

If root `README.md` did not require a change, omit it from `git add`; do not create an empty documentation commit.

---

## Plan self-review

- **Spec coverage:** Task 1 covers the sealed IR variants, dual-read versioning, M9 grammar, and usage shape. Task 2 covers N-CC-12. Task 3 covers N-R-13 including `encrypted_content` and `reasoning_summary_text.done`. Task 4 covers N-AN-11, budget thresholds, signatures, and cache usage. Task 5 activates every locked 0.2.0 vector. Task 6 updates only the now-true TypeScript documentation and runs release-readiness checks without publishing.
- **Scope:** No provider HTTP client, routing, authentication, model capability database, direct face-to-face conversion, vector/spec rewrite, package version bump, tag, release, or registry publication is included.
- **Type consistency:** All spoke tasks consume the exported Task 1 IR types. All native usage values remain `bigint | JsonNumber`, then pass through `parseUsageInteger` / `encodeUsageInteger`. Stream comparison uses `fromValue()` to retain int64 fidelity.
- **Placeholder scan:** This plan contains no unresolved placeholder, deferred implementation instruction, or unscoped “appropriate handling” step.
