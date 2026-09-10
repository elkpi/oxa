import assert from "node:assert/strict";
import test from "node:test";

import { OxaError } from "../src/error.js";
import {
  fromValue,
  integer,
  jsonText,
  parseJson,
  stringifyJson,
  type JsonNumber,
  type JsonObject,
  type JsonValue,
} from "../src/json/index.js";

function expectNumber(value: JsonValue | undefined): JsonNumber {
  assert.ok(value !== null && typeof value === "object" && "kind" in value);
  assert.equal(value.kind, "number");
  return value as JsonNumber;
}

test("preserves non-integer spelling and large integers", () => {
  const value = parseJson('{"a":1.0,"b":9007199254740993}') as JsonObject;

  assert.equal(stringifyJson(value), '{"a":1.0,"b":9007199254740993}');
  assert.equal(expectNumber(value.a).token, "1.0");
  assert.equal(expectNumber(value.b).isInteger, true);
});

test("rejects malformed JSON with a stable error code", () => {
  assert.throws(
    () => parseJson("{"),
    (error: unknown) =>
      error instanceof OxaError && error.code === "invalid-json",
  );
});

test("rejects forged invalid numeric tokens during serialization", () => {
  const forged = { kind: "number", token: "01", isInteger: true } as JsonNumber;

  assert.throws(
    () => stringifyJson(forged),
    (error: unknown) =>
      error instanceof OxaError && error.code === "invalid-json",
  );
});

test("constructs JSON values only through explicit canonicalization helpers", () => {
  assert.equal(stringifyJson(integer(9007199254740993n)), "9007199254740993");
  assert.equal(
    stringifyJson(fromValue({ enabled: true, count: 1 })),
    '{"enabled":true,"count":1}',
  );
  assert.equal(jsonText('{"tool":1.0}'), '{"tool":1.0}');
  assert.throws(
    () => fromValue(Number.NaN),
    (error: unknown) =>
      error instanceof OxaError && error.code === "invalid-json",
  );
});
