import assert from "node:assert/strict";
import test from "node:test";

import { ChatCompletionsStreamDecoder } from "../src/openai/chatcompletions/index.js";
import { jsonText } from "../src/json/index.js";

test("replays interleaved M7 tool calls in index order with exact raw fragments", () => {
  const decoder = new ChatCompletionsStreamDecoder();

  assert.deepEqual(
    decoder.Feed({
      id: "chatcmpl-1",
      model: "gpt-4o-mini",
      choices: [{ delta: { role: "assistant" }, finish_reason: null }],
    }),
    [{ type: "message_start", id: "chatcmpl-1", model: "gpt-4o-mini" }],
  );
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            {
              index: 0,
              id: "call_1",
              type: "function",
              function: { name: "wea", arguments: "" },
            },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            {
              index: 1,
              id: "call_2",
              type: "function",
              function: { name: "clo", arguments: "" },
            },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            {
              index: 0,
              function: {
                name: "ther",
                arguments: '{"city": "P\\u0041ris", "label": "东京",',
              },
            },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            {
              index: 1,
              function: {
                name: "ck",
                arguments: '{"tz": "Asia\\/Shanghai",',
              },
            },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: { tool_calls: [{ index: 0, function: { arguments: "" } }] },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            { index: 0, function: { arguments: ' "days": 1e+01}' } },
          ],
        },
        finish_reason: null,
      },
    ],
  });
  decoder.Feed({
    id: "chatcmpl-1",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            { index: 1, function: { arguments: ' "hour": 1e+01}' } },
          ],
        },
        finish_reason: "tool_calls",
      },
    ],
  });
  assert.deepEqual(
    decoder.Feed({
      id: "chatcmpl-1",
      model: "gpt-4o-mini",
      choices: [],
      usage: { prompt_tokens: 21, completion_tokens: 13, total_tokens: 34 },
    }),
    [],
  );

  assert.deepEqual(decoder.Flush(), [
    {
      type: "content_block_start",
      index: 0,
      block: {
        type: "tool_use",
        id: "call_1",
        name: "weather",
        input: jsonText(
          '{"city": "P\\u0041ris", "label": "东京", "days": 1e+01}',
        ),
      },
    },
    ...[
      "",
      '{"city": "P\\u0041ris", "label": "东京",',
      "",
      ' "days": 1e+01}',
    ].map((partial_json) => ({
      type: "content_block_delta" as const,
      index: 0,
      delta: {
        type: "input_json_delta" as const,
        partial_json: jsonText(partial_json),
      },
    })),
    { type: "content_block_stop", index: 0 },
    {
      type: "content_block_start",
      index: 1,
      block: {
        type: "tool_use",
        id: "call_2",
        name: "clock",
        input: jsonText('{"tz": "Asia\\/Shanghai", "hour": 1e+01}'),
      },
    },
    ...["", '{"tz": "Asia\\/Shanghai",', ' "hour": 1e+01}'].map(
      (partial_json) => ({
        type: "content_block_delta" as const,
        index: 1,
        delta: {
          type: "input_json_delta" as const,
          partial_json: jsonText(partial_json),
        },
      }),
    ),
    { type: "content_block_stop", index: 1 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 21n, output_tokens: 13n },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Losses(), []);
});
