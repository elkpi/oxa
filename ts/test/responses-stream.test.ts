import assert from "node:assert/strict";
import test from "node:test";

import {
  ResponsesStreamDecoder,
  ResponsesStreamEncoder,
  type ResponsesStreamEvent,
} from "../src/openai/responses/index.js";
import { responses } from "../src/index.js";
import { jsonText } from "../src/json/index.js";
import type { Event } from "../src/ir/index.js";

function created(id: string, model: string): ResponsesStreamEvent {
  return {
    type: "response.created",
    response: {
      id,
      object: "response",
      status: "in_progress",
      model,
      output: [],
    },
  };
}

test("decodes the M7 Responses function call vector with exact opaque fragments", () => {
  const decoder = new ResponsesStreamDecoder();
  const events: ResponsesStreamEvent[] = [
    created("resp_fc1", "gpt-4o-mini"),
    {
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "function_call",
        id: "fc_1",
        call_id: "call_1",
        name: "weather",
        status: "in_progress",
        arguments: '{"city":',
      },
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_1",
      output_index: 0,
      delta: ' "P\\u0041ris",',
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_1",
      output_index: 0,
      delta: "",
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_1",
      output_index: 0,
      delta: ' "days": 1e+01}',
    },
    {
      type: "response.function_call_arguments.done",
      item_id: "fc_1",
      output_index: 0,
      call_id: "call_1",
      name: "weather",
      arguments: '{"city": "P\\u0041ris", "days": 1e+01}',
    },
    {
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "function_call",
        id: "fc_1",
        call_id: "call_1",
        name: "weather",
        status: "completed",
        arguments: '{"city": "P\\u0041ris", "days": 1e+01}',
      },
    },
    {
      type: "response.completed",
      response: {
        id: "resp_fc1",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
        usage: { input_tokens: 9n, output_tokens: 4n, total_tokens: 13n },
      },
    },
  ];

  const actual = events.flatMap((event) => decoder.Feed(event));
  assert.deepEqual(actual, [
    { type: "message_start", id: "resp_fc1", model: "gpt-4o-mini" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "call_1",
        name: "weather",
        input: jsonText('{"city": "P\\u0041ris", "days": 1e+01}'),
      },
    },
    ...['{"city":', ' "P\\u0041ris",', "", ' "days": 1e+01}'].map(
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
      usage: { input_tokens: 9n, output_tokens: 4n },
    },
    { type: "message_done" },
  ] satisfies readonly Event[]);
  assert.deepEqual(decoder.Flush(), []);
  assert.deepEqual(decoder.Losses(), []);
});

test("absorbs an M7 function_call_output item with exactly one vector loss", () => {
  const decoder = new ResponsesStreamDecoder();
  const actual = [
    created("resp_fco1", "gpt-4o-mini"),
    {
      type: "response.output_item.added" as const,
      output_index: 0,
      item: {
        type: "function_call_output",
        id: "fco_1",
        call_id: "call_1",
        status: "in_progress",
        output: "",
      },
    },
    {
      type: "response.output_item.done" as const,
      output_index: 0,
      item: {
        type: "function_call_output",
        id: "fco_1",
        call_id: "call_1",
        status: "completed",
        output: "Sunny, 22",
      },
    },
    {
      type: "response.completed" as const,
      response: {
        id: "resp_fco1",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
        usage: { input_tokens: 4n, output_tokens: 2n, total_tokens: 6n },
      },
    },
  ].flatMap((event) => decoder.Feed(event));

  assert.deepEqual(actual, [
    { type: "message_start", id: "resp_fco1", model: "gpt-4o-mini" },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 4n, output_tokens: 2n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Losses(), [
    {
      path: "output[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail:
        "N-S-10: Responses function_call_output has no supported IR block mapping; response.output_item.done completes and is absorbed for this item-only lifecycle vector",
    },
  ]);
});

test("contains a skipped output_image part and its unknown descendant in one loss", () => {
  const decoder = new ResponsesStreamDecoder();
  const imagePart = { type: "output_image" };
  const actual = [
    created("resp_image", "gpt-4o-mini"),
    {
      type: "response.output_item.added" as const,
      output_index: 0,
      item: {
        type: "message",
        id: "msg_image",
        role: "assistant",
        status: "in_progress",
        content: [],
      },
    },
    {
      type: "response.content_part.added" as const,
      item_id: "msg_image",
      output_index: 0,
      content_index: 0,
      part: imagePart,
    },
    {
      type: "response.output_image.delta" as const,
      item_id: "msg_image",
      output_index: 0,
      content_index: 0,
      delta: "opaque-image-fragment",
    },
    {
      type: "response.content_part.done" as const,
      item_id: "msg_image",
      output_index: 0,
      content_index: 0,
      part: imagePart,
    },
    {
      type: "response.output_item.done" as const,
      output_index: 0,
      item: {
        type: "message",
        id: "msg_image",
        role: "assistant",
        status: "completed",
        content: [],
      },
    },
    {
      type: "response.completed" as const,
      response: {
        id: "resp_image",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
        usage: { input_tokens: 3n, output_tokens: 1n, total_tokens: 4n },
      },
    },
  ].flatMap((event) => decoder.Feed(event));

  assert.deepEqual(actual, [
    { type: "message_start", id: "resp_image", model: "gpt-4o-mini" },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 3n, output_tokens: 1n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Flush(), []);
  assert.deepEqual(decoder.Losses(), [
    {
      path: "output[0].content[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail:
        'Responses streaming content type "output_image" is not decoded in the Responses stream profile',
    },
  ]);
});

test("rejects an unknown descendant with mismatched skipped part identity", () => {
  const mismatches: readonly Partial<ResponsesStreamEvent>[] = [
    { item_id: "msg_other" },
    { output_index: 1 },
    { content_index: 1 },
  ];

  for (const mismatch of mismatches) {
    const decoder = new ResponsesStreamDecoder();
    decoder.Feed(created("resp_image_mismatch", "gpt-4o-mini"));
    decoder.Feed({
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_image",
        role: "assistant",
        status: "in_progress",
        content: [],
      },
    });
    decoder.Feed({
      type: "response.content_part.added",
      item_id: "msg_image",
      output_index: 0,
      content_index: 0,
      part: { type: "output_image" },
    });

    assert.throws(
      () =>
        decoder.Feed({
          type: "response.output_image.delta",
          item_id: "msg_image",
          output_index: 0,
          content_index: 0,
          delta: "opaque-image-fragment",
          ...mismatch,
        }),
      { code: "stream-lifecycle" },
    );
    assert.equal(decoder.Losses().length, 1);
  }
});

test("contains unknown descendants of a skipped item in one item loss", () => {
  const decoder = new ResponsesStreamDecoder();
  const actual = [
    created("resp_reasoning", "gpt-4o-mini"),
    {
      type: "response.output_item.added" as const,
      output_index: 0,
      item: {
        type: "reasoning",
        id: "reasoning_1",
        status: "in_progress",
      },
    },
    {
      type: "response.reasoning_summary_part.added" as const,
      item_id: "reasoning_1",
      output_index: 0,
      content_index: 0,
    },
    {
      type: "response.reasoning_summary_text.delta" as const,
      item_id: "reasoning_1",
      output_index: 0,
      content_index: 0,
      delta: "opaque-reasoning-fragment",
    },
    {
      type: "response.reasoning_summary_part.done" as const,
      item_id: "reasoning_1",
      output_index: 0,
      content_index: 0,
    },
    {
      type: "response.output_item.done" as const,
      output_index: 0,
      item: {
        type: "reasoning",
        id: "reasoning_1",
        status: "completed",
      },
    },
    {
      type: "response.completed" as const,
      response: {
        id: "resp_reasoning",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
      },
    },
  ].flatMap((event) => decoder.Feed(event));

  assert.deepEqual(actual, [
    { type: "message_start", id: "resp_reasoning", model: "gpt-4o-mini" },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Losses(), [
    {
      path: "output[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail: 'Responses streaming output item type "reasoning" is not decoded',
    },
  ]);
});

test("encodes the M7 Responses function call vector with synthesized envelopes", () => {
  const encoder = new ResponsesStreamEncoder();
  const input: Event[] = [
    { type: "message_start", id: "resp_fc2", model: "gpt-4o-mini" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "Look" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "ing." },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "content_block_start",
      index: 1,
      block: {
        type: "tool_use",
        id: "call_7",
        name: "clock",
        input: jsonText('{"tz": "Asia\\/Shanghai", "hour": 1e+01}'),
      },
    },
    {
      type: "content_block_delta",
      index: 1,
      delta: { type: "input_json_delta", partial_json: jsonText('{"tz":') },
    },
    {
      type: "content_block_delta",
      index: 1,
      delta: {
        type: "input_json_delta",
        partial_json: jsonText(' "Asia\\/Shanghai",'),
      },
    },
    {
      type: "content_block_delta",
      index: 1,
      delta: {
        type: "input_json_delta",
        partial_json: jsonText(' "hour": 1e+01}'),
      },
    },
    { type: "content_block_stop", index: 1 },
    {
      type: "content_block_start",
      index: 2,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 2,
      delta: { type: "text_delta", text: "Done." },
    },
    { type: "content_block_stop", index: 2 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 6n, output_tokens: 8n },
    },
    { type: "message_done" },
  ];
  const actual = input.flatMap((event) => {
    const result = encoder.Apply(event);
    assert.deepEqual(result.losses, []);
    return result.value;
  });

  assert.deepEqual(actual, [
    created("resp_fc2", "gpt-4o-mini"),
    {
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_abc123",
        status: "in_progress",
        role: "assistant",
      },
    },
    {
      type: "response.content_part.added",
      item_id: "msg_abc123",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    },
    {
      type: "response.output_text.delta",
      item_id: "msg_abc123",
      output_index: 0,
      content_index: 0,
      delta: "Look",
    },
    {
      type: "response.output_text.delta",
      item_id: "msg_abc123",
      output_index: 0,
      content_index: 0,
      delta: "ing.",
    },
    {
      type: "response.output_text.done",
      item_id: "msg_abc123",
      output_index: 0,
      content_index: 0,
      text: "Looking.",
    },
    {
      type: "response.content_part.done",
      item_id: "msg_abc123",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "Looking.", annotations: [] },
    },
    {
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_abc123",
        status: "completed",
        role: "assistant",
        content: [{ type: "output_text", text: "Looking.", annotations: [] }],
      },
    },
    {
      type: "response.output_item.added",
      output_index: 1,
      item: {
        type: "function_call",
        id: "fc_abc123",
        call_id: "call_7",
        name: "clock",
        status: "in_progress",
      },
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_abc123",
      output_index: 1,
      delta: '{"tz":',
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_abc123",
      output_index: 1,
      delta: ' "Asia\\/Shanghai",',
    },
    {
      type: "response.function_call_arguments.delta",
      item_id: "fc_abc123",
      output_index: 1,
      delta: ' "hour": 1e+01}',
    },
    {
      type: "response.function_call_arguments.done",
      item_id: "fc_abc123",
      output_index: 1,
      call_id: "call_7",
      name: "clock",
      arguments: '{"tz": "Asia\\/Shanghai", "hour": 1e+01}',
    },
    {
      type: "response.output_item.done",
      output_index: 1,
      item: {
        type: "function_call",
        id: "fc_abc123",
        call_id: "call_7",
        name: "clock",
        status: "completed",
        arguments: '{"tz": "Asia\\/Shanghai", "hour": 1e+01}',
      },
    },
    {
      type: "response.output_item.added",
      output_index: 2,
      item: {
        type: "message",
        id: "msg_abc456",
        status: "in_progress",
        role: "assistant",
      },
    },
    {
      type: "response.content_part.added",
      item_id: "msg_abc456",
      output_index: 2,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    },
    {
      type: "response.output_text.delta",
      item_id: "msg_abc456",
      output_index: 2,
      content_index: 0,
      delta: "Done.",
    },
    {
      type: "response.output_text.done",
      item_id: "msg_abc456",
      output_index: 2,
      content_index: 0,
      text: "Done.",
    },
    {
      type: "response.content_part.done",
      item_id: "msg_abc456",
      output_index: 2,
      content_index: 0,
      part: { type: "output_text", text: "Done.", annotations: [] },
    },
    {
      type: "response.output_item.done",
      output_index: 2,
      item: {
        type: "message",
        id: "msg_abc456",
        status: "completed",
        role: "assistant",
        content: [{ type: "output_text", text: "Done.", annotations: [] }],
      },
    },
    {
      type: "response.completed",
      response: {
        id: "resp_fc2",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [
          {
            type: "message",
            id: "msg_abc123",
            status: "completed",
            role: "assistant",
            content: [
              { type: "output_text", text: "Looking.", annotations: [] },
            ],
          },
          {
            type: "function_call",
            id: "fc_abc123",
            call_id: "call_7",
            name: "clock",
            status: "completed",
            arguments: '{"tz": "Asia\\/Shanghai", "hour": 1e+01}',
          },
          {
            type: "message",
            id: "msg_abc456",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: "Done.", annotations: [] }],
          },
        ],
        usage: { input_tokens: 6n, output_tokens: 8n, total_tokens: 14n },
      },
    },
  ]);
});

test("exports Responses stream converters from the package root", () => {
  assert.equal(typeof responses.ResponsesStreamDecoder, "function");
  assert.equal(typeof responses.ResponsesStreamEncoder, "function");
});

test("accepts a function call whose optional arguments.done event is omitted", () => {
  const decoder = new ResponsesStreamDecoder();
  assert.deepEqual(decoder.Feed(created("resp_optional_done", "gpt-4o-mini")), [
    { type: "message_start", id: "resp_optional_done", model: "gpt-4o-mini" },
  ]);
  assert.deepEqual(
    decoder.Feed({
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "function_call",
        id: "fc_optional",
        call_id: "call_optional",
        name: "weather",
        status: "in_progress",
        arguments: '{"city":',
      },
    }),
    [],
  );
  assert.deepEqual(
    decoder.Feed({
      type: "response.function_call_arguments.delta",
      item_id: "fc_optional",
      output_index: 0,
      delta: ' "Paris"}',
    }),
    [],
  );
  assert.deepEqual(
    decoder.Feed({
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "function_call",
        id: "fc_optional",
        call_id: "call_optional",
        name: "weather",
        status: "completed",
        arguments: '{"city": "Paris"}',
      },
    }),
    [
      {
        type: "content_block_start",
        index: 0,
        block: {
          type: "tool_use",
          id: "call_optional",
          name: "weather",
          input: jsonText('{"city": "Paris"}'),
        },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "input_json_delta", partial_json: jsonText('{"city":') },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: {
          type: "input_json_delta",
          partial_json: jsonText(' "Paris"}'),
        },
      },
      { type: "content_block_stop", index: 0 },
    ],
  );
  assert.deepEqual(decoder.Losses(), []);
});

test("rejects a supplied arguments.done value that differs from raw fragments", () => {
  const decoder = new ResponsesStreamDecoder();
  decoder.Feed(created("resp_bad_done", "gpt-4o-mini"));
  decoder.Feed({
    type: "response.output_item.added",
    output_index: 0,
    item: {
      type: "function_call",
      id: "fc_bad",
      call_id: "call_bad",
      name: "weather",
      status: "in_progress",
      arguments: '{"city": "Paris"}',
    },
  });

  assert.throws(
    () =>
      decoder.Feed({
        type: "response.function_call_arguments.done",
        item_id: "fc_bad",
        output_index: 0,
        call_id: "call_bad",
        name: "weather",
        arguments: '{"city": "Lyon"}',
      }),
    { code: "stream-lifecycle" },
  );
});

test("Responses stream usage preserves lossless int64 values", () => {
  for (const value of [9_007_199_254_740_993n, 9_223_372_036_854_775_807n]) {
    const decoder = new ResponsesStreamDecoder();
    decoder.Feed(created("resp_usage", "gpt-4o-mini"));
    const events = decoder.Feed({
      type: "response.completed",
      response: {
        id: "resp_usage",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
        usage: {
          input_tokens: value,
          output_tokens: 0n,
          total_tokens: value,
        },
      },
    });
    assert.deepEqual(events[0], {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: value, output_tokens: 0n },
    });
  }

  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "resp_usage",
    model: "gpt-4o-mini",
  });
  const encoded = encoder.Apply({
    type: "message_delta",
    stop_reason: "end_turn",
    usage: {
      input_tokens: 9_223_372_036_854_775_807n,
      output_tokens: 0n,
    },
  }).value;
  assert.deepEqual(encoded.at(-1)?.response?.usage, {
    input_tokens: 9_223_372_036_854_775_807n,
    output_tokens: 0n,
    total_tokens: 9_223_372_036_854_775_807n,
  });
});

test("Responses stream usage rejects invalid values", () => {
  const invalid = [
    9_223_372_036_854_775_808n,
    -1n,
    { kind: "number", token: "1.5", isInteger: false },
  ] as const;
  for (const value of invalid) {
    const decoder = new ResponsesStreamDecoder();
    decoder.Feed(created("resp_invalid_usage", "gpt-4o-mini"));
    assert.throws(
      () =>
        decoder.Feed({
          type: "response.completed",
          response: {
            id: "resp_invalid_usage",
            object: "response",
            status: "completed",
            model: "gpt-4o-mini",
            output: [],
            usage: {
              input_tokens: value,
              output_tokens: 0n,
              total_tokens: 0n,
            },
          },
        }),
      { code: "invalid-input" },
    );
  }

  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "resp_invalid_usage",
    model: "gpt-4o-mini",
  });
  assert.throws(
    () =>
      encoder.Apply({
        type: "message_delta",
        stop_reason: "end_turn",
        usage: {
          input_tokens: 9_223_372_036_854_775_808n,
          output_tokens: 0n,
        },
      }),
    { code: "invalid-input" },
  );
});
