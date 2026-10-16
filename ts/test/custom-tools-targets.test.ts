import assert from "node:assert/strict";
import test from "node:test";
import type { Loss } from "../src/loss.js";
import { integer, jsonText } from "../src/json/index.js";

import {
  encodeRequest as encodeAnthropicRequest,
  encodeResponse as encodeAnthropicResponse,
  AnthropicStreamEncoder,
  type AnthropicStreamEvent,
} from "../src/anthropic/messages/index.js";
import {
  decodeRequest as decodeIrRequest,
  type Event,
  type Response,
} from "../src/ir/index.js";
import {
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

test("Chat and Anthropic drop custom declarations and choices with explicit losses", () => {
  const request = decodeIrRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "go" }] }],
    tools: [{ type: "custom", name: "run" }],
    params: { max_tokens: integer(32n) },
    tool_choice: { mode: "tool", name: "run", tool_type: "custom" },
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
  const chat = encodeChatResponse(customResponse);
  assert.equal(
    (chat.value.choices as readonly { finish_reason: string }[])[0]
      ?.finish_reason,
    "stop",
  );
  assert.ok(chat.losses.some((loss) => loss.reason === "degraded"));
  const anthropic = encodeAnthropicResponse(customResponse);
  assert.equal(anthropic.value.stop_reason, "end_turn");
  assert.ok(anthropic.losses.some((loss) => loss.reason === "degraded"));

  const mixed = decodeIrRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [
      { role: "user", content: [{ type: "text", text: "go" }] },
      {
        role: "assistant",
        content: [
          { type: "custom_tool_use", id: "custom", name: "run", input: "" },
          { type: "tool_use", id: "function", name: "lookup", input: "{}" },
        ],
      },
      {
        role: "user",
        content: [
          {
            type: "custom_tool_result",
            tool_use_id: "custom",
            output: "dropped",
          },
          {
            type: "tool_result",
            tool_use_id: "function",
            content: [{ type: "text", text: "kept" }],
          },
        ],
      },
    ],
  });
  const downgraded = encodeChatRequest(mixed);
  assert.deepEqual(
    (downgraded.value.messages as readonly { role: string }[]).map(
      ({ role }) => role,
    ),
    ["user", "assistant", "tool"],
  );
  assert.equal(
    downgraded.losses.filter((loss) => loss.reason === "unsupported-semantic")
      .length,
    2,
  );
});

test("unsupported-target stream encoders absorb custom deltas and compact retained indexes", () => {
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "custom_tool_use", id: "c", name: "run", input: "x" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "x" },
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

test("custom-only unsupported streams normalize tool_use to end_turn with degraded loss", () => {
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "custom_tool_use", id: "c", name: "run", input: "" },
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
