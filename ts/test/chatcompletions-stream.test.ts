import assert from "node:assert/strict";
import test from "node:test";

import { chatcompletions } from "../src/index.js";
import {
  ChatCompletionsStreamDecoder,
  ChatCompletionsStreamEncoder,
  decodeRequest,
  encodeResponse,
} from "../src/openai/chatcompletions/index.js";
import { jsonText } from "../src/json/index.js";

test("decodes assistant reasoning_content and request reasoning_effort", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    messages: [
      { role: "user", content: "question" },
      { role: "assistant", reasoning_content: "plan", content: "answer" },
    ],
    reasoning_effort: "high",
  });
  assert.deepEqual(decoded.value.messages[1]?.content[0], {
    type: "thinking",
    thinking: "plan",
  });
  assert.equal(decoded.value.params?.reasoning_effort, "high");
  assert.deepEqual(decoded.losses, []);
});

test("drops an unknown reasoning_effort with exactly one unmapped-value loss", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    messages: [{ role: "user", content: "question" }],
    reasoning_effort: "ultra",
  });
  assert.equal(decoded.value.params?.reasoning_effort, undefined);
  assert.deepEqual(
    decoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [{ path: "reasoning_effort", field: "reasoning_effort", reason: "unmapped-value" }],
  );
});

test("encodes a signed thinking block with exactly one signature loss", () => {
  const encoded = encodeResponse({
    id: "chatcmpl-reasoning123",
    model: "o3-mini",
    content: [
      { type: "thinking", thinking: "plan", signature: "sig" },
      { type: "text", text: "42 is the answer." },
    ],
    stop_reason: "end_turn",
    usage: { input_tokens: 10n, output_tokens: 20n },
  });
  const choices = encoded.value.choices as unknown as readonly {
    message: { reasoning_content?: string; content?: string };
  }[];
  const message = choices[0]!.message;
  assert.equal(message.reasoning_content, "plan");
  assert.equal(message.content, "42 is the answer.");
  assert.deepEqual(
    encoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [{ path: "content[0].signature", field: "signature", reason: "unmapped-field" }],
  );
});

test("streams reasoning content before text and preserves usage details", () => {
  const decoder = new ChatCompletionsStreamDecoder();
  const actual = [
    decoder.Feed({
      id: "chatcmpl-stream-think",
      model: "o3-mini",
      choices: [
        { delta: { role: "assistant", reasoning_content: "Pondering " }, finish_reason: null },
      ],
    }),
    decoder.Feed({
      id: "chatcmpl-stream-think",
      model: "o3-mini",
      choices: [{ delta: { reasoning_content: "deeply..." }, finish_reason: null }],
    }),
    decoder.Feed({
      id: "chatcmpl-stream-think",
      model: "o3-mini",
      choices: [{ delta: { content: "42." }, finish_reason: "stop" }],
      usage: {
        prompt_tokens: 8n,
        completion_tokens: 12n,
        total_tokens: 20n,
        prompt_tokens_details: { cached_tokens: 0n },
        completion_tokens_details: { reasoning_tokens: 6n },
      },
    }),
    decoder.Flush(),
  ].flat();
  assert.deepEqual(actual, [
    { type: "message_start", id: "chatcmpl-stream-think", model: "o3-mini" },
    { type: "content_block_start", index: 0, block: { type: "thinking", thinking: "" } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "Pondering " } },
    { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", text: "deeply..." } },
    { type: "content_block_stop", index: 0 },
    { type: "content_block_start", index: 1, block: { type: "text", text: "" } },
    { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "42." } },
    { type: "content_block_stop", index: 1 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: {
        input_tokens: 8n,
        output_tokens: 12n,
        input_tokens_details: { cached_tokens: 0n },
        output_tokens_details: { reasoning_tokens: 6n },
      },
    },
    { type: "message_done" },
  ]);
  assert.deepEqual(decoder.Losses(), []);
});

test("encodes thinking deltas as reasoning_content chunks with terminal details", () => {
  const encoder = new ChatCompletionsStreamEncoder();
  const chunks = [
    encoder.Apply({
      type: "message_start",
      id: "chatcmpl-from-think",
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
      delta: { type: "thinking_delta", text: "Reasoning " },
    }),
    encoder.Apply({
      type: "content_block_delta",
      index: 0,
      delta: { type: "thinking_delta", text: "text." },
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
      delta: { type: "text_delta", text: "Answer." },
    }),
    encoder.Apply({ type: "content_block_stop", index: 1 }),
    encoder.Apply({
      type: "message_delta",
      stop_reason: "end_turn",
      usage: {
        input_tokens: 8n,
        output_tokens: 12n,
        input_tokens_details: { cached_tokens: 4n },
        output_tokens_details: { reasoning_tokens: 8n },
      },
    }),
    encoder.Apply({ type: "message_done" }),
  ];
  for (const result of chunks) assert.deepEqual(result.losses, []);
  assert.deepEqual(
    chunks.flatMap(({ value }) => value),
    [
      {
        id: "chatcmpl-from-think",
        object: "chat.completion.chunk",
        created: 0,
        model: "o3-mini",
        choices: [{ index: 0, delta: { role: "assistant" }, finish_reason: null }],
      },
      {
        id: "chatcmpl-from-think",
        object: "chat.completion.chunk",
        created: 0,
        model: "o3-mini",
        choices: [
          { index: 0, delta: { reasoning_content: "Reasoning " }, finish_reason: null },
        ],
      },
      {
        id: "chatcmpl-from-think",
        object: "chat.completion.chunk",
        created: 0,
        model: "o3-mini",
        choices: [
          { index: 0, delta: { reasoning_content: "text." }, finish_reason: null },
        ],
      },
      {
        id: "chatcmpl-from-think",
        object: "chat.completion.chunk",
        created: 0,
        model: "o3-mini",
        choices: [{ index: 0, delta: { content: "Answer." }, finish_reason: null }],
      },
      {
        id: "chatcmpl-from-think",
        object: "chat.completion.chunk",
        created: 0,
        model: "o3-mini",
        choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
        usage: {
          prompt_tokens: 8n,
          completion_tokens: 12n,
          total_tokens: 20n,
          prompt_tokens_details: { cached_tokens: 4n },
          completion_tokens_details: { reasoning_tokens: 8n },
        },
      },
    ],
  );
});

test("reports each stream signature source exactly once without native chunks", () => {
  const encoder = new ChatCompletionsStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "chatcmpl-signature",
    model: "o3-mini",
  });
  const started = encoder.Apply({
    type: "content_block_start",
    index: 0,
    block: { type: "thinking", thinking: "", signature: "start-sig" },
  });
  assert.deepEqual(started.value, []);
  assert.deepEqual(
    started.losses.map(({ field, reason }) => ({ field, reason })),
    [{ field: "signature", reason: "unmapped-field" }],
  );
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
  const ended = encoder.Apply({ type: "content_block_stop", index: 0 });
  assert.deepEqual(ended.value, []);
  assert.deepEqual(ended.losses, []);
});

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
      usage: { prompt_tokens: 21n, completion_tokens: 13n, total_tokens: 34n },
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
          usage: {
            prompt_tokens: 5n,
            completion_tokens: 7n,
            total_tokens: 12n,
          },
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

test("Chat Completions stream usage preserves lossless int64 values", () => {
  for (const value of [9_007_199_254_740_993n, 9_223_372_036_854_775_807n]) {
    const decoder = new ChatCompletionsStreamDecoder();
    decoder.Feed({
      id: "chatcmpl-usage",
      model: "gpt-4o-mini",
      choices: [{ delta: { role: "assistant" }, finish_reason: null }],
    });
    decoder.Feed({
      id: "chatcmpl-usage",
      model: "gpt-4o-mini",
      choices: [{ delta: {}, finish_reason: "stop" }],
      usage: {
        prompt_tokens: value,
        completion_tokens: 0n,
        total_tokens: value,
      },
    });
    assert.deepEqual(decoder.Flush().at(-2), {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: value, output_tokens: 0n },
    });
  }

  const encoder = new ChatCompletionsStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "chatcmpl-usage",
    model: "gpt-4o-mini",
  });
  assert.deepEqual(
    encoder.Apply({
      type: "message_delta",
      stop_reason: "end_turn",
      usage: {
        input_tokens: 9_223_372_036_854_775_807n,
        output_tokens: 0n,
      },
    }).value[0]?.usage,
    {
      prompt_tokens: 9_223_372_036_854_775_807n,
      completion_tokens: 0n,
      total_tokens: 9_223_372_036_854_775_807n,
    },
  );
});

test("Chat Completions stream usage rejects invalid values", () => {
  const invalid = [
    9_223_372_036_854_775_808n,
    -1n,
    { kind: "number", token: "1.5", isInteger: false },
  ] as const;
  for (const value of invalid) {
    const decoder = new ChatCompletionsStreamDecoder();
    decoder.Feed({
      id: "chatcmpl-invalid-usage",
      model: "gpt-4o-mini",
      choices: [{ delta: { role: "assistant" }, finish_reason: null }],
    });
    assert.throws(
      () =>
        decoder.Feed({
          id: "chatcmpl-invalid-usage",
          model: "gpt-4o-mini",
          choices: [{ delta: {}, finish_reason: "stop" }],
          usage: {
            prompt_tokens: value,
            completion_tokens: 0n,
            total_tokens: 0n,
          },
        }),
      { code: "invalid-input" },
    );
  }

  const encoder = new ChatCompletionsStreamEncoder();
  encoder.Apply({
    type: "message_start",
    id: "chatcmpl-invalid-usage",
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
