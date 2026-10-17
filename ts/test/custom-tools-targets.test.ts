import assert from "node:assert/strict";
import test from "node:test";
import type { Loss } from "../src/loss.js";
import { integer, jsonText } from "../src/json/index.js";

import {
  decodeRequest as decodeAnthropicRequest,
  encodeRequest as encodeAnthropicRequest,
  encodeResponse as encodeAnthropicResponse,
  AnthropicStreamEncoder,
  type AnthropicStreamEvent,
} from "../src/anthropic/messages/index.js";
import {
  decodeRequest as decodeIrRequest,
  type Event,
  type Request,
  type Response,
} from "../src/ir/index.js";
import {
  decodeRequest as decodeChatRequest,
  encodeRequest as encodeChatRequest,
  encodeResponse as encodeChatResponse,
  ChatCompletionsStreamEncoder,
  type ChatCompletionsChunk,
} from "../src/openai/chatcompletions/index.js";

const usage = { input_tokens: 1n, output_tokens: 2n };
const customResponse: Response = {
  id: "r",
  model: "m",
  content: [
    { type: "custom_tool_use", id: "call_1", name: "run", input: "raw" },
  ],
  stop_reason: "tool_use",
  usage,
};

test("Chat and Anthropic support custom declarations, choices and responses", () => {
  const request = decodeIrRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "go" }] }],
    tools: [{ type: "custom", name: "run" }],
    params: { max_tokens: integer(32n) },
    tool_choice: { mode: "tool", name: "run", tool_type: "custom" },
  });
  const chatReq = encodeChatRequest(request);
  assert.equal(chatReq.losses.length, 0);
  assert.ok(chatReq.value.tools !== undefined);
  assert.ok(chatReq.value.tool_choice !== undefined);

  const anthropicReq = encodeAnthropicRequest(request);
  assert.equal(anthropicReq.losses.length, 0);
  assert.ok(anthropicReq.value.tools !== undefined);
  assert.ok(anthropicReq.value.tool_choice !== undefined);

  const chat = encodeChatResponse(customResponse);
  assert.equal(
    (chat.value.choices as readonly { finish_reason: string }[])[0]
      ?.finish_reason,
    "tool_calls",
  );
  assert.equal(chat.losses.length, 0);

  const anthropic = encodeAnthropicResponse(customResponse);
  assert.equal(anthropic.value.stop_reason, "tool_use");
  assert.equal(anthropic.losses.length, 0);
});

test("Chat and Anthropic drop apply_patch declarations and choices with explicit losses", () => {
  const request = decodeIrRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "go" }] }],
    tools: [{ type: "apply_patch" }],
    params: { max_tokens: integer(32n) },
    tool_choice: { mode: "tool", tool_type: "apply_patch" },
  });
  for (const encoded of [
    encodeChatRequest(request),
    encodeAnthropicRequest(request),
  ]) {
    assert.deepEqual(
      encoded.losses.map(({ reason }) => reason),
      ["unsupported-semantic", "unsupported-semantic"],
    );
    assert.equal(encoded.value.tools, undefined);
    assert.equal(encoded.value.tool_choice, undefined);
  }
});

test("unsupported-target stream encoders absorb apply_patch deltas and compact retained indexes", () => {
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "apply_patch_tool_use",
        id: "p",
        status: "in_progress",
        operation: { type: "create_file", path: "a.txt", diff: "" },
      },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "content_block_start",
      index: 1,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 1,
      delta: { type: "text_delta", text: "kept" },
    },
    { type: "content_block_stop", index: 1 },
    {
      type: "content_block_start",
      index: 2,
      block: {
        type: "tool_use",
        id: "function_call",
        name: "lookup",
        input: jsonText("{}"),
      },
    },
    { type: "content_block_stop", index: 2 },
    { type: "message_delta", stop_reason: "tool_use", usage },
    { type: "message_done" },
  ];
  const chat = new ChatCompletionsStreamEncoder();
  const chatOutput: ChatCompletionsChunk[] = [];
  const chatLosses: Loss[] = [];
  for (const event of events) {
    const result = chat.Apply(event);
    chatOutput.push(...result.value);
    chatLosses.push(...result.losses);
  }
  assert.equal(
    chatLosses.filter((loss) => loss.reason === "unsupported-semantic").length,
    1,
  );
  assert.equal(
    chatOutput.flatMap((chunk) =>
      chunk.choices.flatMap((choice) => choice.delta.tool_calls ?? []),
    )[0]?.index,
    0,
  );
  assert.equal(chatOutput.at(-1)?.choices[0]?.finish_reason, "tool_calls");

  const anthropic = new AnthropicStreamEncoder();
  const anthropicOutput: AnthropicStreamEvent[] = [];
  const anthropicLosses: Loss[] = [];
  for (const event of events) {
    const result = anthropic.Apply(event);
    anthropicOutput.push(...result.value);
    anthropicLosses.push(...result.losses);
  }
  assert.equal(
    anthropicLosses.filter((loss) => loss.reason === "unsupported-semantic")
      .length,
    1,
  );
  assert.deepEqual(
    anthropicOutput
      .filter((event) => event.type === "content_block_start")
      .map((event) => event.index),
    [0, 1],
  );
  assert.equal(
    anthropicOutput.find((event) => event.type === "message_delta")?.delta
      ?.stop_reason,
    "tool_use",
  );
});

test("apply_patch only unsupported streams normalize tool_use to end_turn with degraded loss", () => {
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "apply_patch_tool_use",
        id: "p",
        status: "in_progress",
        operation: { type: "create_file", path: "a.txt", diff: "" },
      },
    },
    { type: "content_block_stop", index: 0 },
    { type: "message_delta", stop_reason: "tool_use", usage },
    { type: "message_done" },
  ];
  const chat = new ChatCompletionsStreamEncoder();
  const chatOutput: ChatCompletionsChunk[] = [];
  const chatLosses: Loss[] = [];
  for (const event of events) {
    const result = chat.Apply(event);
    chatOutput.push(...result.value);
    chatLosses.push(...result.losses);
  }
  assert.equal(chatOutput.at(-1)?.choices[0]?.finish_reason, "stop");
  assert.ok(chatLosses.some((loss) => loss.reason === "degraded"));

  const anthropic = new AnthropicStreamEncoder();
  const output: AnthropicStreamEvent[] = [];
  const losses: Loss[] = [];
  for (const event of events) {
    const result = anthropic.Apply(event);
    output.push(...result.value);
    losses.push(...result.losses);
  }
  assert.equal(
    output.find((event) => event.type === "message_delta")?.delta?.stop_reason,
    "end_turn",
  );
  assert.ok(losses.some((loss) => loss.reason === "degraded"));
});

test("Anthropic custom tool multi-fragment stream produces valid concatenated JSON", () => {
  const enc = new AnthropicStreamEncoder();
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "custom_tool_use", id: "c", name: "exec", input: "ab" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "a" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "b" },
    },
    { type: "content_block_stop", index: 0 },
    { type: "message_delta", stop_reason: "tool_use", usage },
    { type: "message_done" },
  ];
  const wire: AnthropicStreamEvent[] = [];
  for (const event of events) {
    wire.push(...enc.Apply(event).value);
  }
  const joined = wire
    .filter((w) => w.type === "content_block_delta")
    .map((w) => (w.delta as { partial_json: string }).partial_json)
    .join("");
  assert.doesNotThrow(() => JSON.parse(joined));
  const parsed = JSON.parse(joined);
  assert.equal(parsed.input, "ab");
});

test("declared function tool with matching input shape is not promoted to custom tool", () => {
  const wire = {
    model: "claude-3-5-sonnet-20241022",
    max_tokens: integer(1024n),
    tools: [
      {
        name: "echo",
        input_schema: {
          type: "object",
          properties: { input: { type: "string" }, extra: { type: "string" } },
          required: ["input"],
        },
      },
    ],
    messages: [
      { role: "user", content: "hi" },
      {
        role: "assistant",
        content: [
          {
            type: "tool_use",
            id: "call_1",
            name: "echo",
            input: { input: "hello" },
          },
        ],
      },
      {
        role: "user",
        content: [
          { type: "tool_result", tool_use_id: "call_1", content: "ok" },
        ],
      },
    ],
  };
  const decoded = decodeAnthropicRequest(wire);
  assert.equal(decoded.value.tools?.[0]?.type, undefined);
  const call = decoded.value.messages[1]?.content[0];
  assert.equal(call?.type, "tool_use");
  const res = decoded.value.messages[2]?.content[0];
  assert.equal(res?.type, "tool_result");
});

test("CustomToolResultBlock content array encodes as array and round-trips", () => {
  const req: Request = {
    model: "m",
    messages: [
      { role: "user", content: [{ type: "text", text: "hi" }] },
      {
        role: "assistant",
        content: [
          { type: "custom_tool_use", id: "c", name: "run", input: "cmd" },
        ],
      },
      {
        role: "user",
        content: [
          {
            type: "custom_tool_result",
            tool_use_id: "c",
            output: [
              { type: "text", text: "line 1" },
              { type: "text", text: "line 2" },
            ],
          },
        ],
      },
    ],
  };
  const encoded = encodeChatRequest(req);
  const messages = encoded.value.messages as readonly any[];
  assert.ok(Array.isArray(messages[2]?.content));
  const decoded = decodeChatRequest(encoded.value);
  const result = decoded.value.messages[2]?.content[0];
  assert.equal(result?.type, "custom_tool_result");
  if (result?.type === "custom_tool_result") {
    assert.ok(Array.isArray(result.output));
    assert.equal(result.output.length, 2);
  }
});

test("Custom tool with grammar format produces unsupported-semantic loss on Anthropic encode", () => {
  const req: Request = {
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    tools: [
      {
        type: "custom",
        name: "calc",
        format: { type: "grammar", syntax: "regex", definition: "[0-9]+" },
      },
    ],
  };
  const encoded = encodeAnthropicRequest(req);
  assert.ok(
    encoded.losses.some(
      (l: Loss) =>
        l.path === "tools[0].format" && l.reason === "unsupported-semantic",
    ),
  );
});

test("Chat Completions preserves missing format and reports unknown format", () => {
  const wireMissing = {
    model: "m",
    messages: [{ role: "user", content: "hi" }],
    tools: [{ type: "custom", custom: { name: "c" } }],
  };
  const decodedMissing = decodeChatRequest(wireMissing);
  assert.equal(decodedMissing.losses.length, 0);
  assert.equal(decodedMissing.value.tools?.[0]?.format, undefined);

  const wireUnknown = {
    model: "m",
    messages: [{ role: "user", content: "hi" }],
    tools: [
      { type: "custom", custom: { name: "c", format: { type: "unknown" } } },
    ],
  };
  const decodedUnknown = decodeChatRequest(wireUnknown);
  assert.ok(
    decodedUnknown.losses.some(
      (l: Loss) => l.reason === "unsupported-semantic",
    ),
  );
  assert.equal(decodedUnknown.value.tools?.[0]?.format, undefined);
});
