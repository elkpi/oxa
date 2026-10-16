import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeEventStream,
  decodeRequest as decodeIrRequest,
  encodeEventStream,
  encodeRequest as encodeIrRequest,
} from "../src/ir/index.js";
import {
  ResponsesStreamDecoder,
  ResponsesStreamEncoder,
  decodeRequest as decodeResponsesRequest,
} from "../src/openai/responses/index.js";
import type { ResponsesStreamEvent } from "../src/openai/responses/index.js";
import type { Event } from "../src/ir/index.js";

const usage = { input_tokens: 1n, output_tokens: 2n };
const created: ResponsesStreamEvent = {
  type: "response.created",
  response: {
    id: "r",
    object: "response",
    status: "in_progress",
    model: "m",
    output: [],
  },
};
const added = (input: string = ""): ResponsesStreamEvent => ({
  type: "response.output_item.added",
  output_index: 0,
  item: {
    type: "custom_tool_call",
    id: "ctc_0",
    call_id: "call_1",
    name: "code_exec",
    input,
  },
});

test("IR 0.2.0 codecs preserve custom formats, raw strings, results, and empty deltas", () => {
  const request = decodeIrRequest({
    specVersion: "0.2.0",
    model: "m",
    messages: [
      { role: "user", content: [{ type: "text", text: "go" }] },
      {
        role: "assistant",
        content: [
          {
            type: "custom_tool_use",
            id: "c",
            name: "run",
            input: "not JSON\r\n\u0000",
          },
        ],
      },
      {
        role: "user",
        content: [{ type: "custom_tool_result", tool_use_id: "c", output: [] }],
      },
    ],
    tools: [
      {
        type: "custom",
        name: "run",
        format: {
          type: "grammar",
          syntax: "lark",
          definition: 'start: "hi"\n',
        },
      },
    ],
    tool_choice: { mode: "tool", name: "run", tool_type: "custom" },
  });
  const encoded = encodeIrRequest(request);
  assert.equal(encoded.specVersion, "0.2.0");
  assert.deepEqual(decodeIrRequest(encoded), request);
  assert.deepEqual(request.messages[1]?.content[0], {
    type: "custom_tool_use",
    id: "c",
    name: "run",
    input: "not JSON\r\n\u0000",
  });

  const stream = {
    events: [
      { type: "message_start" as const, id: "r", model: "m" },
      {
        type: "content_block_start" as const,
        index: 0,
        block: {
          type: "custom_tool_use" as const,
          id: "c",
          name: "run",
          input: "",
        },
      },
      {
        type: "content_block_delta" as const,
        index: 0,
        delta: { type: "custom_tool_input_delta" as const, text: "" },
      },
      { type: "content_block_stop" as const, index: 0 },
      {
        type: "message_delta" as const,
        stop_reason: "tool_use" as const,
        usage,
      },
      { type: "message_done" as const },
    ],
  };
  assert.equal(encodeEventStream(stream).specVersion, "0.2.0");
  assert.deepEqual(decodeEventStream(encodeEventStream(stream)), stream);
});

test("Responses custom output drops unsupported parts once and audits supported variants", () => {
  const dropped = decodeResponsesRequest({
    model: "m",
    input: [
      { role: "user", content: "go" },
      { type: "custom_tool_call", call_id: "c", name: "run", input: "" },
      {
        type: "custom_tool_call_output",
        call_id: "c",
        output: [
          {
            type: "input_file",
            file_id: "f",
            text: { irrelevant: false },
            image_url: { ignored: true },
          },
        ],
      },
    ],
  });
  assert.deepEqual(dropped.value.messages[2]?.content, [
    { type: "custom_tool_result", tool_use_id: "c", output: [] },
  ]);
  assert.deepEqual(
    dropped.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [
      {
        path: "input[2].output[0]",
        field: "type",
        reason: "unsupported-semantic",
      },
    ],
  );

  const audited = decodeResponsesRequest({
    model: "m",
    input: [
      { role: "user", content: "go" },
      { type: "custom_tool_call", call_id: "c", name: "run", input: "" },
      {
        type: "custom_tool_call_output",
        call_id: "c",
        output: [
          { type: "input_text", text: "ok", image_url: { unknown: true } },
        ],
      },
    ],
    tools: [
      {
        type: "custom",
        name: "run",
        format: {
          type: "grammar",
          syntax: "lark",
          definition: "",
          extra: false,
        },
      },
    ],
  });
  assert.ok(
    audited.losses.some(
      (loss) =>
        loss.path === "input[2].output[0].image_url" &&
        loss.reason === "unmapped-field",
    ),
  );
  assert.ok(
    audited.losses.some(
      (loss) =>
        loss.path === "tools[0].format.extra" &&
        loss.reason === "unmapped-field",
    ),
  );
});

test("Responses custom stream accepts empty fragments, validates snapshots, and rejects absent or null deltas", () => {
  const decoder = new ResponsesStreamDecoder();
  decoder.Feed(created);
  decoder.Feed(added());
  decoder.Feed({
    type: "response.custom_tool_call_input.delta",
    item_id: "ctc_0",
    output_index: 0,
    delta: "",
  });
  decoder.Feed({
    type: "response.custom_tool_call_input.done",
    item_id: "ctc_0",
    output_index: 0,
    input: "",
  });
  const replay = decoder.Feed({
    type: "response.output_item.done",
    output_index: 0,
    item: {
      type: "custom_tool_call",
      id: "ctc_0",
      call_id: "call_1",
      name: "code_exec",
      input: "",
    },
  });
  assert.deepEqual(replay, [
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "custom_tool_use",
        id: "call_1",
        name: "code_exec",
        input: "",
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "" },
    },
    { type: "content_block_stop", index: 0 },
  ]);

  for (const delta of [undefined, null]) {
    const invalid = new ResponsesStreamDecoder();
    invalid.Feed(created);
    invalid.Feed(added());
    const event = {
      type: "response.custom_tool_call_input.delta",
      item_id: "ctc_0",
      output_index: 0,
      ...(delta === undefined ? {} : { delta }),
    } as unknown as ResponsesStreamEvent;
    assert.throws(() => invalid.Feed(event), { code: "stream-lifecycle" });
  }

  const conflict = new ResponsesStreamDecoder();
  conflict.Feed(created);
  conflict.Feed(added());
  conflict.Feed({
    type: "response.custom_tool_call_input.delta",
    item_id: "ctc_0",
    output_index: 0,
    delta: "x",
  });
  conflict.Feed({
    type: "response.custom_tool_call_input.done",
    item_id: "ctc_0",
    output_index: 0,
    input: "x",
  });
  assert.throws(
    () =>
      conflict.Feed({
        type: "response.output_item.done",
        output_index: 0,
        item: {
          type: "custom_tool_call",
          id: "ctc_0",
          call_id: "call_1",
          name: "code_exec",
          input: "y",
        },
      }),
    { code: "stream-lifecycle" },
  );
});

test("Responses stream encoder emits custom native event family without status or function arguments", () => {
  const encoder = new ResponsesStreamEncoder();
  const output: ResponsesStreamEvent[] = [];
  const events: Event[] = [
    { type: "message_start", id: "r", model: "m" },
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "custom_tool_use",
        id: "call_1",
        name: "code_exec",
        input: "abc",
      },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "a" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "custom_tool_input_delta", text: "bc" },
    },
    { type: "content_block_stop", index: 0 },
    { type: "message_delta", stop_reason: "tool_use", usage },
    { type: "message_done" },
  ];
  for (const event of events) output.push(...encoder.Apply(event).value);
  assert.deepEqual(output.slice(0, 5), [
    {
      type: "response.created",
      response: {
        id: "r",
        object: "response",
        status: "in_progress",
        model: "m",
        output: [],
      },
    },
    {
      type: "response.output_item.added",
      output_index: 0,
      item: {
        type: "custom_tool_call",
        id: "ctc_0",
        call_id: "call_1",
        name: "code_exec",
        input: "",
      },
    },
    {
      type: "response.custom_tool_call_input.delta",
      item_id: "ctc_0",
      output_index: 0,
      delta: "a",
    },
    {
      type: "response.custom_tool_call_input.delta",
      item_id: "ctc_0",
      output_index: 0,
      delta: "bc",
    },
    {
      type: "response.custom_tool_call_input.done",
      item_id: "ctc_0",
      output_index: 0,
      input: "abc",
    },
  ]);
  assert.equal(
    output.some(
      (event) => event.type === "response.function_call_arguments.delta",
    ),
    false,
  );
});
