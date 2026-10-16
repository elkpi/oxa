import assert from "node:assert/strict";
import test from "node:test";
import {
  ResponsesStreamDecoder,
  type ResponsesStreamEvent,
} from "../src/openai/responses/index.js";

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
const added: ResponsesStreamEvent = {
  type: "response.output_item.added",
  output_index: 0,
  item: {
    type: "custom_tool_call",
    id: "ctc_0",
    call_id: "call_1",
    name: "run",
    input: "",
  },
};

test("custom stream requires added and item.done input fields", () => {
  const noAddedInput = new ResponsesStreamDecoder();
  noAddedInput.Feed(created);
  assert.throws(
    () =>
      noAddedInput.Feed({
        type: "response.output_item.added",
        output_index: 0,
        item: {
          type: "custom_tool_call",
          id: "ctc_0",
          call_id: "call_1",
          name: "run",
        },
      } as unknown as ResponsesStreamEvent),
    { code: "stream-lifecycle" },
  );
  const noFinalInput = new ResponsesStreamDecoder();
  noFinalInput.Feed(created);
  noFinalInput.Feed(added);
  assert.throws(
    () =>
      noFinalInput.Feed({
        type: "response.output_item.done",
        output_index: 0,
        item: {
          type: "custom_tool_call",
          id: "ctc_0",
          call_id: "call_1",
          name: "run",
        },
      } as unknown as ResponsesStreamEvent),
    { code: "stream-lifecycle" },
  );
});

test("custom stream rejects conflicting repeated snapshots and duplicate closure", () => {
  const conflict = new ResponsesStreamDecoder();
  conflict.Feed(created);
  conflict.Feed(added);
  conflict.Feed({
    type: "response.custom_tool_call_input.done",
    item_id: "ctc_0",
    output_index: 0,
    input: "one",
  });
  assert.throws(
    () =>
      conflict.Feed({
        type: "response.custom_tool_call_input.done",
        item_id: "ctc_0",
        output_index: 0,
        input: "two",
      }),
    { code: "stream-lifecycle" },
  );

  const duplicate = new ResponsesStreamDecoder();
  duplicate.Feed(created);
  duplicate.Feed(added);
  duplicate.Feed({
    type: "response.output_item.done",
    output_index: 0,
    item: {
      type: "custom_tool_call",
      id: "ctc_0",
      call_id: "call_1",
      name: "run",
      input: "",
    },
  });
  assert.throws(
    () =>
      duplicate.Feed({
        type: "response.output_item.done",
        output_index: 0,
        item: {
          type: "custom_tool_call",
          id: "ctc_0",
          call_id: "call_1",
          name: "run",
          input: "",
        },
      }),
    { code: "stream-lifecycle" },
  );
});
