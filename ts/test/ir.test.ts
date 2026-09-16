import assert from "node:assert/strict";
import test from "node:test";

import { OxaError } from "../src/error.js";
import {
  assertEventSequence,
  decodeRequest,
  type Event,
} from "../src/ir/index.js";

test("IR request decoding rejects empty message content", () => {
  assert.throws(
    () =>
      decodeRequest({
        specVersion: "0.1.0",
        model: "model",
        messages: [{ role: "user", content: [] }],
      }),
    (error: unknown) =>
      error instanceof OxaError && error.code === "invalid-input",
  );
});

test("rejects a delta before its block start", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "x" },
    },
  ];

  assert.throws(
    () => assertEventSequence(events),
    (error: unknown) =>
      error instanceof OxaError && error.code === "ir-invariant",
  );
});

test("accepts contiguous text block events", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "x" },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ];

  assert.doesNotThrow(() => assertEventSequence(events));
});

import { decodeEventStream, encodeEventStream } from "../src/ir/index.js";
import { jsonText, stringifyJson } from "../src/json/index.js";

test("encodes and decodes an event stream with integer usage tokens", () => {
  const stream = {
    events: [
      { type: "message_start", id: "m", model: "model" },
      {
        type: "message_delta",
        stop_reason: "end_turn",
        usage: { input_tokens: 1n, output_tokens: 0n },
      },
      { type: "message_done" },
    ],
  } as const;

  const document = encodeEventStream(stream);
  assert.equal(
    stringifyJson(document),
    '{"specVersion":"0.1.0","events":[{"type":"message_start","id":"m","model":"model"},{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":0}},{"type":"message_done"}]}',
  );
  assert.deepEqual(decodeEventStream(document), stream);
});

test("round-trips text block events through the IR JSON codec", () => {
  const stream = {
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
        delta: { type: "text_delta", text: "hello" },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "message_delta",
        stop_reason: "end_turn",
        usage: { input_tokens: 1n, output_tokens: 2n },
      },
      { type: "message_done" },
    ],
  } as const;

  assert.deepEqual(decodeEventStream(encodeEventStream(stream)), stream);
});

test("round-trips M7 tool JSON fragments without reformatting", () => {
  const stream = {
    events: [
      { type: "message_start", id: "m", model: "model" },
      {
        type: "content_block_start",
        index: 0,
        block: {
          type: "tool_use",
          id: "tool_1",
          name: "lookup",
          input: jsonText('{"a":1.0}'),
        },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: jsonText('{"a":') },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: jsonText("1.0}") },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "message_delta",
        stop_reason: "tool_use",
        usage: { input_tokens: 1n, output_tokens: 2n },
      },
      { type: "message_done" },
    ],
  } as const;

  assert.deepEqual(decodeEventStream(encodeEventStream(stream)), stream);
});

test("rejects tool input whose raw fragments do not match", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "tool_1",
        name: "lookup",
        input: jsonText("{}"),
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "input_json_delta", partial_json: jsonText("x") },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 1n, output_tokens: 2n },
    },
    { type: "message_done" },
  ];

  assert.throws(
    () => assertEventSequence(events),
    (error: unknown) =>
      error instanceof OxaError && error.code === "ir-invariant",
  );
});
