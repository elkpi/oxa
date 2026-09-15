import assert from "node:assert/strict";
import test from "node:test";

import type { Event } from "../src/ir/index.js";
import { jsonText } from "../src/json/index.js";
import type { ChatCompletionsChunk } from "../src/openai/chatcompletions/index.js";
import type { ResponsesStreamEvent } from "../src/openai/responses/index.js";
import {
  decodeChatCompletions,
  decodeResponses,
  encodeChatCompletions,
} from "../src/stream/index.js";

async function* chatChunks(): AsyncIterable<ChatCompletionsChunk> {
  yield {
    id: "chatcmpl-async",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: { role: "assistant", content: "hello" },
        finish_reason: "stop",
      },
    ],
  };
}

test("Chat Completions async decoding emits the decoder's final flush", async () => {
  const decoded = decodeChatCompletions(chatChunks());
  const events = [];

  for await (const event of decoded) events.push(event);

  assert.deepEqual(events, [
    { type: "message_start", id: "chatcmpl-async", model: "gpt-4o-mini" },
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
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoded.losses(), []);
});

async function* irEvents(): AsyncIterable<Event> {
  yield { type: "message_start", id: "msg-async", model: "gpt-4o-mini" };
  yield {
    type: "content_block_start",
    index: 0,
    block: {
      type: "tool_use",
      id: "call-1",
      name: "weather",
      input: jsonText('{"city":"Paris"}'),
    },
  };
  yield {
    type: "content_block_delta",
    index: 0,
    delta: {
      type: "input_json_delta",
      partial_json: jsonText('{"city":"Paris"}'),
    },
  };
  yield { type: "content_block_stop", index: 0 };
  yield {
    type: "content_block_start",
    index: 1,
    block: { type: "text", text: "" },
  };
  yield {
    type: "content_block_delta",
    index: 1,
    delta: { type: "text_delta", text: "done" },
  };
  yield { type: "content_block_stop", index: 1 };
  yield {
    type: "message_delta",
    stop_reason: "stop_sequence",
    stop_sequence: "END",
    usage: { input_tokens: 2n, output_tokens: 3n },
  };
  yield { type: "message_done" };
}

test("Chat Completions async encoding preserves output and loss order", async () => {
  const encoded = encodeChatCompletions(irEvents());
  const outputs = [];

  for await (const chunk of encoded) {
    outputs.push(
      chunk.choices[0]?.delta.content ??
        chunk.choices[0]?.delta.tool_calls?.[0]?.function?.arguments ??
        chunk.choices[0]?.finish_reason ??
        chunk.choices[0]?.delta.role,
    );
  }

  assert.deepEqual(outputs, ["assistant", "done", '{"city":"Paris"}', "stop"]);
  assert.deepEqual(
    encoded.losses().map(({ field, reason }) => ({ field, reason })),
    [
      { field: "stop_sequence", reason: "unmapped-value" },
      { field: "ordering", reason: "degraded" },
    ],
  );
});

test("Responses async decoding exposes source errors after prior events", async () => {
  const sourceError = new Error("source failed");
  async function* source(): AsyncIterable<ResponsesStreamEvent> {
    yield {
      type: "response.created",
      response: {
        id: "resp-async",
        object: "response",
        model: "gpt-4.1-mini",
        status: "in_progress",
        output: [],
      },
    };
    throw sourceError;
  }

  const iterator = decodeResponses(source())[Symbol.asyncIterator]();
  assert.deepEqual(await iterator.next(), {
    done: false,
    value: {
      type: "message_start",
      id: "resp-async",
      model: "gpt-4.1-mini",
    },
  });
  await assert.rejects(iterator.next(), (error) => error === sourceError);
});
