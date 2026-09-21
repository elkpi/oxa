import {
  isJsonArray,
  isJsonNumber,
  type JsonObject,
  type JsonValue,
} from "../json/index.js";
import type { Loss } from "../loss.js";

/** Returns a path-specific difference, or undefined when values match. */
export function compareJson(
  expected: JsonValue,
  actual: JsonValue,
): string | undefined {
  return compareValue(expected, actual, "$");
}

/** Compares losses as sets keyed by path, field, and reason. */
export function compareLosses(
  expected: readonly Loss[],
  actual: readonly Loss[],
): string | undefined {
  const expectedKeys = expected.map(lossKey).sort();
  const actualKeys = actual.map(lossKey).sort();
  if (expectedKeys.length !== actualKeys.length)
    return `loss count differs: expected ${expectedKeys.length}, got ${actualKeys.length}`;
  for (let index = 0; index < expectedKeys.length; index += 1) {
    if (expectedKeys[index] !== actualKeys[index])
      return `loss differs: expected ${expectedKeys[index]}, got ${actualKeys[index]}`;
  }
  return undefined;
}

function compareValue(
  expected: JsonValue,
  actual: JsonValue,
  path: string,
): string | undefined {
  if (isJsonNumber(expected) || isJsonNumber(actual)) {
    if (!isJsonNumber(expected) || !isJsonNumber(actual))
      return `${path}: value kind differs`;
    if (expected.isInteger !== actual.isInteger)
      return `${path}: integer fidelity differs`;
    if (expected.isInteger)
      return BigInt(expected.token) === BigInt(actual.token)
        ? undefined
        : `${path}: integer differs`;
    return decimalIdentity(expected.token) === decimalIdentity(actual.token)
      ? undefined
      : `${path}: decimal differs`;
  }
  if (
    expected === null ||
    actual === null ||
    typeof expected !== "object" ||
    typeof actual !== "object"
  )
    return Object.is(expected, actual) ? undefined : `${path}: value differs`;
  if (isJsonArray(expected) || isJsonArray(actual)) {
    if (!isJsonArray(expected) || !isJsonArray(actual))
      return `${path}: value kind differs`;
    if (expected.length !== actual.length)
      return `${path}: array length differs`;
    for (let index = 0; index < expected.length; index += 1) {
      const difference = compareValue(
        expected[index]!,
        actual[index]!,
        `${path}[${index}]`,
      );
      if (difference !== undefined) return difference;
    }
    return undefined;
  }
  return compareObject(expected, actual, path);
}

function compareObject(
  expected: JsonObject,
  actual: JsonObject,
  path: string,
): string | undefined {
  const expectedKeys = Object.keys(expected).sort();
  const actualKeys = Object.keys(actual).sort();
  if (expectedKeys.length !== actualKeys.length)
    return `${path}: object key count differs`;
  for (let index = 0; index < expectedKeys.length; index += 1) {
    const key = expectedKeys[index]!;
    if (key !== actualKeys[index]) return `${path}: object keys differ`;
    const difference =
      key === "specVersion"
        ? compareSpecVersion(expected[key]!, actual[key]!, `${path}.${key}`)
        : compareValue(expected[key]!, actual[key]!, `${path}.${key}`);
    if (difference !== undefined) return difference;
  }
  return undefined;
}

/**
 * Transitional equivalence for the Spec 2.0 rollout: baseline vectors pin the
 * 0.1.0 IR contract while dual-read encoders emit 0.2.0. Mirrors the Go
 * harness; tighten once every language implements 0.2.0.
 */
function compareSpecVersion(
  expected: JsonValue,
  actual: JsonValue,
  path: string,
): string | undefined {
  if (
    (expected === "0.1.0" || expected === "0.2.0") &&
    (actual === "0.1.0" || actual === "0.2.0")
  )
    return undefined;
  return compareValue(expected, actual, path);
}

function decimalIdentity(token: string): string {
  const match = /^(-?)([0-9]+)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$/.exec(
    token,
  );
  if (match === null) throw new Error(`invalid JSON number token ${token}`);
  const sign = match[1] === "-" ? "-" : "+";
  const integer = match[2]!;
  const fraction = match[3] ?? "";
  const exponent = Number.parseInt(match[4] ?? "0", 10);
  let digits = (integer + fraction).replace(/^0+/, "");
  if (digits === "") return "+0e0";
  let scale = fraction.length - exponent;
  while (digits.endsWith("0")) {
    digits = digits.slice(0, -1);
    scale -= 1;
  }
  return `${sign}${digits}e${scale}`;
}

function lossKey(loss: Loss): string {
  return `${loss.path}\u0000${loss.field}\u0000${loss.reason}`;
}
