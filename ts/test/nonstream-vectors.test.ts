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
    const isRequest = strings(vector.document.tags, `${vector.name}.tags`).includes(
      "request",
    );
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
          ? encodeIrRequest(result.value as Parameters<typeof encodeIrRequest>[0])
          : encodeIrResponse(result.value as Parameters<typeof encodeIrResponse>[0])
        : result.value;
    const expected = object(
      conversion === "to-ir"
        ? vector.document.expected_ir
        : vector.document.expected_output,
      `${vector.name}.expected`,
    );

    assert.equal(compareJson(expected, actual as JsonObject), undefined, vector.name);
    assert.equal(
      compareLosses(expectedLosses, result.losses),
      undefined,
      vector.name,
    );
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

function strings(value: JsonValue | undefined, name: string): readonly string[] {
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
