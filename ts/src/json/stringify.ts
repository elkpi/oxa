import { OxaError } from "../error.js";
import {
  isJsonArray,
  isJsonNumber,
  type JsonNumber,
  type JsonValue,
} from "./types.js";

const numberToken = new RegExp(
  "^-?(?:0|[1-9][0-9]*)(?:\\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$",
);

/** Serializes a lossless JSON value without normalizing number tokens. */
export function stringifyJson(value: JsonValue): string {
  if (value === null) return "null";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "string") return quoteString(value);
  if (isJsonNumber(value)) return stringifyNumber(value);
  if (isJsonArray(value)) return `[${value.map(stringifyJson).join(",")}]`;
  return `{${Object.keys(value)
    .map((key) => `${quoteString(key)}:${stringifyJson(value[key]!)}`)
    .join(",")}}`;
}

function stringifyNumber(value: JsonNumber): string {
  if (
    typeof value.token !== "string" ||
    !numberToken.test(value.token) ||
    value.isInteger !== !/[.eE]/.test(value.token)
  ) {
    throw new OxaError("invalid-json", "invalid JsonNumber token");
  }
  return value.token;
}

function quoteString(value: string): string {
  let output = '"';
  for (const character of value) {
    switch (character) {
      case '"':
        output += '\\"';
        break;
      case "\\":
        output += "\\\\";
        break;
      case "\b":
        output += "\\b";
        break;
      case "\f":
        output += "\\f";
        break;
      case "\n":
        output += "\\n";
        break;
      case "\r":
        output += "\\r";
        break;
      case "\t":
        output += "\\t";
        break;
      default:
        if (character < " ") {
          output += `\\u${character.codePointAt(0)!.toString(16).padStart(4, "0")}`;
        } else {
          output += character;
        }
    }
  }
  return `${output}"`;
}
