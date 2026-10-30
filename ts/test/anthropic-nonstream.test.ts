import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeRequestJSON,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/anthropic/messages/index.js";
import { OxaError } from "../src/error.js";
import {
  decodeRequestJSON as decodeChatJSON,
  encodeRequest as encodeChat,
} from "../src/openai/chatcompletions/index.js";
import {
  decodeRequestJSON as decodeResponsesJSON,
  encodeRequest as encodeResponses,
} from "../src/openai/responses/index.js";
import {
  encodeRequest as encodeIRRequest,
  decodeRequest as decodeIRRequest,
} from "../src/ir/index.js";
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

test("preserves output schema source bytes through canonical IR and Anthropic JSON", () => {
  const schema = `{ "type" : "object", "properties" : { "n" : {"type":"string"} } }`;
  const raw = `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":${schema}}}}`;
  const decoded = decodeRequestJSON(raw);
  assert.equal(decoded.losses.length, 0);
  const canonical = stringifyJson(encodeIRRequest(decoded.value));
  assert.ok(canonical.includes(schema), canonical);
  const ir = decodeIRRequest(parseJson(canonical));
  const encoded = encodeRequest(ir);
  assert.equal(encoded.losses.length, 0);
  assert.ok(
    stringifyJson(encoded.value).includes(schema),
    stringifyJson(encoded.value),
  );
  const format = ir.params?.response_format;
  assert.equal(format?.type, "json_schema");
  if (format?.type !== "json_schema") throw new Error("missing schema");
  (format.schema as Record<string, unknown>).title = "changed";
  assert.throws(() => encodeRequest(ir));
  assert.throws(() => encodeIRRequest(ir));
});

test("OpenAI to Anthropic preserves schema source bytes", () => {
  const schema = `{ "type" : "object", "maximum" : 1e400 }`;
  const cases = [
    decodeChatJSON(
      `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":${schema}}}}`,
    ),
    decodeResponsesJSON(
      `{"model":"m","max_output_tokens":64,"input":"hi","text":{"format":{"type":"json_schema","name":"result","schema":${schema}}}}`,
    ),
  ];
  for (const result of cases) {
    const canonical = stringifyJson(encodeIRRequest(result.value));
    assert.ok(canonical.includes(schema), canonical);
    const ir = decodeIRRequest(parseJson(canonical));
    const encoded = encodeRequest(ir);
    assert.equal(encoded.losses.length, 1);
    assert.ok(stringifyJson(encoded.value).includes(schema));
  }
});

test("nested schema source edits cannot be hidden by a new outer object", () => {
  const original = decodeChatJSON(
    `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object","properties":{"n":{"type":"string"}}}}}}`,
  ).value;
  const oldFormat = original.params?.response_format;
  if (oldFormat?.type !== "json_schema") throw new Error("missing schema");
  const nested = oldFormat.schema.properties as JsonObject;
  (nested.n as Record<string, unknown>).type = "number";
  const req = {
    ...original,
    params: {
      ...original.params,
      response_format: { ...oldFormat, schema: { properties: nested } },
    },
  };
  assert.throws(() => encodeRequest(req));
  assert.throws(() => encodeIRRequest(req));
});

test("OpenAI encoders reject stale schema source", () => {
  const decoded = decodeChatJSON(
    `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object"}}}}`,
  ).value;
  const format = decoded.params?.response_format;
  if (format?.type !== "json_schema") throw new Error("missing schema");
  (format.schema as Record<string, unknown>).title = "changed";
  assert.throws(() => encodeChat(decoded));
  assert.throws(() => encodeResponses(decoded));
});

test("Responses encoder preserves the original schema bytes", () => {
  const schema = `{ "type" : "object", "maximum" : 1e400 }`;
  const req = decodeResponsesJSON(
    `{"model":"m","input":"hi","text":{"format":{"type":"json_schema","name":"result","schema":${schema}}}}`,
  ).value;
  const encoded = stringifyJson(encodeResponses(req).value);
  assert.ok(encoded.includes(schema), encoded);
});

test("opaque schema objects may contain number-marker properties", () => {
  for (const schema of [
    `{"kind":"number","type":"object"}`,
    `{"kind":"number","token":"1","isInteger":true}`,
  ]) {
    const raw = `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":${schema}}}}`;
    const result = decodeRequestJSON(raw);
    assert.equal(result.losses.length, 0);
    assert.ok(
      stringifyJson(encodeRequest(result.value).value).includes(schema),
    );
  }
});

test("output_config accepts null but rejects non-object values", () => {
  const prefix = `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":`;
  const result = decodeRequestJSON(prefix + "null}");
  assert.equal(result.value.params?.response_format, undefined);
  assert.equal(result.losses.length, 0);
  for (const value of ["[]", "42", '"invalid"', "false"]) {
    assert.throws(() => decodeRequestJSON(prefix + value + "}"), value);
  }
});

test("malformed Anthropic output formats are structural errors", () => {
  for (const format of [
    "null",
    "{}",
    `{"type":"json_schema"}`,
    `{"type":"json_schema","schema":null}`,
    `{"type":"json_schema","schema":[]}`,
    `{"type":false}`,
    `{"type":[]}`,
  ]) {
    assert.throws(
      () =>
        decodeRequestJSON(
          `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":${format}}}`,
        ),
      format,
    );
  }
});

test("OpenAI JSON Schema wire requires a non-empty name", () => {
  assert.throws(() =>
    decodeChatJSON(
      `{"model":"m","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"","schema":{}}}}`,
    ),
  );
  assert.throws(() =>
    decodeResponsesJSON(
      `{"model":"m","input":"hi","text":{"format":{"type":"json_schema","name":"","schema":{}}}}`,
    ),
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
    assert.equal(
      decoded.value.params?.reasoning_effort,
      effort,
      `budget ${budget}`,
    );
    assert.deepEqual(decoded.losses, [], `budget ${budget}`);
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
        {
          role: "user",
          content: [{ type: "text", text: "Solve this puzzle." }],
        },
      ],
      params: { max_tokens: 4096n, reasoning_effort: effort },
    });
    assert.deepEqual(
      (encoded.value.thinking as JsonObject | undefined)?.budget_tokens,
      integer(budget),
      `effort ${effort}`,
    );
    assert.deepEqual(
      encoded.losses.map(({ path, field, reason }) => ({
        path,
        field,
        reason,
      })),
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
    [
      {
        path: "messages[1].content[0]",
        field: "signature",
        reason: "degraded",
      },
    ],
  );
});

test("rejects negative and out-of-range Anthropic cache usage", () => {
  const invalidValues = [integer(-1n), integer(9_223_372_036_854_775_808n)];
  for (const value of invalidValues) {
    for (const usageDetail of [
      { cache_read_input_tokens: value },
      { cache_creation_input_tokens: value },
    ]) {
      assert.throws(() =>
        decodeResponse({
          id: "msg_invalid_usage_details",
          type: "message",
          role: "assistant",
          model: "claude-sonnet-4-5",
          content: [{ type: "text", text: "answer" }],
          stop_reason: "end_turn",
          usage: {
            input_tokens: integer(1n),
            output_tokens: integer(2n),
            ...usageDetail,
          },
        }),
      );
    }
  }
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

test("decodes mid-conversation system messages and reports degraded loss", () => {
  const decoded = decodeRequest({
    model: "claude-opus-5-5",
    max_tokens: integer(1024n),
    system: [{ type: "text", text: "Base prompt." }],
    messages: [
      { role: "user", content: "Hi" },
      { role: "assistant", content: "Hello" },
      { role: "user", content: "Summarize" },
      { role: "system", content: "Be concise." },
      {
        role: "system",
        content: [
          {
            type: "text",
            text: "Limit 20 words.",
            cache_control: { type: "ephemeral" },
          },
        ],
      },
    ],
  });

  assert.equal(decoded.value.messages.length, 3);
  assert.equal(decoded.value.system?.length, 3);
  assert.equal(decoded.value.system[0]?.text, "Base prompt.");
  assert.equal(decoded.value.system[1]?.text, "Be concise.");
  assert.equal(decoded.value.system[2]?.text, "Limit 20 words.");
  assert.equal(decoded.value.system[2]?.native?.[0]?.kind, "cache_control");

  assert.equal(decoded.losses.length, 2);
  assert.deepEqual(decoded.losses[0], {
    path: "messages[3].role",
    field: "role",
    reason: "degraded",
    detail:
      "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.",
  });
  assert.deepEqual(decoded.losses[1], {
    path: "messages[4].role",
    field: "role",
    reason: "degraded",
    detail:
      "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.",
  });
});

test("records role loss before content-derived losses for mid-conversation system", () => {
  const decoded = decodeRequest({
    model: "claude-opus-5-5",
    max_tokens: integer(1024n),
    messages: [
      { role: "user", content: "Hi" },
      {
        role: "system",
        content: [
          {
            type: "text",
            text: "x",
            cache_control: { type: "ephemeral", ttl: "10m" },
          },
        ],
      },
    ],
  });

  assert.equal(decoded.losses.length, 2);
  assert.equal(decoded.losses[0]?.path, "messages[1].role");
  assert.equal(decoded.losses[0]?.reason, "degraded");
  assert.equal(
    decoded.losses[1]?.path,
    "messages[1].content[0].cache_control.ttl",
  );
  assert.equal(decoded.losses[1]?.reason, "unmapped-value");
});

test("rejects invalid mid-conversation system placement and content", () => {
  // system at index 0
  assert.throws(
    () =>
      decodeRequest({
        model: "claude-opus-5-5",
        max_tokens: integer(1024n),
        messages: [
          { role: "system", content: "Initial." },
          { role: "user", content: "Hi" },
        ],
      }),
    (error: unknown) =>
      error instanceof OxaError &&
      error.message.includes(
        "messages[0]: system message cannot appear at messages[0]",
      ),
  );

  // system followed by user
  assert.throws(
    () =>
      decodeRequest({
        model: "claude-opus-5-5",
        max_tokens: integer(1024n),
        messages: [
          { role: "user", content: "Hi" },
          { role: "system", content: "Mid" },
          { role: "user", content: "Another" },
        ],
      }),
    (error: unknown) =>
      error instanceof OxaError &&
      error.message.includes(
        "system message must be followed by an assistant turn or be the last message",
      ),
  );

  // system preceded by assistant
  assert.throws(
    () =>
      decodeRequest({
        model: "claude-opus-5-5",
        max_tokens: integer(1024n),
        messages: [
          { role: "user", content: "Hi" },
          { role: "assistant", content: "Answer" },
          { role: "system", content: "Mid" },
        ],
      }),
    (error: unknown) =>
      error instanceof OxaError &&
      error.message.includes("system message must follow a user turn"),
  );

  // non-text content block
  assert.throws(
    () =>
      decodeRequest({
        model: "claude-opus-5-5",
        max_tokens: integer(1024n),
        messages: [
          { role: "user", content: "Hi" },
          {
            role: "system",
            content: [
              {
                type: "image",
                source: {
                  type: "base64",
                  media_type: "image/png",
                  data: "abc",
                },
              },
            ],
          },
        ],
      }),
    (error: unknown) =>
      error instanceof OxaError &&
      error.message.includes("system content must be text"),
  );
});
