import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/openai/responses/index.js";
import { integer } from "../src/json/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("runs every Responses non-stream vector", () => {
  assert.ok(
    runNonstreamVectors("responses", {
      decodeRequest,
      decodeResponse,
      encodeRequest,
      encodeResponse,
    }) > 0,
  );
});

test("decodes a reasoning output item and reports encrypted content loss", () => {
  const decoded = decodeResponse({
    id: "resp_reasoning1",
    object: "response",
    model: "o3-mini",
    status: "completed",
    output: [
      {
        type: "reasoning",
        id: "rs_01",
        summary: [
          { type: "output_text", text: "Step 1: analyze.", annotations: [] },
          { type: "output_text", text: "Step 2: conclude.", annotations: [] },
        ],
        encrypted_content: "gAAAAABnz...",
      },
      {
        type: "message",
        id: "msg_01",
        role: "assistant",
        status: "completed",
        content: [
          { type: "output_text", text: "Final answer is 42.", annotations: [] },
        ],
      },
    ],
    usage: {
      input_tokens: integer(12n),
      output_tokens: integer(18n),
      total_tokens: integer(30n),
    },
  });
  assert.deepEqual(decoded.value.content, [
    { type: "thinking", thinking: "Step 1: analyze." },
    { type: "thinking", thinking: "Step 2: conclude." },
    { type: "text", text: "Final answer is 42." },
  ]);
  assert.deepEqual(
    decoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [
      {
        path: "output[0].encrypted_content",
        field: "encrypted_content",
        reason: "unmapped-field",
      },
    ],
  );
});

test("decodes reasoning request effort and drops the summary preference", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    input: "Solve this riddle.",
    reasoning: { effort: "high", summary: "auto" },
  });
  assert.equal(decoded.value.params?.reasoning_effort, "high");
  assert.deepEqual(
    decoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [{ path: "reasoning.summary", field: "summary", reason: "unmapped-field" }],
  );
});

test("drops an invalid reasoning effort with one unmapped-value loss", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    input: "Solve this riddle.",
    reasoning: { effort: "ultra" },
  });
  assert.equal(decoded.value.params?.reasoning_effort, undefined);
  assert.deepEqual(
    decoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [{ path: "reasoning.effort", field: "effort", reason: "unmapped-value" }],
  );
});

test("decodes reasoning input items into assistant thinking blocks", () => {
  const decoded = decodeRequest({
    model: "o3-mini",
    input: [
      { type: "message", role: "user", content: "question" },
      {
        type: "reasoning",
        id: "rs_9",
        summary: [{ type: "summary_text", text: "recap" }],
      },
      { type: "message", role: "assistant", content: "answer" },
    ],
  });
  assert.deepEqual(decoded.value.messages[1]?.content, [
    { type: "thinking", thinking: "recap" },
    { type: "text", text: "answer" },
  ]);
  assert.deepEqual(decoded.losses, []);
});

test("accepts reasoning request items with absent or empty summaries", () => {
  const summaries = [undefined, []] as const;
  for (const summary of summaries) {
    const decoded = decodeRequest({
      model: "o3-mini",
      input: [
        { type: "message", role: "user", content: "question" },
        {
          type: "reasoning",
          id: "rs_empty",
          ...(summary === undefined ? {} : { summary }),
        },
        { type: "message", role: "assistant", content: "answer" },
      ],
    });

    assert.deepEqual(decoded.value.messages[1]?.content, [
      { type: "text", text: "answer" },
    ]);
    assert.deepEqual(decoded.losses, []);
  }
});

test("encodes thinking blocks as reasoning items and reports signature loss", () => {
  const encoded = encodeResponse({
    id: "resp_reasoning1",
    model: "o3-mini",
    content: [
      { type: "thinking", thinking: "Step 1.", signature: "sig" },
      { type: "text", text: "Final answer is 42." },
    ],
    stop_reason: "end_turn",
    usage: { input_tokens: 12n, output_tokens: 18n },
  });
  assert.deepEqual(encoded.value.output, [
    {
      type: "reasoning",
      id: "rs_abc123",
      summary: [{ type: "output_text", text: "Step 1.", annotations: [] }],
    },
    {
      type: "message",
      id: "msg_abc123",
      status: "completed",
      role: "assistant",
      content: [
        { type: "output_text", text: "Final answer is 42.", annotations: [] },
      ],
    },
  ]);
  assert.deepEqual(
    encoded.losses.map(({ field, reason }) => ({ field, reason })),
    [{ field: "signature", reason: "unmapped-field" }],
  );
});

test("encodes request reasoning effort and thinking input items", () => {
  const encoded = encodeRequest({
    model: "o3-mini",
    messages: [
      { role: "user", content: [{ type: "text", text: "Solve this riddle." }] },
      {
        role: "assistant",
        content: [{ type: "thinking", thinking: "recap" }],
      },
    ],
    params: { reasoning_effort: "high" },
  });
  assert.equal(
    (encoded.value.reasoning as { effort?: string } | undefined)?.effort,
    "high",
  );
  const items = encoded.value.input as readonly Record<string, unknown>[];
  assert.deepEqual(items, [
    { role: "user", content: "Solve this riddle." },
    {
      type: "reasoning",
      id: "rs_abc123",
      summary: [{ type: "output_text", text: "recap", annotations: [] }],
    },
  ]);
  assert.deepEqual(encoded.losses, []);
});

test("rejects negative and out-of-range Responses usage details", () => {
  const invalidValues = [
    integer(-1n),
    integer(9_223_372_036_854_775_808n),
  ];
  for (const value of invalidValues) {
    for (const usageDetail of [
      { input_token_details: { cached_tokens: value } },
      { output_token_details: { reasoning_tokens: value } },
    ]) {
      assert.throws(() =>
        decodeResponse({
          id: "resp_invalid_usage_details",
          object: "response",
          model: "o3-mini",
          status: "completed",
          output: [],
          usage: {
            input_tokens: integer(1n),
            output_tokens: integer(2n),
            total_tokens: integer(3n),
            ...usageDetail,
          },
        }),
      );
    }
  }
});

test("decodes responses usage token details", () => {
  const decoded = decodeResponse({
    id: "resp_details",
    object: "response",
    model: "o3-mini",
    status: "completed",
    output: [
      {
        type: "message",
        id: "msg_d1",
        role: "assistant",
        status: "completed",
        content: [
          { type: "output_text", text: "Detailed response.", annotations: [] },
        ],
      },
    ],
    usage: {
      input_tokens: integer(60n),
      output_tokens: integer(30n),
      total_tokens: integer(90n),
      input_token_details: { cached_tokens: integer(50n) },
      output_token_details: { reasoning_tokens: integer(20n) },
    },
  });
  assert.deepEqual(decoded.value.usage, {
    input_tokens: 60n,
    output_tokens: 30n,
    input_tokens_details: { cached_tokens: 50n },
    output_tokens_details: { reasoning_tokens: 20n },
  });
  assert.deepEqual(decoded.losses, []);
});

test("decodes empty assistant content as one empty text block", () => {
  const decoded = decodeRequest({
    model: "gpt-5",
    input: [
      { type: "message", role: "user", content: "hello" },
      { type: "message", role: "assistant", content: [] },
    ],
  });

  assert.deepEqual(decoded.value.messages, [
    { role: "user", content: [{ type: "text", text: "hello" }] },
    { role: "assistant", content: [{ type: "text", text: "" }] },
  ]);
  assert.deepEqual(decoded.losses, []);
});

test("decodes empty user content as one empty text block", () => {
  const decoded = decodeRequest({
    model: "gpt-5",
    input: [{ type: "message", role: "user", content: [] }],
  });

  assert.deepEqual(decoded.value.messages, [
    { role: "user", content: [{ type: "text", text: "" }] },
  ]);
  assert.deepEqual(decoded.losses, []);
});
