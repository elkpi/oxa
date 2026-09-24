import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import {
  compareJson,
  compareLosses,
  compareStreams,
  findRepoRoot,
  loadVectors,
} from "../src/vectest/index.js";
import { jsonText, parseJson } from "../src/json/index.js";

test("distinguishes integers while normalizing decimal spelling", () => {
  assert.notEqual(compareJson(parseJson("1"), parseJson("1.0")), undefined);
  assert.equal(compareJson(parseJson("0.50"), parseJson("0.5")), undefined);
  assert.equal(compareJson(parseJson("1e+01"), parseJson("10.0")), undefined);
});

test("compares losses as unordered semantic sets", () => {
  const left = [
    {
      path: "output[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail: "source wording",
    },
    { path: "params", field: "top_k", reason: "unmapped-field" },
  ] as const;
  const right = [
    { path: "params", field: "top_k", reason: "unmapped-field" },
    {
      path: "output[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail: "other wording",
    },
  ] as const;

  assert.equal(compareLosses(left, right), undefined);
  assert.notEqual(compareLosses(left, right.slice(1)), undefined);
});

test("normalizes equivalent text and tool stream chunking", () => {
  const first = {
    events: [
      { type: "message_start", id: "m", model: "model" },
      {
        type: "content_block_start",
        index: 0,
        block: { type: "text", text: "" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "text_delta", text: "Hel" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "text_delta", text: "lo" },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "content_block_start",
        index: 1,
        block: {
          type: "tool_use",
          id: "call_1",
          name: "weather",
          input: jsonText('{"city":"Paris"}'),
        },
      },
      {
        type: "content_block_delta",
        index: 1,
        delta: { type: "input_json_delta", partial_json: jsonText('{"city":') },
      },
      {
        type: "content_block_delta",
        index: 1,
        delta: { type: "input_json_delta", partial_json: jsonText('"Paris"}') },
      },
      { type: "content_block_stop", index: 1 },
      {
        type: "message_delta",
        stop_reason: "tool_use",
        usage: { input_tokens: 3n, output_tokens: 2n },
      },
      { type: "message_done" },
    ],
  } as const;
  const second = {
    events: [
      { type: "message_start", id: "m", model: "model" },
      {
        type: "content_block_start",
        index: 0,
        block: { type: "text", text: "" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "text_delta", text: "Hello" },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "content_block_start",
        index: 1,
        block: {
          type: "tool_use",
          id: "call_1",
          name: "weather",
          input: jsonText('{"city":"Paris"}'),
        },
      },
      {
        type: "content_block_delta",
        index: 1,
        delta: {
          type: "input_json_delta",
          partial_json: jsonText('{"city":"Paris"}'),
        },
      },
      { type: "content_block_stop", index: 1 },
      {
        type: "message_delta",
        stop_reason: "tool_use",
        usage: { input_tokens: 3n, output_tokens: 2n },
      },
      { type: "message_done" },
    ],
  } as const;

  assert.equal(compareStreams(first, second), undefined);
});

test("finds and loads repository vectors without native JSON coercion", () => {
  const root = findRepoRoot(process.cwd());
  assert.equal(root, resolve(process.cwd(), ".."));
  assert.ok(root !== undefined);
  const vectors = loadVectors(root);
  assert.equal(vectors.length, 154);
  assert.equal(
    vectors.filter((vector) => vector.document.spec_version === "0.2.0")
      .length,
    24,
  );
  assert.ok(
    vectors.some(
      (vector) => vector.name === "responses.stream.m9-reasoning-summary-from-ir",
    ),
  );
  assert.ok(
    vectors.some(
      (vector) => vector.name === "chatcompletions.stream.m7-tool-calls-to-ir",
    ),
  );
});
