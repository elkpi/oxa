import { OxaError } from "../error.js";
import {
  isJsonArray,
  isJsonNumber,
  type JsonObject,
  type JsonValue,
} from "../json/index.js";

export function validateWebSearchExtra(value: JsonValue | undefined): void {
  if (value === undefined) return;
  if (
    value === null ||
    typeof value !== "object" ||
    isJsonArray(value) ||
    isJsonNumber(value)
  )
    throw new OxaError(
      "invalid-input",
      "web_search_call extra must be an object",
    );
  if (
    ["type", "id", "status", "action"].some((key) =>
      Object.prototype.hasOwnProperty.call(value, key),
    )
  )
    throw new OxaError(
      "invalid-input",
      "web_search_call extra cannot shadow known item fields",
    );
}

export function inspectWebSearchCall(
  id: unknown,
  status: unknown,
  value: JsonValue | undefined,
  path = "web_search_call",
): { action: JsonObject; unknown: readonly { path: string; field: string }[] } {
  const fail = (message: string): never => {
    throw new OxaError("invalid-input", `${path}: ${message}`);
  };
  if (
    typeof id !== "string" ||
    id === "" ||
    typeof status !== "string" ||
    status === ""
  )
    fail("requires non-empty id and status");
  const asObject = (node: JsonValue | undefined, name: string): JsonObject => {
    if (
      node === null ||
      typeof node !== "object" ||
      isJsonArray(node) ||
      isJsonNumber(node)
    )
      fail(`${name} must be an object`);
    return node as JsonObject;
  };
  const action = asObject(value, "action");
  const kind = action.type;
  const keys =
    kind === "search"
      ? ["query", "queries", "sources"]
      : kind === "open_page"
        ? ["url"]
        : kind === "find_in_page"
          ? ["url", "pattern"]
          : fail("unknown or absent action.type");
  const unknown: { path: string; field: string }[] = [];
  for (const key of Object.keys(action)) {
    if (key !== "type" && !keys.includes(key))
      unknown.push({ path: `${path}.action.${key}`, field: key });
  }
  if (
    kind === "find_in_page" &&
    (action.url === undefined || action.pattern === undefined)
  )
    fail("find_in_page requires url and pattern");
  for (const key of keys) {
    const node = action[key];
    if (
      node === undefined ||
      (kind === "open_page" && key === "url" && node === null)
    )
      continue;
    if (key === "queries") {
      if (!isJsonArray(node) || node.some((item) => typeof item !== "string"))
        fail("action.queries must be an array of strings");
    } else if (key === "sources") {
      if (!isJsonArray(node)) fail("action.sources must be an array");
      for (const [index, item] of (node as readonly JsonValue[]).entries()) {
        const source = asObject(item, `action.sources[${index}]`);
        if (source.type !== "url" || typeof source.url !== "string")
          fail(`action.sources[${index}] requires type url and string url`);
        for (const field of Object.keys(source)) {
          if (field !== "type" && field !== "url")
            unknown.push({
              path: `${path}.action.sources[${index}].${field}`,
              field,
            });
        }
      }
    } else if (typeof node !== "string") fail(`action.${key} must be a string`);
  }
  return { action, unknown };
}
