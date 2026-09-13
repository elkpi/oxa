import { OxaError } from "../error.js";
import {
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  type JsonObject,
  type JsonText,
  type JsonValue,
} from "../json/index.js";
import {
  specVersion,
  type Block,
  type Params,
  type Request,
  type Response,
  type ToolChoice,
  type Usage,
} from "./types.js";

export function encodeRequest(request: Request): JsonObject {
  const params = request.params;
  return {
    specVersion,
    model: request.model,
    ...(request.system === undefined || request.system.length === 0
      ? {}
      : { system: request.system.map(encodeBlock) }),
    messages: request.messages.map((message) => ({
      role: message.role,
      content: message.content.map(encodeBlock),
    })),
    ...(request.tools === undefined || request.tools.length === 0
      ? {}
      : {
          tools: request.tools.map((tool) => ({
            name: tool.name,
            ...(tool.description === undefined
              ? {}
              : { description: tool.description }),
            input_schema: tool.input_schema,
          })),
        }),
    ...(request.tool_choice === undefined
      ? {}
      : { tool_choice: { ...request.tool_choice } }),
    ...(params === undefined || !paramsSet(params)
      ? {}
      : { params: encodeParams(params) }),
    ...(request.metadata === undefined ||
    Object.keys(request.metadata).length === 0
      ? {}
      : { metadata: { ...request.metadata } }),
  };
}

export function decodeRequest(document: JsonValue): Request {
  const root = documentRoot(document);
  return {
    model: string(root.model, "request.model"),
    ...(root.system === undefined
      ? {}
      : {
          system: array(root.system, "request.system").map((value) => {
            const block = decodeBlock(value);
            if (block.type !== "text")
              fail("request.system must contain only text blocks");
            return block;
          }),
        }),
    messages: array(root.messages, "request.messages").map((value) => {
      const message = object(value, "request.message");
      const role = string(message.role, "request.message.role");
      if (role !== "user" && role !== "assistant")
        fail("request.message.role is unsupported");
      return {
        role,
        content: array(
          message.content,
          "request.message.content",
        ).map(decodeBlock),
      };
    }),
    ...(root.tools === undefined
      ? {}
      : {
          tools: array(root.tools, "request.tools").map((value) => {
            const tool = object(value, "request.tool");
            return {
              name: string(tool.name, "request.tool.name"),
              ...(tool.description === undefined
                ? {}
                : {
                    description: string(
                      tool.description,
                      "request.tool.description",
                    ),
                  }),
              input_schema: object(
                tool.input_schema,
                "request.tool.input_schema",
              ),
            };
          }),
        }),
    ...(root.tool_choice === undefined
      ? {}
      : { tool_choice: decodeToolChoice(root.tool_choice) }),
    ...(root.params === undefined
      ? {}
      : { params: decodeParams(root.params) }),
    ...(root.metadata === undefined
      ? {}
      : { metadata: stringRecord(root.metadata, "request.metadata") }),
  };
}

export function encodeResponse(response: Response): JsonObject {
  return {
    specVersion,
    id: response.id,
    model: response.model,
    content: response.content.map(encodeBlock),
    stop_reason: response.stop_reason,
    ...(response.stop_sequence === undefined
      ? {}
      : { stop_sequence: response.stop_sequence }),
    usage: encodeUsage(response.usage),
  };
}

export function decodeResponse(document: JsonValue): Response {
  const root = documentRoot(document);
  return {
    id: string(root.id, "response.id"),
    model: string(root.model, "response.model"),
    content: array(root.content, "response.content").map(decodeBlock),
    stop_reason: string(
      root.stop_reason,
      "response.stop_reason",
    ) as Response["stop_reason"],
    ...(root.stop_sequence === undefined
      ? {}
      : {
          stop_sequence: string(
            root.stop_sequence,
            "response.stop_sequence",
          ),
        }),
    usage: decodeUsage(root.usage),
  };
}

function encodeBlock(block: Block): JsonObject {
  switch (block.type) {
    case "text":
      return { type: "text", text: block.text };
    case "image":
      return {
        type: "image",
        ...(block.media_type === undefined
          ? {}
          : { media_type: block.media_type }),
        ...(block.data === undefined ? {} : { data: block.data }),
        ...(block.url === undefined ? {} : { url: block.url }),
      };
    case "tool_use":
      return {
        type: "tool_use",
        id: block.id,
        name: block.name,
        input: block.input,
      };
    case "tool_result":
      return {
        type: "tool_result",
        tool_use_id: block.tool_use_id,
        content: block.content.map(encodeBlock),
        ...(block.is_error === undefined
          ? {}
          : { is_error: block.is_error }),
      };
  }
}

function decodeBlock(value: JsonValue): Block {
  const block = object(value, "block");
  switch (block.type) {
    case "text":
      return { type: "text", text: string(block.text, "block.text") };
    case "image":
      return {
        type: "image",
        ...(block.media_type === undefined
          ? {}
          : {
              media_type: string(block.media_type, "block.media_type"),
            }),
        ...(block.data === undefined
          ? {}
          : { data: string(block.data, "block.data") }),
        ...(block.url === undefined
          ? {}
          : { url: string(block.url, "block.url") }),
      };
    case "tool_use":
      return {
        type: "tool_use",
        id: string(block.id, "block.id"),
        name: string(block.name, "block.name"),
        input: string(block.input, "block.input") as JsonText,
      };
    case "tool_result":
      return {
        type: "tool_result",
        tool_use_id: string(block.tool_use_id, "block.tool_use_id"),
        content: array(block.content, "block.content").map(decodeBlock),
        ...(block.is_error === undefined
          ? {}
          : { is_error: boolean(block.is_error, "block.is_error") }),
      };
    default:
      fail("unsupported IR block type");
  }
}

function encodeParams(params: Params): JsonObject {
  return {
    ...(params.temperature === undefined
      ? {}
      : { temperature: fromValue(params.temperature) }),
    ...(params.top_p === undefined
      ? {}
      : { top_p: fromValue(params.top_p) }),
    ...(params.max_tokens === undefined
      ? {}
      : { max_tokens: integer(params.max_tokens) }),
    ...(params.stop_sequences === undefined ||
    params.stop_sequences.length === 0
      ? {}
      : { stop_sequences: [...params.stop_sequences] }),
  };
}

function decodeParams(value: JsonValue): Params {
  const params = object(value, "request.params");
  return {
    ...(params.temperature === undefined
      ? {}
      : {
          temperature: finiteNumber(
            params.temperature,
            "params.temperature",
          ),
        }),
    ...(params.top_p === undefined
      ? {}
      : { top_p: finiteNumber(params.top_p, "params.top_p") }),
    ...(params.max_tokens === undefined
      ? {}
      : {
          max_tokens: token(params.max_tokens, "params.max_tokens"),
        }),
    ...(params.stop_sequences === undefined
      ? {}
      : {
          stop_sequences: array(
            params.stop_sequences,
            "params.stop_sequences",
          ).map((entry) => string(entry, "params.stop_sequences[]")),
        }),
  };
}

function paramsSet(params: Params): boolean {
  return (
    params.temperature !== undefined ||
    params.top_p !== undefined ||
    params.max_tokens !== undefined ||
    (params.stop_sequences !== undefined &&
      params.stop_sequences.length > 0)
  );
}

function encodeUsage(usage: Usage): JsonObject {
  return {
    input_tokens: integer(usage.input_tokens),
    output_tokens: integer(usage.output_tokens),
  };
}

function decodeUsage(value: JsonValue | undefined): Usage {
  const usage = object(value, "response.usage");
  return {
    input_tokens: token(usage.input_tokens, "usage.input_tokens"),
    output_tokens: token(usage.output_tokens, "usage.output_tokens"),
  };
}

function decodeToolChoice(value: JsonValue): ToolChoice {
  const choice = object(value, "request.tool_choice");
  const mode = string(choice.mode, "request.tool_choice.mode");
  if (mode === "tool")
    return {
      mode,
      name: string(choice.name, "request.tool_choice.name"),
    };
  if (mode === "auto" || mode === "any" || mode === "none") return { mode };
  fail("request.tool_choice.mode is unsupported");
}

function documentRoot(document: JsonValue): JsonObject {
  const root = object(document, "IR document");
  if (root.specVersion !== specVersion) fail("unsupported specVersion");
  return root;
}

function stringRecord(
  value: JsonValue,
  name: string,
): Readonly<Record<string, string>> {
  const source = object(value, name);
  const result: Record<string, string> = {};
  for (const [key, child] of Object.entries(source))
    result[key] = string(child, `${name}.${key}`);
  return result;
}

function token(value: JsonValue | undefined, name: string): bigint {
  if (!isJsonNumber(value) || !value.isInteger)
    fail(`${name} must be an integer`);
  return BigInt(value.token);
}

function finiteNumber(value: JsonValue | undefined, name: string): number {
  if (!isJsonNumber(value)) fail(`${name} must be a number`);
  const result = Number(value.token);
  if (!Number.isFinite(result)) fail(`${name} must be finite`);
  return result;
}

function array(
  value: JsonValue | undefined,
  name: string,
): readonly JsonValue[] {
  if (!isJsonArray(value)) fail(`${name} must be an array`);
  return value;
}

function object(
  value: JsonValue | undefined,
  name: string,
): JsonObject {
  if (
    value === undefined ||
    value === null ||
    typeof value !== "object" ||
    isJsonArray(value) ||
    isJsonNumber(value)
  )
    fail(`${name} must be an object`);
  return value;
}

function string(value: JsonValue | undefined, name: string): string {
  if (typeof value !== "string") fail(`${name} must be a string`);
  return value;
}

function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
}

function fail(message: string): never {
  throw new OxaError("type-violation", message);
}
