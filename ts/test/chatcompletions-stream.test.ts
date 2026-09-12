import assert from "node:assert/strict";
import test from "node:test";

import { chatcompletions } from "../src/index.js";
import {
  ChatCompletionsStreamDecoder,
  ChatCompletionsStreamEncoder,
} from "../src/openai/chatcompletions/index.js";
import { jsonText } from "../src/json/index.js";

test("exports Chat Completions stream converters from the package root", () => {
  assert.equal(typeof chatcompletions.ChatCompletionsStreamDecoder, "function");
  assert.equal(typeof chatcompletions.ChatCompletionsStreamEncoder, "function");
});

test("rejects a changed native stream identity", () => {
  const decoder = new ChatCompletionsStreamDecoder();
  decoder.Feed({
    id: "chatcmpl-identity-a",
    model: "gpt-4o-mini",
    choices: [{ delta: { role: "assistant" }, finish_reason: null }],
  });

  assert.throws(
    () =>
      decoder.Feed({
        id: "chatcmpl-identity-b",
        model: "gpt-4o-mini",
        choices: [{ delta: { content: "ignored" }, finish_reason: null }],
      }),
    { code: "stream-lifecycle" },
  );
});

test("rejects a changed native stream model", () => {
  const decoder = new ChatCompletionsStreamDecoder();
  decoder.Feed({
    id: "chatcmpl-model",
    model: "gpt-4o-mini",
    choices: [{ delta: { role: "assistant" }, finish_reason: null }],
  });

  assert.throws(
    () =>
      decoder.Feed({
        id: "chatcmpl-model",
        model: "gpt-4.1-mini",
        choices: [{ delta: { content: "ignored" }, finish_reason: null }],
      }),
    { code: "stream-lifecycle" },
  );
});

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

test("emits text live before replaying M7 tool calls at flush", () => {
  const decoder = new ChatCompletionsStreamDecoder();
  assert.deepEqual(
    decoder.Feed({
      id: "chatcmpl-text",
      model: "gpt-4o-mini",
      choices: [
        {
          delta: { role: "assistant", content: "Checking." },
          finish_reason: null,
        },
      ],
    }),
    [
      { type: "message_start", id: "chatcmpl-text", model: "gpt-4o-mini" },
      {
        type: "content_block_start",
        index: 0,
        block: { type: "text", text: "" },
      },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "text_delta", text: "Checking." },
      },
    ],
  );
  decoder.Feed({
    id: "chatcmpl-text",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: {
          tool_calls: [
            {
              index: 0,
              id: "call_1",
              type: "function",
              function: { name: "weather", arguments: "{}" },
            },
          ],
        },
        finish_reason: "tool_calls",
      },
    ],
  });

  assert.deepEqual(decoder.Flush(), [
    { type: "content_block_stop", index: 0 },
    {
      type: "content_block_start",
      index: 1,
      block: {
        type: "tool_use",
        id: "call_1",
        name: "weather",
        input: jsonText("{}"),
      },
    },
    {
      type: "content_block_delta",
      index: 1,
      delta: { type: "input_json_delta", partial_json: jsonText("{}") },
    },
    { type: "content_block_stop", index: 1 },
    {
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ]);
});

test("encodes M7 tool calls with raw fragments and normalizes later text", () => {
  const encoder = new ChatCompletionsStreamEncoder();
  const apply = (event: Parameters<typeof encoder.Apply>[0]) =>
    encoder.Apply(event);
  assert.deepEqual(
    apply({ type: "message_start", id: "chatcmpl-9", model: "gpt-4o-mini" }),
    {
      value: [
        {
          id: "chatcmpl-9",
          object: "chat.completion.chunk",
          created: 0,
          model: "gpt-4o-mini",
          choices: [
            { index: 0, delta: { role: "assistant" }, finish_reason: null },
          ],
        },
      ],
      losses: [],
    },
  );
  apply({
    type: "content_block_start",
    index: 0,
    block: { type: "text", text: "" },
  });
  assert.deepEqual(
    apply({
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "Checking." },
    }).value,
    [
      {
        id: "chatcmpl-9",
        object: "chat.completion.chunk",
        created: 0,
        model: "gpt-4o-mini",
        choices: [
          { index: 0, delta: { content: "Checking." }, finish_reason: null },
        ],
      },
    ],
  );
  apply({ type: "content_block_stop", index: 0 });
  apply({
    type: "content_block_start",
    index: 1,
    block: {
      type: "tool_use",
      id: "call_1",
      name: "weather",
      input: jsonText('{"city": "Paris", "label": "东京"}'),
    },
  });
  apply({
    type: "content_block_delta",
    index: 1,
    delta: { type: "input_json_delta", partial_json: jsonText('{"city":') },
  });
  apply({
    type: "content_block_delta",
    index: 1,
    delta: { type: "input_json_delta", partial_json: jsonText("") },
  });
  apply({
    type: "content_block_delta",
    index: 1,
    delta: {
      type: "input_json_delta",
      partial_json: jsonText(' "Paris", "label": "东京"}'),
    },
  });
  apply({ type: "content_block_stop", index: 1 });
  apply({
    type: "content_block_start",
    index: 2,
    block: {
      type: "tool_use",
      id: "call_2",
      name: "clock",
      input: jsonText('{"tz": "Asia\\/Shanghai", "hour": 1e+01}'),
    },
  });
  apply({ type: "content_block_stop", index: 2 });
  apply({
    type: "content_block_start",
    index: 3,
    block: { type: "text", text: "" },
  });
  apply({
    type: "content_block_delta",
    index: 3,
    delta: { type: "text_delta", text: "Done." },
  });
  apply({ type: "content_block_stop", index: 3 });
  assert.deepEqual(
    apply({
      type: "message_delta",
      stop_reason: "tool_use",
      usage: { input_tokens: 5n, output_tokens: 7n },
    }),
    {
      value: [
        ...[
          {
            index: 0,
            id: "call_1",
            type: "function",
            function: { name: "weather", arguments: '{"city":' },
          },
          { index: 0, function: { arguments: "" } },
          { index: 0, function: { arguments: ' "Paris", "label": "东京"}' } },
          {
            index: 1,
            id: "call_2",
            type: "function",
            function: {
              name: "clock",
              arguments: '{"tz": "Asia\\/Shanghai", "hour": 1e+01}',
            },
          },
        ].map((toolCall) => ({
          id: "chatcmpl-9",
          object: "chat.completion.chunk" as const,
          created: 0,
          model: "gpt-4o-mini",
          choices: [
            {
              index: 0,
              delta: { tool_calls: [toolCall] },
              finish_reason: null,
            },
          ],
        })),
        {
          id: "chatcmpl-9",
          object: "chat.completion.chunk",
          created: 0,
          model: "gpt-4o-mini",
          choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }],
          usage: { prompt_tokens: 5, completion_tokens: 7, total_tokens: 12 },
        },
      ],
      losses: [
        {
          path: "events",
          field: "ordering",
          reason: "degraded",
          detail:
            "N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved",
        },
      ],
    },
  );
  assert.deepEqual(apply({ type: "message_done" }), { value: [], losses: [] });
});
