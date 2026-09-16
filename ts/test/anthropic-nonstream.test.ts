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
