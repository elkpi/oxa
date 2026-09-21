import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/anthropic/messages/index.js";
import { OxaError } from "../src/error.js";
import {
  integer,
  jsonText,
  parseJson,
  stringifyJson,
  type JsonObject,
} from "../src/json/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("runs every Anthropic non-stream vector", () => {
  assert.ok(
    runNonstreamVectors("anthropic", {
      decodeRequest,
      decodeResponse,
      encodeRequest,
      encodeResponse,
    }) > 0,
  );
});

test("maps an Anthropic thinking response block and cache usage", () => {
  const decoded = decodeResponse({
    id: "msg_think",
    type: "message",
    role: "assistant",
    model: "claude-3-7-sonnet-20250219",
    content: [
      {
        type: "thinking",
        thinking: "Let me reason through this carefully.",
        signature: "sig_abc123opaque",
      },
      { type: "text", text: "Here is the final answer." },
    ],
    stop_reason: "end_turn",
    usage: {
      input_tokens: integer(15n),
      output_tokens: integer(25n),
      cache_read_input_tokens: integer(3n),
      cache_creation_input_tokens: integer(4n),
    },
  });
  assert.deepEqual(decoded.value.content, [
    {
      type: "thinking",
      thinking: "Let me reason through this carefully.",
      signature: "sig_abc123opaque",
    },
    { type: "text", text: "Here is the final answer." },
  ]);
  assert.deepEqual(decoded.value.usage, {
    input_tokens: 15n,
    output_tokens: 25n,
    cache_read_input_tokens: 3n,
    cache_creation_input_tokens: 4n,
  });
  assert.deepEqual(decoded.losses, []);
});

test("encodes thinking blocks verbatim including opaque signatures", () => {
  const encoded = encodeResponse({
    id: "msg_think",
    model: "claude-3-7-sonnet-20250219",
    content: [
      {
        type: "thinking",
        thinking: "Let me reason through this carefully.",
        signature: "sig_abc123opaque",
      },
      { type: "text", text: "Here is the final answer." },
    ],
    stop_reason: "end_turn",
    usage: {
      input_tokens: 15n,
      output_tokens: 25n,
      cache_read_input_tokens: 3n,
    },
  });
  const content = encoded.value.content as unknown as readonly JsonObject[];
  assert.equal(
    stringifyJson(content[0]!),
    '{"type":"thinking","thinking":"Let me reason through this carefully.","signature":"sig_abc123opaque"}',
  );
  assert.deepEqual(encoded.losses, []);
  const usage = encoded.value.usage as unknown as JsonObject;
  assert.deepEqual(usage.cache_read_input_tokens, integer(3n));
});

test("maps reasoning effort budgets in both directions with degraded losses", () => {
  const decodeCases = [
    [2048n, "low"],
    [2049n, "medium"],
    [8192n, "medium"],
    [8193n, "high"],
    [16384n, "high"],
  ] as const;
  for (const [budget, effort] of decodeCases) {
    const decoded = decodeRequest({
      model: "claude-3-7-sonnet-20250219",
      max_tokens: integer(16000n),
      messages: [{ role: "user", content: "Solve this puzzle." }],
      thinking: { type: "enabled", budget_tokens: integer(budget) },
    });
    assert.equal(decoded.value.params?.reasoning_effort, effort, `budget ${budget}`);
    assert.deepEqual(
      decoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
      [
        {
          path: "thinking.budget_tokens",
          field: "budget_tokens",
          reason: "degraded",
        },
      ],
      `budget ${budget}`,
    );
  }

  const encodeCases = [
    ["minimal", 1024n],
    ["low", 2048n],
    ["medium", 8192n],
    ["high", 16384n],
  ] as const;
  for (const [effort, budget] of encodeCases) {
    const encoded = encodeRequest({
      model: "claude-3-7-sonnet-20250219",
      messages: [
        { role: "user", content: [{ type: "text", text: "Solve this puzzle." }] },
      ],
      params: { max_tokens: 4096n, reasoning_effort: effort },
    });
    assert.deepEqual(
      (encoded.value.thinking as JsonObject | undefined)?.budget_tokens,
      integer(budget),
      `effort ${effort}`,
    );
    assert.deepEqual(
      encoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
      [
        {
          path: "params.reasoning_effort",
          field: "reasoning_effort",
          reason: "degraded",
        },
      ],
      `effort ${effort}`,
    );
  }
});

test("encodes unsigned request thinking blocks with a degraded signature loss", () => {
  const encoded = encodeRequest({
    model: "claude-3-7-sonnet-20250219",
    messages: [
      { role: "user", content: [{ type: "text", text: "question" }] },
      {
        role: "assistant",
        content: [{ type: "thinking", thinking: "replay reasoning" }],
      },
    ],
    params: { max_tokens: 32n },
  });
  const messages = encoded.value.messages as readonly {
    content?: unknown;
  }[];
  assert.deepEqual(messages[1]!.content, [
    { type: "thinking", thinking: "replay reasoning" },
  ]);
  assert.deepEqual(
    encoded.losses.map(({ path, field, reason }) => ({ path, field, reason })),
    [{ path: "messages[1].content[0]", field: "signature", reason: "degraded" }],
  );
});

test("raw tool input preserves source bytes", () => {
  const rawInput = jsonText('{ "b" : "\\u0041", "a" : 1e+01 }');
  const source = `{"id":"msg_raw","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"tool_use","id":"toolu_raw","name":"inspect","input":${rawInput}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":2}}`;

  const decoded = decodeResponse(parseJson(source) as JsonObject);
  const encoded = encodeResponse(decoded.value);

  assert.deepEqual(decoded.losses, []);
  assert.deepEqual(encoded.losses, []);
  assert.equal(stringifyJson(encoded.value), source);
  const block = decoded.value.content[0]!;
  if (block.type !== "tool_use") assert.fail("expected tool_use block");
  assert.equal(block.input, rawInput);
});

test("generic in-memory tool input uses compact canonical text", () => {
  const decoded = decodeResponse({
    id: "msg_generic",
    type: "message",
    role: "assistant",
    model: "claude-sonnet-4-5",
    content: [
      {
        type: "tool_use",
        id: "toolu_generic",
        name: "inspect",
        input: { b: "A", a: integer(10n) },
      },
    ],
    stop_reason: "tool_use",
    usage: { input_tokens: integer(1n), output_tokens: integer(2n) },
  });

  assert.deepEqual(decoded.losses, []);
  const block = decoded.value.content[0]!;
  if (block.type !== "tool_use") assert.fail("expected tool_use block");
  assert.equal(block.input, jsonText('{"b":"A","a":10}'));
});

test("rejects tool input sidecar that is not an object token", () => {
  assert.throws(
    () =>
      decodeResponse({
        id: "msg_sidecar",
        type: "message",
        role: "assistant",
        model: "claude-sonnet-4-5",
        content: [
          {
            type: "tool_use",
            id: "toolu_sidecar",
            name: "inspect",
            input: {},
            inputText: jsonText("[]"),
          },
        ],
        stop_reason: "tool_use",
        usage: { input_tokens: integer(1n), output_tokens: integer(2n) },
      }),
    (error: unknown) =>
      error instanceof OxaError && error.code === "type-violation",
  );
});
