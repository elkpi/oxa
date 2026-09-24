import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/openai/chatcompletions/index.js";
import {
  decodeRequest as decodeIrRequest,
  decodeResponse as decodeIrResponse,
  encodeRequest as encodeIrRequest,
  encodeResponse as encodeIrResponse,
} from "../src/ir/index.js";
import {
  integer,
  isJsonArray,
  isJsonNumber,
  type JsonArray,
  type JsonObject,
  type JsonValue,
} from "../src/json/index.js";
import type { Loss } from "../src/loss.js";
import {
  compareJson,
  compareLosses,
  findRepoRoot,
  loadVectors,
} from "../src/vectest/index.js";

test("runs every Chat Completions non-stream vector", () => {
  const root = findRepoRoot(process.cwd());
  assert.ok(root !== undefined);
  const vectors = loadVectors(root).filter(
    ({ name, document }) =>
      name.startsWith("chatcompletions.nonstream.") &&
      document.mode === "nonstream",
  );
  assert.ok(vectors.length > 0);

  for (const vector of vectors) {
    const input = object(vector.document.input, `${vector.name}.input`);
    const expectedLosses = losses(vector.document.expected_losses, vector.name);
    const isRequest = strings(
      vector.document.tags,
      `${vector.name}.tags`,
    ).includes("request");
    const conversion = vector.document.conversion;

    const result =
      conversion === "to-ir"
        ? isRequest
          ? decodeRequest(input)
          : decodeResponse(input)
        : isRequest
          ? encodeRequest(decodeIrRequest(input))
          : encodeResponse(decodeIrResponse(input));
    const actual =
      conversion === "to-ir"
        ? isRequest
          ? encodeIrRequest(
              result.value as Parameters<typeof encodeIrRequest>[0],
            )
          : encodeIrResponse(
              result.value as Parameters<typeof encodeIrResponse>[0],
            )
        : result.value;
    const expected = object(
      conversion === "to-ir"
        ? vector.document.expected_ir
        : vector.document.expected_output,
      `${vector.name}.expected`,
    );

    assert.equal(
      compareJson(expected, actual as JsonObject),
      undefined,
      vector.name,
    );
    assert.equal(
      compareLosses(expectedLosses, result.losses),
      undefined,
      vector.name,
    );
  }
});

test("preserves request reasoning when assistant tool calls have null content", () => {
  const decoded = decodeRequest({
    model: "gpt-4o-mini",
    messages: [
      { role: "user", content: "question" },
      {
        role: "assistant",
        content: null,
        reasoning_content: "Plan.",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: { name: "lookup", arguments: "{}" },
          },
        ],
      },
      { role: "tool", tool_call_id: "call_1", content: "result" },
    ],
  });

  const content = decoded.value.messages[1]?.content;
  assert.deepEqual(content?.[0], { type: "thinking", thinking: "Plan." });
  assert.equal(content?.[1]?.type, "tool_use");
});

test("preserves response reasoning when tool calls have null content", () => {
  const decoded = decodeResponse({
    id: "chatcmpl_tool_reasoning",
    model: "o3-mini",
    choices: [
      {
        index: integer(0n),
        message: {
          role: "assistant",
          content: null,
          reasoning_content: "Plan.",
          tool_calls: [
            {
              id: "call_1",
              type: "function",
              function: { name: "lookup", arguments: "{}" },
            },
          ],
        },
        finish_reason: "tool_calls",
      },
    ],
    usage: {
      prompt_tokens: integer(1n),
      completion_tokens: integer(1n),
      total_tokens: integer(2n),
    },
  });

  assert.deepEqual(decoded.value.content[0], {
    type: "thinking",
    thinking: "Plan.",
  });
  assert.equal(decoded.value.content[1]?.type, "tool_use");
});

test("encodes multiple thinking blocks without dropping earlier reasoning", () => {
  const encoded = encodeResponse({
    id: "chatcmpl_multiple_thinking",
    model: "o3-mini",
    content: [
      { type: "thinking", thinking: "First." },
      { type: "thinking", thinking: "Second." },
      { type: "text", text: "Answer." },
    ],
    stop_reason: "end_turn",
    usage: { input_tokens: 1n, output_tokens: 2n },
  });
  const choices = array(encoded.value.choices, "choices");
  const choice = object(choices[0], "choices[0]");
  const message = object(choice.message, "choices[0].message");

  assert.equal(message.reasoning_content, "First.Second.");
});

test("rejects negative and out-of-range Chat Completions usage details", () => {
  const invalidValues = [integer(-1n), integer(9_223_372_036_854_775_808n)];
  const detailCases = [
    (value: (typeof invalidValues)[number]) => ({
      prompt_tokens_details: { cached_tokens: value },
    }),
    (value: (typeof invalidValues)[number]) => ({
      completion_tokens_details: { reasoning_tokens: value },
    }),
  ];

  for (const value of invalidValues) {
    for (const details of detailCases) {
      assert.throws(() =>
        decodeResponse({
          id: "chatcmpl_invalid_usage_details",
          model: "o3-mini",
          choices: [
            {
              index: integer(0n),
              message: { role: "assistant", content: "answer" },
              finish_reason: "stop",
            },
          ],
          usage: {
            prompt_tokens: integer(1n),
            completion_tokens: integer(2n),
            total_tokens: integer(3n),
            ...details(value),
          },
        }),
      );
    }
  }
});

function losses(value: JsonValue | undefined, name: string): readonly Loss[] {
  return array(value, `${name}.expected_losses`).map((entry, index) => {
    const loss = object(entry, `${name}.expected_losses[${index}]`);
    return {
      path: string(loss.path, "loss.path"),
      field: string(loss.field, "loss.field"),
      reason: string(loss.reason, "loss.reason") as Loss["reason"],
      ...(loss.detail === undefined
        ? {}
        : { detail: string(loss.detail, "loss.detail") }),
    };
  });
}

function strings(
  value: JsonValue | undefined,
  name: string,
): readonly string[] {
  return array(value, name).map((entry) => string(entry, name));
}

function array(value: JsonValue | undefined, name: string): JsonArray {
  if (!isJsonArray(value)) throw new Error(`${name}: expected array`);
  return value;
}

function object(value: JsonValue | undefined, name: string): JsonObject {
  if (
    value === undefined ||
    value === null ||
    typeof value !== "object" ||
    isJsonArray(value) ||
    isJsonNumber(value)
  )
    throw new Error(`${name}: expected object`);
  return value;
}

function string(value: JsonValue | undefined, name: string): string {
  if (typeof value !== "string") throw new Error(`${name}: expected string`);
  return value;
}
