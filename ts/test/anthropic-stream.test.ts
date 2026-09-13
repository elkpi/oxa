import assert from "node:assert/strict";
import test from "node:test";

import { anthropic } from "../src/index.js";
import {
  AnthropicStreamDecoder,
  AnthropicStreamEncoder,
  type AnthropicStreamEvent,
} from "../src/anthropic/messages/index.js";
import type { Event } from "../src/ir/index.js";
import { jsonText } from "../src/json/index.js";

function messageStart(id: string, model: string): AnthropicStreamEvent {
  return {
    type: "message_start",
    message: {
      id,
      type: "message",
      role: "assistant",
      model,
      content: [],
      stop_reason: null,
      usage: { input_tokens: 0, output_tokens: 0 },
    },
  };
}

function terminal(
  stop_reason: string,
  input_tokens: number,
  output_tokens: number,
): readonly AnthropicStreamEvent[] {
  return [
    {
      type: "message_delta",
      delta: { stop_reason },
      usage: { input_tokens, output_tokens },
    },
    { type: "message_stop" },
  ];
}

test("decodes anthropic.stream.m7-tool-use-to-ir with exact opaque fragments", () => {
  const decoder = new AnthropicStreamDecoder();
  assert.deepEqual(decoder.Feed(messageStart("msg_1", "claude-sonnet-4-5")), [
    {
      type: "message_start",
      id: "msg_1",
      model: "claude-sonnet-4-5",
    },
  ]);
  assert.deepEqual(
    decoder.Feed({
      type: "content_block_start",
      index: 0,
      content_block: {
        type: "tool_use",
        id: "toolu_1",
        name: "weather",
        input: {},
      },
    }),
    [],
  );
  for (const partial_json of [
    '{"city":',
    "",
    ' "P\\u0041ris", "days": 1e+01}',
  ]) {
    assert.deepEqual(
      decoder.Feed({
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json },
      }),
      [],
    );
  }

  assert.deepEqual(decoder.Feed({ type: "content_block_stop", index: 0 }), [
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "toolu_1",
        name: "weather",
        input: jsonText('{"city": "P\\u0041ris", "days": 1e+01}'),
      },
    },
    ...['{"city":', "", ' "P\\u0041ris", "days": 1e+01}'].map(
      (partial_json) => ({
        type: "content_block_delta" as const,
        index: 0,
        delta: {
          type: "input_json_delta" as const,
          partial_json: jsonText(partial_json),
        },
      }),
    ),
    { type: "content_block_stop", index: 0 },
  ]);
  const endings = terminal("tool_use", 12, 7).flatMap((event) =>
    decoder.Feed(event),
  );
  assert.deepEqual(endings, [
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 12n, output_tokens: 7n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Flush(), []);
  assert.deepEqual(decoder.Losses(), []);
});

test("decodes anthropic.stream.m7-tool-use-start-input-fallback-to-ir exactly once", () => {
  const decoder = new AnthropicStreamDecoder();
  decoder.Feed(messageStart("msg_2", "claude-sonnet-4-5"));
  const raw = jsonText('{ "tz": "Asia\\/Shanghai", "hour": 1e+01 }');
  assert.deepEqual(
    decoder.Feed({
      type: "content_block_start",
      index: 0,
      content_block: {
        type: "tool_use",
        id: "toolu_2",
        name: "clock",
        input: raw,
      },
    }),
    [],
  );
  assert.deepEqual(decoder.Feed({ type: "content_block_stop", index: 0 }), [
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "toolu_2",
        name: "clock",
        input: raw,
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "input_json_delta", partial_json: raw },
    },
    { type: "content_block_stop", index: 0 },
  ]);
});

test("encodes anthropic.stream.m7-tool-use-from-ir with canonical start input", () => {
  const encoder = new AnthropicStreamEncoder();
  const input = [
    { type: "message_start", id: "msg_9", model: "claude-sonnet-4-5" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "toolu_3",
        name: "weather",
        input: jsonText('{"city": "P\\u0041ris", "weight": 1.0}'),
      },
    },
    ...['{"city":', ' "P\\u0041ris",', ' "weight": 1.0}'].map(
      (partial_json) => ({
        type: "content_block_delta" as const,
        index: 0,
        delta: {
          type: "input_json_delta" as const,
          partial_json: jsonText(partial_json),
        },
      }),
    ),
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 3n, output_tokens: 5n },
    },
    { type: "message_done" },
  ] satisfies readonly Event[];
  const actual = input.flatMap((event) => {
    const result = encoder.Apply(event);
    assert.deepEqual(result.losses, []);
    return result.value;
  });

  assert.deepEqual(actual, [
    messageStart("msg_9", "claude-sonnet-4-5"),
    {
      type: "content_block_start",
      index: 0,
      content_block: {
        type: "tool_use",
        id: "toolu_3",
        name: "weather",
        input: {},
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "input_json_delta", partial_json: '{"city":' },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: {
        type: "input_json_delta",
        partial_json: ' "P\\u0041ris",',
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "input_json_delta", partial_json: ' "weight": 1.0}' },
    },
    { type: "content_block_stop", index: 0 },
    ...terminal("tool_use", 3, 5),
  ]);
});

test("encoder synthesizes exactly one full input delta when IR provides none", () => {
  const encoder = new AnthropicStreamEncoder();
  encoder.Apply({ type: "message_start", id: "msg_synth", model: "claude" });
  encoder.Apply({
    type: "content_block_start",
    index: 0,
    block: {
      type: "tool_use",
      id: "toolu_synth",
      name: "clock",
      input: jsonText('{"tz":"UTC"}'),
    },
  });
  assert.deepEqual(
    encoder.Apply({ type: "content_block_stop", index: 0 }).value,
    [
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: '{"tz":"UTC"}' },
      },
      { type: "content_block_stop", index: 0 },
    ],
  );
});

test("streams Anthropic text blocks and maps models and stop sequences", () => {
  const decoder = new AnthropicStreamDecoder({
    modelMapper: (model) => (model === "claude" ? "mapped" : undefined),
  });
  const wire: AnthropicStreamEvent[] = [
    messageStart("msg_text", "claude"),
    {
      type: "content_block_start",
      index: 0,
      content_block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "hello" },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      delta: { stop_reason: "stop_sequence", stop_sequence: "END" },
      usage: { input_tokens: 2, output_tokens: 1 },
    },
    { type: "message_stop" },
  ];
  assert.deepEqual(
    wire.flatMap((event) => decoder.Feed(event)),
    [
      { type: "message_start", id: "msg_text", model: "mapped" },
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
        stop_reason: "stop_sequence",
        stop_sequence: "END",
        usage: { input_tokens: 2n, output_tokens: 1n },
      },
      { type: "message_done" },
    ],
  );
});

test("absorbs an unsupported native block once and compacts retained indexes", () => {
  const decoder = new AnthropicStreamDecoder();
  const actual = [
    messageStart("msg_skip", "claude"),
    {
      type: "content_block_start" as const,
      index: 0,
      content_block: { type: "server_tool_use" },
    },
    {
      type: "content_block_delta" as const,
      index: 0,
      delta: { type: "input_json_delta", partial_json: "ignored" },
    },
    { type: "content_block_stop" as const, index: 0 },
    {
      type: "content_block_start" as const,
      index: 1,
      content_block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta" as const,
      index: 1,
      delta: { type: "text_delta", text: "kept" },
    },
    { type: "content_block_stop" as const, index: 1 },
    ...terminal("end_turn", 1, 1),
  ].flatMap((event) => decoder.Feed(event));

  assert.deepEqual(actual.slice(1, 4), [
    {
      type: "content_block_start",
      index: 0,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "kept" },
    },
    { type: "content_block_stop", index: 0 },
  ]);
  assert.deepEqual(decoder.Losses(), [
    {
      path: "content_block_start[0].content_block.type",
      field: "content_block.type",
      reason: "unsupported-semantic",
      detail:
        'Anthropic streaming block type "server_tool_use" is not decodable in M7; the index is skipped',
    },
  ]);
});

test("records unknown event, delta, and stop values as ordered losses", () => {
  const decoder = new AnthropicStreamDecoder();
  decoder.Feed({ type: "ping" });
  decoder.Feed(messageStart("msg_losses", "claude"));
  decoder.Feed({
    type: "content_block_start",
    index: 0,
    content_block: { type: "text", text: "" },
  });
  assert.deepEqual(
    decoder.Feed({
      type: "content_block_delta",
      index: 0,
      delta: { type: "citations_delta" },
    }),
    [],
  );
  decoder.Feed({ type: "content_block_stop", index: 0 });
  decoder.Feed({
    type: "message_delta",
    delta: { stop_reason: "pause_turn" },
    usage: { input_tokens: 0, output_tokens: 0 },
  });
  decoder.Feed({ type: "message_stop" });
  assert.deepEqual(
    decoder.Losses().map(({ field, reason }) => ({ field, reason })),
    [
      { field: "type", reason: "unsupported-semantic" },
      { field: "delta.type", reason: "unsupported-semantic" },
      { field: "stop_reason", reason: "unmapped-value" },
    ],
  );
});

test("rejects malformed native and IR lifecycles", () => {
  const beforeStart = new AnthropicStreamDecoder();
  assert.throws(
    () => beforeStart.Feed({ type: "content_block_stop", index: 0 }),
    { code: "stream-lifecycle" },
  );

  const open = new AnthropicStreamDecoder();
  open.Feed(messageStart("msg_error", "claude"));
  open.Feed({
    type: "content_block_start",
    index: 0,
    content_block: { type: "text", text: "" },
  });
  assert.throws(
    () =>
      open.Feed({
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: "{}" },
      }),
    { code: "stream-lifecycle" },
  );

  const noTerminal = new AnthropicStreamDecoder();
  noTerminal.Feed(messageStart("msg_flush", "claude"));
  assert.throws(() => noTerminal.Flush(), { code: "stream-lifecycle" });

  const encoder = new AnthropicStreamEncoder();
  encoder.Apply({ type: "message_start", id: "msg_ir_error", model: "claude" });
  assert.throws(
    () =>
      encoder.Apply({
        type: "content_block_start",
        index: 1,
        block: { type: "text", text: "" },
      }),
    { code: "stream-lifecycle" },
  );
});

test("exports Anthropic stream converters from the package root", () => {
  assert.equal(typeof anthropic.AnthropicStreamDecoder, "function");
  assert.equal(typeof anthropic.AnthropicStreamEncoder, "function");
});

test("does not emit an empty native stop sequence into IR", () => {
  const decoder = new AnthropicStreamDecoder();
  decoder.Feed(messageStart("msg_empty_stop", "claude"));
  decoder.Feed({
    type: "message_delta",
    delta: { stop_reason: "stop_sequence", stop_sequence: "" },
    usage: { input_tokens: 0, output_tokens: 0 },
  });
  assert.deepEqual(decoder.Feed({ type: "message_stop" }), [
    {
      type: "message_delta",
      stop_reason: "stop_sequence",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ]);
});
