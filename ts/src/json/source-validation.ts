import { OxaError } from "../error.js";
import { parseJson } from "./parse.js";
import {
  isJsonArray,
  isJsonNumber,
  type JsonObject,
  type JsonValue,
} from "./types.js";
import { sourceTextOf } from "./value.js";

function sameValue(a: JsonValue, b: JsonValue): boolean {
  if (
    a === null ||
    b === null ||
    typeof a !== "object" ||
    typeof b !== "object"
  )
    return a === b;
  if (isJsonNumber(a) || isJsonNumber(b)) {
    return (
      isJsonNumber(a) &&
      isJsonNumber(b) &&
      a.token === b.token &&
      a.isInteger === b.isInteger
    );
  }
  if (isJsonArray(a) || isJsonArray(b)) {
    return (
      isJsonArray(a) &&
      isJsonArray(b) &&
      a.length === b.length &&
      a.every((value, i) => sameValue(value, b[i]!))
    );
  }
  const aObject = a as JsonObject;
  const bObject = b as JsonObject;
  const keys = Object.keys(aObject);
  return (
    keys.length === Object.keys(bObject).length &&
    keys.every(
      (key) =>
        Object.prototype.hasOwnProperty.call(bObject, key) &&
        sameValue(aObject[key]!, bObject[key]!),
    )
  );
}

/** 原始文本与当前值不一致时拒绝使用过期文本。 */
export function assertSourceMatches(value: JsonObject): void {
  const visit = (node: JsonValue): void => {
    if (node === null || typeof node !== "object" || isJsonNumber(node)) return;
    if (isJsonArray(node)) {
      node.forEach(visit);
      return;
    }
    const source = sourceTextOf(node);
    if (source !== undefined) {
      if (!sameValue(parseJson(source), node)) {
        throw new OxaError(
          "invalid-json",
          "response format schema source no longer matches its value",
        );
      }
      return;
    }
    Object.values(node).forEach(visit);
  };
  visit(value);
}
