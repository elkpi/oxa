import assert from "node:assert/strict";

import {
  decodeRequest as decodeIrRequest,
  decodeResponse as decodeIrResponse,
  encodeRequest as encodeIrRequest,
  encodeResponse as encodeIrResponse,
  type Request,
  type Response,
} from "../src/ir/index.js";
import {
  isJsonArray,
  isJsonNumber,
  type JsonArray,
  type JsonObject,
  type JsonValue,
} from "../src/json/index.js";
import type { ConversionResult, Loss } from "../src/loss.js";
import {
  compareJson,
  compareLosses,
  findRepoRoot,
  loadVectors,
} from "../src/vectest/index.js";

export interface NonstreamVectorAdapter {
  decodeRequest(input: JsonObject): ConversionResult<Request>;
  decodeResponse(input: JsonObject): ConversionResult<Response>;
  encodeRequest(input: Request): ConversionResult<JsonObject>;
  encodeResponse(input: Response): ConversionResult<JsonObject>;
}

export function runNonstreamVectors(
  face: "chatcompletions" | "responses" | "anthropic",
  adapter: NonstreamVectorAdapter,
): number {
  const root = findRepoRoot(process.cwd());
  assert.ok(root !== undefined);
  const vectors = loadVectors(root).filter(
    ({ name, document }) =>
      name.startsWith(`${face}.nonstream.`) && document.mode === "nonstream",
  );
  assert.ok(vectors.length > 0);

  for (const vector of vectors) {
    const input = object(vector.document.input, `${vector.name}.input`);
    const expectedLosses = losses(
      vector.document.expected_losses,
      vector.name,
    );
    const isRequest = !strings(
      vector.document.tags,
      `${vector.name}.tags`,
    ).includes("response");
    const conversion = vector.document.conversion;
    let actual: JsonObject;
    let actualLosses: readonly Loss[];
    if (conversion === "to-ir") {
      if (isRequest) {
        const result = adapter.decodeRequest(input);
        actual = encodeIrRequest(result.value);
        actualLosses = result.losses;
      } else {
        const result = adapter.decodeResponse(input);
        actual = encodeIrResponse(result.value);
        actualLosses = result.losses;
      }
    } else if (isRequest) {
      const result = adapter.encodeRequest(decodeIrRequest(input));
      actual = result.value;
      actualLosses = result.losses;
    } else {
      const result = adapter.encodeResponse(decodeIrResponse(input));
      actual = result.value;
      actualLosses = result.losses;
    }
    const expected = object(
      conversion === "to-ir"
        ? vector.document.expected_ir
        : vector.document.expected_output,
      `${vector.name}.expected`,
    );
    assert.equal(compareJson(expected, actual), undefined, vector.name);
    assert.equal(
      compareLosses(expectedLosses, actualLosses),
      undefined,
      vector.name,
    );
  }
  return vectors.length;
}

function losses(value: JsonValue | undefined, name: string): readonly Loss[] {
  return array(value, `${name}.expected_losses`).map((entry, index) => {
    const item = object(entry, `${name}.expected_losses[${index}]`);
    return {
      path: string(item.path, "loss.path"),
      field: string(item.field, "loss.field"),
      reason: string(item.reason, "loss.reason") as Loss["reason"],
      ...(item.detail === undefined
        ? {}
        : { detail: string(item.detail, "loss.detail") }),
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
