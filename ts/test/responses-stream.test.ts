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

test("keeps output_text strict while accepting unknown content parts", () => {
  type OutputPart = NonNullable<ResponsesStreamEvent["part"]>;
  const textPart: OutputPart = {
    type: "output_text",
    text: "hello",
    annotations: [],
  };
  const imagePart: OutputPart = { type: "output_image" };
  const partValue = (part: OutputPart): string =>
    part.type === "output_text" ? part.text : part.type;
  // @ts-expect-error output_text requires both text and annotations.
  const incompleteText: OutputPart = { type: "output_text" };

  assert.equal(partValue(textPart), "hello");
  assert.equal(partValue(imagePart), "output_image");
  void incompleteText;
});

test("contains a skipped output_image part and its unknown descendant in one loss", () => {
  const decoder = new ResponsesStreamDecoder();
  const imagePart = { type: "output_image" } as const;
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

test("rejects unknown events with mismatched supported item identity", () => {
  const mismatches: readonly Partial<ResponsesStreamEvent>[] = [
    { item_id: "msg_other" },
    { output_index: 1 },
  ];

  for (const mismatch of mismatches) {
    const decoder = new ResponsesStreamDecoder();
    decoder.Feed(created("resp_item_mismatch", "gpt-4o-mini"));
    decoder.Feed({
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_supported",
        role: "assistant",
        status: "in_progress",
        content: [],
      },
    });

    assert.throws(
      () =>
        decoder.Feed({
          type: "response.unknown_item_child",
          item_id: "msg_supported",
          output_index: 0,
          ...mismatch,
        }),
      { code: "stream-lifecycle" },
    );
    assert.deepEqual(decoder.Losses(), []);
  }
});

test("rejects unknown events with mismatched supported part identity", () => {
  const mismatches: readonly Partial<ResponsesStreamEvent>[] = [
    { item_id: "msg_other" },
    { output_index: 1 },
    { content_index: 1 },
  ];

  for (const mismatch of mismatches) {
    const decoder = new ResponsesStreamDecoder();
    decoder.Feed(created("resp_part_mismatch", "gpt-4o-mini"));
    decoder.Feed({
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_supported",
        role: "assistant",
        status: "in_progress",
        content: [],
      },
    });
    decoder.Feed({
      type: "response.content_part.added",
      item_id: "msg_supported",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    });

    assert.throws(
      () =>
        decoder.Feed({
          type: "response.unknown_part_child",
          item_id: "msg_supported",
          output_index: 0,
          content_index: 0,
          ...mismatch,
        }),
      { code: "stream-lifecycle" },
    );
    assert.deepEqual(decoder.Losses(), []);
  }
});

test("rejects a late unknown descendant after a skipped part closes", () => {
  const decoder = new ResponsesStreamDecoder();
  const imagePart = { type: "output_image" } as const;
  decoder.Feed(created("resp_late_image", "gpt-4o-mini"));
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
    part: imagePart,
  });
  decoder.Feed({
    type: "response.content_part.done",
    item_id: "msg_image",
    output_index: 0,
    content_index: 0,
    part: imagePart,
  });

  assert.throws(
    () =>
      decoder.Feed({
        type: "response.output_image.delta",
        item_id: "msg_image",
        output_index: 0,
        content_index: 0,
        delta: "late-fragment",
      }),
    { code: "stream-lifecycle" },
  );
  assert.equal(decoder.Losses().length, 1);
});

test("rejects an identity-bearing unknown event after the output item closes", () => {
  const decoder = new ResponsesStreamDecoder();
  decoder.Feed(created("resp_late_item", "gpt-4o-mini"));
  decoder.Feed({
    type: "response.output_item.added",
    output_index: 0,
    item: {
      type: "message",
      id: "msg_closed",
      role: "assistant",
      status: "in_progress",
      content: [],
    },
  });
  decoder.Feed({
    type: "response.output_item.done",
    output_index: 0,
    item: {
      type: "message",
      id: "msg_closed",
      role: "assistant",
      status: "completed",
      content: [],
    },
  });

  assert.throws(
    () =>
      decoder.Feed({
        type: "response.unknown_item_child",
        item_id: "msg_closed",
        output_index: 0,
      }),
    { code: "stream-lifecycle" },
  );
  assert.deepEqual(decoder.Losses(), []);
});

test("records an identity-less standalone unknown event with an item open", () => {
  const decoder = new ResponsesStreamDecoder();
  decoder.Feed(created("resp_standalone", "gpt-4o-mini"));
  decoder.Feed({
    type: "response.output_item.added",
    output_index: 0,
    item: {
      type: "message",
      id: "msg_open",
      role: "assistant",
      status: "in_progress",
      content: [],
    },
  });

  assert.deepEqual(decoder.Feed({ type: "response.heartbeat" }), []);
  assert.deepEqual(decoder.Losses(), [
    {
      path: "type",
      field: "type",
      reason: "unsupported-semantic",
      detail:
        'Responses stream event type "response.heartbeat" is not decoded in the Responses stream profile',
    },
  ]);
  assert.deepEqual(
    decoder.Feed({
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "message",
        id: "msg_open",
        role: "assistant",
        status: "completed",
        content: [],
      },
    }),
    [],
  );
  assert.deepEqual(
    decoder.Feed({
      type: "response.completed",
      response: {
        id: "resp_standalone",
        object: "response",
        status: "completed",
        model: "gpt-4o-mini",
        output: [],
      },
    }),
    [
      {
        type: "message_delta",
        stop_reason: "end_turn",
        usage: { input_tokens: 0n, output_tokens: 0n },
      },
      { type: "message_done" },
    ],
  );
  assert.deepEqual(decoder.Flush(), []);
});

test("decodes reasoning summary parts into a thinking block with text.done validation", () => {
  const decoder = new ResponsesStreamDecoder();
  const actual = [
    created("resp_think_stream", "o3-mini"),
    {
      type: "response.output_item.added" as const,
      output_index: 0,
      item: { type: "reasoning", id: "rs_1", summary: [] },
    },
    {
      type: "response.reasoning_summary_part.added" as const,
      item_id: "rs_1",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text" as const, text: "" },
    },
    {
      type: "response.reasoning_summary_text.delta" as const,
      item_id: "rs_1",
      output_index: 0,
      content_index: 0,
      delta: "Analyzing...",
    },
    {
      type: "response.reasoning_summary_text.done" as const,
      item_id: "rs_1",
      output_index: 0,
      content_index: 0,
      text: "Analyzing...",
    },
    {
      type: "response.reasoning_summary_part.done" as const,
      item_id: "rs_1",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text" as const, text: "Analyzing..." },
    },
    {
      type: "response.output_item.done" as const,
      output_index: 0,
      item: {
        type: "reasoning",
        id: "rs_1",
        summary: [{ type: "output_text" as const, text: "Analyzing..." }],
      },
    },
    {
      type: "response.completed" as const,
      response: {
        id: "resp_think_stream",
        object: "response",
        status: "completed",
        model: "o3-mini",
        output: [],
        usage: { input_tokens: 10n, output_tokens: 15n, total_tokens: 25n },
      },
    },
  ].flatMap((event) => decoder.Feed(event));

  assert.deepEqual(actual, [
    { type: "message_start", id: "resp_think_stream", model: "o3-mini" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "thinking", thinking: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "thinking_delta", text: "Analyzing..." },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 10n, output_tokens: 15n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Flush(), []);
  assert.deepEqual(decoder.Losses(), []);
});

test("preserves terminal reasoning and cache usage details", () => {
  const decoder = new ResponsesStreamDecoder();
  decoder.Feed(created("resp_usage_details", "o3-mini"));

  assert.deepEqual(
    decoder.Feed({
      type: "response.completed",
      response: {
        id: "resp_usage_details",
        object: "response",
        status: "completed",
        model: "o3-mini",
        output: [],
        usage: {
          input_tokens: 10n,
          output_tokens: 15n,
          total_tokens: 25n,
          input_token_details: { cached_tokens: 4n },
          output_token_details: { reasoning_tokens: 8n },
        },
      },
    }),
    [
      {
        type: "message_delta",
        stop_reason: "end_turn",
        usage: {
          input_tokens: 10n,
          output_tokens: 15n,
          input_tokens_details: { cached_tokens: 4n },
          output_tokens_details: { reasoning_tokens: 8n },
        },
      },
      { type: "message_done" },
    ],
  );
});

test("records one loss for a reasoning item closed without summary parts", () => {
  const decoder = new ResponsesStreamDecoder();
  [
    created("resp_empty_reasoning", "o3-mini"),
    {
      type: "response.output_item.added" as const,
      output_index: 0,
      item: { type: "reasoning", id: "rs_empty", summary: [] },
    },
    {
      type: "response.output_item.done" as const,
      output_index: 0,
      item: { type: "reasoning", id: "rs_empty", summary: [] },
    },
  ].forEach((event) => decoder.Feed(event));

  assert.deepEqual(decoder.Losses(), [
    {
      path: "output[0]",
      field: "type",
      reason: "unsupported-semantic",
      detail:
        "Responses reasoning output item with empty summary carries no convertible content",
    },
  ]);
});

test("rejects thinking deltas after a Responses signature delta", () => {
  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({ type: "message_start", id: "resp_late", model: "o3-mini" });
  encoder.Apply({
    type: "content_block_start",
    index: 0,
    block: { type: "thinking", thinking: "" },
  });
  encoder.Apply({
    type: "content_block_delta",
    index: 0,
    delta: { type: "signature_delta", signature: "sig" },
  });

  assert.throws(
    () =>
      encoder.Apply({
        type: "content_block_delta",
        index: 0,
        delta: { type: "thinking_delta", text: "late" },
      }),
    { code: "stream-lifecycle" },
  );
});

test("rejects duplicate Responses signature deltas", () => {
  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "resp_duplicate_signature",
    model: "o3-mini",
  });
  encoder.Apply({
    type: "content_block_start",
    index: 0,
    block: { type: "thinking", thinking: "" },
  });
  encoder.Apply({
    type: "content_block_delta",
    index: 0,
    delta: { type: "signature_delta", signature: "sig_1" },
  });

  assert.throws(
    () =>
      encoder.Apply({
        type: "content_block_delta",
        index: 0,
        delta: { type: "signature_delta", signature: "sig_2" },
      }),
    { code: "stream-lifecycle" },
  );
});

test("encodes a thinking stream as a reasoning summary item", () => {
  const encoder = new ResponsesStreamEncoder();
  const actual = [
    encoder.Apply({
      type: "message_start",
      id: "resp_think_from",
      model: "o3-mini",
    }),
    encoder.Apply({
      type: "content_block_start",
      index: 0,
      block: { type: "thinking", thinking: "" },
    }),
    encoder.Apply({
      type: "content_block_delta",
      index: 0,
      delta: { type: "thinking_delta", text: "Analyzing..." },
    }),
    encoder.Apply({ type: "content_block_stop", index: 0 }),
    encoder.Apply({
      type: "content_block_start",
      index: 1,
      block: { type: "text", text: "" },
    }),
    encoder.Apply({
      type: "content_block_delta",
      index: 1,
      delta: { type: "text_delta", text: "Done." },
    }),
    encoder.Apply({ type: "content_block_stop", index: 1 }),
    encoder.Apply({
      type: "message_delta",
      stop_reason: "end_turn",
      usage: {
        input_tokens: 10n,
        output_tokens: 15n,
        input_tokens_details: { cached_tokens: 5n },
        output_tokens_details: { reasoning_tokens: 10n },
      },
    }),
    encoder.Apply({ type: "message_done" }),
  ];
  for (const result of actual) assert.deepEqual(result.losses, []);

  const events = actual.flatMap(({ value }) => value);
  assert.deepEqual(events, [
    {
      type: "response.created",
      response: {
        id: "resp_think_from",
        object: "response",
        status: "in_progress",
        model: "o3-mini",
        output: [],
      },
    },
    {
      type: "response.output_item.added",
      output_index: 0,
      item: { type: "reasoning", id: "rs_abc123", status: "in_progress" },
    },
    {
      type: "response.reasoning_summary_part.added",
      item_id: "rs_abc123",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    },
    {
      type: "response.reasoning_summary_text.delta",
      item_id: "rs_abc123",
      output_index: 0,
      content_index: 0,
      delta: "Analyzing...",
    },
    {
      type: "response.reasoning_summary_part.done",
      item_id: "rs_abc123",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "Analyzing...", annotations: [] },
    },
    {
      type: "response.output_item.done",
      output_index: 0,
      item: {
        type: "reasoning",
        id: "rs_abc123",
        status: "completed",
        summary: [
          { type: "output_text", text: "Analyzing...", annotations: [] },
        ],
      },
    },
    {
      type: "response.output_item.added",
      output_index: 1,
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
      output_index: 1,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    },
    {
      type: "response.output_text.delta",
      item_id: "msg_abc123",
      output_index: 1,
      content_index: 0,
      delta: "Done.",
    },
    {
      type: "response.output_text.done",
      item_id: "msg_abc123",
      output_index: 1,
      content_index: 0,
      text: "Done.",
    },
    {
      type: "response.content_part.done",
      item_id: "msg_abc123",
      output_index: 1,
      content_index: 0,
      part: { type: "output_text", text: "Done.", annotations: [] },
    },
    {
      type: "response.output_item.done",
      output_index: 1,
      item: {
        type: "message",
        id: "msg_abc123",
        status: "completed",
        role: "assistant",
        content: [{ type: "output_text", text: "Done.", annotations: [] }],
      },
    },
    {
      type: "response.completed",
      response: {
        id: "resp_think_from",
        object: "response",
        status: "completed",
        model: "o3-mini",
        output: [
          {
            type: "reasoning",
            id: "rs_abc123",
            status: "completed",
            summary: [
              { type: "output_text", text: "Analyzing...", annotations: [] },
            ],
          },
          {
            type: "message",
            id: "msg_abc123",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: "Done.", annotations: [] }],
          },
        ],
        usage: {
          input_tokens: 10n,
          output_tokens: 15n,
          total_tokens: 25n,
          input_token_details: { cached_tokens: 5n },
          output_token_details: { reasoning_tokens: 10n },
        },
      },
    },
  ]);
});

test("reports each stream signature source exactly once for reasoning blocks", () => {
  const encoder = new ResponsesStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "resp_signature",
    model: "o3-mini",
  });
  const started = encoder.Apply({
    type: "content_block_start",
    index: 0,
    block: { type: "thinking", thinking: "", signature: "start-sig" },
  });
  assert.equal(started.losses.length, 0);
  const signed = encoder.Apply({
    type: "content_block_delta",
    index: 0,
    delta: { type: "signature_delta", signature: "delta-sig" },
  });
  assert.deepEqual(signed.value, []);
  assert.deepEqual(
    signed.losses.map(({ field, reason }) => ({ field, reason })),
    [{ field: "signature", reason: "unmapped-field" }],
  );
  const stopped = encoder.Apply({ type: "content_block_stop", index: 0 });
  assert.deepEqual(
    stopped.losses.map(({ field, reason }) => ({ field, reason })),
    [],
  );

  const unsigned = new ResponsesStreamEncoder();
  unsigned.Apply({
    type: "message_start",
    id: "resp_signature2",
    model: "o3-mini",
  });
  const cached = unsigned.Apply({
    type: "content_block_start",
    index: 0,
    block: { type: "thinking", thinking: "", signature: "cached-sig" },
  });
  assert.equal(cached.losses.length, 0);
  const stoppedCached = unsigned.Apply({
    type: "content_block_stop",
    index: 0,
  });
  assert.deepEqual(
    stoppedCached.value.at(-1)?.type,
    "response.output_item.done",
  );
  assert.deepEqual(
    stoppedCached.losses.map(({ field, reason }) => ({ field, reason })),
    [{ field: "signature", reason: "unmapped-field" }],
  );
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
