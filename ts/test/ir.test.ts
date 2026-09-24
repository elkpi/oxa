import assert from "node:assert/strict";
import test from "node:test";

import { OxaError } from "../src/error.js";
import {
  assertEventSequence,
  decodeEventStream,
  decodeRequest,
  encodeEventStream,
  encodeRequest,
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

test("emits 0.2.0 while accepting a 0.1.0 IR request", () => {
  const request = {
    model: "m",
    messages: [
      { role: "user" as const, content: [{ type: "text" as const, text: "hi" }] },
    ],
  };
  assert.deepEqual(decodeRequest({ specVersion: "0.1.0", ...request }), request);
  assert.equal(encodeRequest(request).specVersion, "0.2.0");
});

test("round-trips a signed thinking stream and usage details", () => {
  const stream = {
    events: [
      { type: "message_start", id: "m", model: "model" },
      {
        type: "content_block_start",
        index: 0,
        block: { type: "thinking", thinking: "" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "thinking_delta", text: "reason" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "signature_delta", signature: "sig" },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "message_delta",
        stop_reason: "end_turn",
        usage: {
          input_tokens: 2n,
          output_tokens: 3n,
          input_tokens_details: { cached_tokens: 1n },
          output_tokens_details: { reasoning_tokens: 2n },
        },
      },
      { type: "message_done" },
    ],
  } as const;
  assert.deepEqual(decodeEventStream(encodeEventStream(stream)), stream);
});

test("rejects a thinking delta after its signature", () => {
  const events = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "thinking", thinking: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "signature_delta", signature: "sig" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "thinking_delta", text: "late" },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ] as const;
  assert.throws(() => assertEventSequence(events), { code: "ir-invariant" });
});

test("accepts a start signature followed by one signature delta", () => {
  const events = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "thinking", thinking: "Thinking carefully.", signature: "sig_abc" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "thinking_delta", text: "more" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "signature_delta", signature: "sig_abc" },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ] as const;
  assert.doesNotThrow(() => assertEventSequence(events));
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
    '{"specVersion":"0.2.0","events":[{"type":"message_start","id":"m","model":"model"},{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":0}},{"type":"message_done"}]}',
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

test("accepts a tool block without argument fragments for encoder synthesis", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "tool_1",
        name: "lookup",
        input: jsonText('{"city":"Paris"}'),
      },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 1n, output_tokens: 2n },
    },
    { type: "message_done" },
  ];

  assert.doesNotThrow(() => assertEventSequence(events));
});
