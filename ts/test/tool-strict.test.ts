import assert from "node:assert/strict";
import test from "node:test";

import { decodeRequest, encodeRequest, type Request } from "../src/ir/index.js";
import * as chat from "../src/openai/chatcompletions/index.js";
import * as responses from "../src/openai/responses/index.js";
import * as anthropic from "../src/anthropic/messages/index.js";

const message = {
  role: "user",
  content: [{ type: "text", text: "hi" }],
} as const;

test("canonical tool strict preserves omission, false and true", () => {
  const tools = [
    { name: "omitted", input_schema: {} },
    { name: "non_strict", input_schema: {}, strict: false },
    { name: "strict", input_schema: {}, strict: true },
  ];
  const request = decodeRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [message],
    tools,
  });
  assert.deepEqual(encodeRequest(request).tools, tools);
});

test("OpenAI raw requests normalize null strict and reject other non-booleans", () => {
  for (const [decode, encode, expectedTool, wire] of [
    [
      chat.decodeRequestJSON,
      responses.encodeRequest,
      { type: "function", name: "f", parameters: {} },
      (strict: unknown) => ({
        model: "m",
        messages: [{ role: "user", content: "hi" }],
        tools: [
          { type: "function", function: { name: "f", parameters: {}, strict } },
        ],
      }),
    ],
    [
      responses.decodeRequestJSON,
      chat.encodeRequest,
      { type: "function", function: { name: "f", parameters: {} } },
      (strict: unknown) => ({
        model: "m",
        input: "hi",
        tools: [{ type: "function", name: "f", parameters: {}, strict }],
      }),
    ],
  ] as const) {
    const result = decode(JSON.stringify(wire(null)));
    assert.equal(result.value.tools?.[0]?.strict, undefined);
    assert.deepEqual(result.losses, []);
    const encoded = encode(result.value);
    assert.deepEqual(encoded.value.tools, [expectedTool]);
    assert.deepEqual(encoded.losses, []);
    for (const value of [0, "true", {}, []]) {
      assert.throws(() => decode(JSON.stringify(wire(value))));
    }
  }
});

test("direct non-function strict is rejected by all request encoders", () => {
  for (const tool of [
    { type: "custom", name: "x", strict: false },
    { type: "apply_patch", strict: false },
    {
      type: "native",
      name: "x",
      source: "s",
      definition: '{"type":"provider_tool","name":"x"}',
      strict: false,
    },
  ]) {
    const request = {
      model: "m",
      messages: [message],
      tools: [tool],
    } as unknown as Request;
    for (const encoder of [
      encodeRequest,
      chat.encodeRequest,
      responses.encodeRequest,
      anthropic.encodeRequest,
    ]) {
      assert.throws(() => encoder(request), tool.type);
    }
  }
});

test("canonical non-function tools reject strict", () => {
  for (const tool of [
    { type: "custom", name: "x", strict: false },
    { type: "apply_patch", strict: true },
    {
      type: "native",
      name: "x",
      source: "s",
      definition: '{"type":"provider_tool","name":"x"}',
      strict: false,
    },
  ]) {
    assert.throws(() =>
      decodeRequest({
        specVersion: "0.2.0",
        model: "m",
        messages: [message],
        tools: [tool],
      }),
    );
  }
});
