import { OxaError } from "../error.js";
import {
  isJsonNumber,
  type JsonInput,
  type JsonNumber,
  type JsonObject,
  type JsonText,
  type JsonValue,
} from "./types.js";

const sourceTexts = new WeakMap<object, JsonText>();

/** Returns the exact source token registered for a lossless JSON object. */
export function sourceTextOf(value: JsonValue): JsonText | undefined {
  return typeof value === "object" && value !== null
    ? sourceTexts.get(value)
    : undefined;
}

/** Registers exact source text for an object without changing its identity. */
export function withSourceText(
  value: JsonObject,
  source: JsonText,
): JsonObject {
  sourceTexts.set(value, source);
  return value;
}

/** Constructs an integer JSON token without number precision loss. */
export function integer(value: bigint): JsonNumber {
  return { kind: "number", token: value.toString(), isInteger: true };
}

/** Marks caller-owned JSON source text as opaque converter input. */
export function jsonText(value: string): JsonText {
  return value as JsonText;
}

/** Explicitly canonicalizes ordinary JavaScript values into lossless JSON. */
export function fromValue(value: JsonInput): JsonValue {
  if (value === null || typeof value === "boolean" || typeof value === "string")
    return value;
  if (typeof value === "bigint") return integer(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value))
      throw new OxaError("invalid-json", "JSON numbers must be finite");
    const token = value.toString();
    return { kind: "number", token, isInteger: Number.isInteger(value) };
  }
  if (isJsonNumber(value)) return value;
  if (Array.isArray(value)) return value.map(fromValue);
  const object: Record<string, JsonValue> = {};
  for (const [key, child] of Object.entries(value))
    object[key] = fromValue(child);
  return object;
}
