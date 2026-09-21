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
  validateRequest(request);
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
  const request: Request = {
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
        content: array(message.content, "request.message.content").map(
          decodeBlock,
        ),
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
    ...(root.params === undefined ? {} : { params: decodeParams(root.params) }),
    ...(root.metadata === undefined
      ? {}
      : { metadata: stringRecord(root.metadata, "request.metadata") }),
  };
  validateRequest(request);
  return request;
}

/** Enforces request conversation invariants INV-2 through INV-4. */
export function validateRequest(request: Request): void {
  if (request.messages.length === 0)
    invalid("request.messages must not be empty");
  if (request.messages[0]!.role !== "user")
    invalid("INV-2: first message must be user");
  for (let index = 0; index < request.messages.length; index += 1) {
    const message = request.messages[index]!;
    if (message.content.length === 0)
      invalid("messages[" + index + "].content must not be empty");
    validateToolTurn(request.messages, index);
  }
}

function validateToolTurn(messages: Request["messages"], index: number): void {
  const message = messages[index]!;
  const toolUses =
    message.role === "assistant"
      ? message.content.filter((block) => block.type === "tool_use")
      : [];
  const toolResults = topLevelToolResults(message.content);

  if (message.role !== "user" && toolResults.length > 0)
    invalid("INV-3: tool results must appear in a user message");

  if (toolUses.length > 0) {
    const following = messages[index + 1];
    if (following === undefined || following.role !== "user")
      invalid("INV-3: tool uses require one following user message");
    const followingResults = topLevelToolResults(following.content);
    if (
      followingResults.length !== toolUses.length ||
      toolUses.some(
        (toolUse, toolIndex) =>
          followingResults[toolIndex]?.tool_use_id !== toolUse.id,
      )
    )
      invalid(
        "INV-3/INV-4: tool results must match preceding tool uses in order",
      );
  }

  if (toolResults.length > 0) {
    const preceding = messages[index - 1];
    if (
      preceding === undefined ||
      preceding.role !== "assistant" ||
      !preceding.content.some((block) => block.type === "tool_use")
    )
      invalid("INV-3: orphan or split tool results");
  }
}

function topLevelToolResults(
  content: readonly Block[],
): readonly Extract<Block, { readonly type: "tool_result" }>[] {
  const results: Extract<Block, { readonly type: "tool_result" }>[] = [];
  collectToolResults(content, 0, results);
  return results;
}

function collectToolResults(
  content: readonly Block[],
  depth: number,
  results: Extract<Block, { readonly type: "tool_result" }>[],
): void {
  for (const block of content) {
    if (block.type !== "tool_result") continue;
    if (depth > 0)
      invalid("INV-3: nested tool results cannot answer a tool use");
    results.push(block);
    collectToolResults(block.content, depth + 1, results);
  }
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
          stop_sequence: string(root.stop_sequence, "response.stop_sequence"),
        }),
    usage: decodeUsage(root.usage),
  };
}

function encodeBlock(block: Block): JsonObject {
  switch (block.type) {
    case "text":
      return { type: "text", text: block.text };
    case "thinking":
      return {
        type: "thinking",
        thinking: block.thinking,
        ...(block.signature === undefined
          ? {}
          : { signature: block.signature }),
      };
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
        ...(block.is_error === undefined ? {} : { is_error: block.is_error }),
      };
  }
}

function decodeBlock(value: JsonValue): Block {
  const block = object(value, "block");
  switch (block.type) {
    case "text":
      return { type: "text", text: string(block.text, "block.text") };
    case "thinking":
      return {
        type: "thinking",
        thinking: string(block.thinking, "block.thinking"),
        ...(block.signature === undefined
          ? {}
          : { signature: nonEmptyString(block.signature, "block.signature") }),
      };
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
    ...(params.top_p === undefined ? {} : { top_p: fromValue(params.top_p) }),
    ...(params.max_tokens === undefined
      ? {}
      : { max_tokens: integer(params.max_tokens) }),
    ...(params.stop_sequences === undefined ||
    params.stop_sequences.length === 0
      ? {}
      : { stop_sequences: [...params.stop_sequences] }),
    ...(params.reasoning_effort === undefined
      ? {}
      : { reasoning_effort: params.reasoning_effort }),
  };
}

function decodeParams(value: JsonValue): Params {
  const params = object(value, "request.params");
  return {
    ...(params.temperature === undefined
      ? {}
      : {
          temperature: finiteNumber(params.temperature, "params.temperature"),
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
    ...(params.reasoning_effort === undefined
      ? {}
      : {
          reasoning_effort: reasoningEffort(
            params.reasoning_effort,
            "params.reasoning_effort",
          ),
        }),
  };
}

function paramsSet(params: Params): boolean {
  return (
    params.temperature !== undefined ||
    params.top_p !== undefined ||
    params.max_tokens !== undefined ||
    (params.stop_sequences !== undefined && params.stop_sequences.length > 0) ||
    params.reasoning_effort !== undefined
  );
}

function encodeUsage(usage: Usage): JsonObject {
  return {
    input_tokens: integer(usage.input_tokens),
    output_tokens: integer(usage.output_tokens),
    ...(usage.cache_read_input_tokens === undefined
      ? {}
      : { cache_read_input_tokens: integer(usage.cache_read_input_tokens) }),
    ...(usage.cache_creation_input_tokens === undefined
      ? {}
      : {
          cache_creation_input_tokens: integer(
            usage.cache_creation_input_tokens,
          ),
        }),
    ...(usage.input_tokens_details === undefined
      ? {}
      : { input_tokens_details: encodeInputTokensDetails(usage.input_tokens_details) }),
    ...(usage.output_tokens_details === undefined
      ? {}
      : {
          output_tokens_details: encodeOutputTokensDetails(
            usage.output_tokens_details,
          ),
        }),
  };
}

function encodeInputTokensDetails(
  details: NonNullable<Usage["input_tokens_details"]>,
): JsonObject {
  return { cached_tokens: integer(details.cached_tokens) };
}

function encodeOutputTokensDetails(
  details: NonNullable<Usage["output_tokens_details"]>,
): JsonObject {
  return { reasoning_tokens: integer(details.reasoning_tokens) };
}

function decodeUsage(value: JsonValue | undefined): Usage {
  const usage = object(value, "response.usage");
  return {
    input_tokens: token(usage.input_tokens, "usage.input_tokens"),
    output_tokens: token(usage.output_tokens, "usage.output_tokens"),
    ...(usage.cache_read_input_tokens === undefined
      ? {}
      : {
          cache_read_input_tokens: token(
            usage.cache_read_input_tokens,
            "usage.cache_read_input_tokens",
          ),
        }),
    ...(usage.cache_creation_input_tokens === undefined
      ? {}
      : {
          cache_creation_input_tokens: token(
            usage.cache_creation_input_tokens,
            "usage.cache_creation_input_tokens",
          ),
        }),
    ...(usage.input_tokens_details === undefined
      ? {}
      : {
          input_tokens_details: decodeInputTokensDetails(
            usage.input_tokens_details,
          ),
        }),
    ...(usage.output_tokens_details === undefined
      ? {}
      : {
          output_tokens_details: decodeOutputTokensDetails(
            usage.output_tokens_details,
          ),
        }),
  };
}

function decodeInputTokensDetails(
  value: JsonValue,
): NonNullable<Usage["input_tokens_details"]> {
  const details = object(value, "usage.input_tokens_details");
  return {
    cached_tokens: token(
      details.cached_tokens,
      "usage.input_tokens_details.cached_tokens",
    ),
  };
}

function decodeOutputTokensDetails(
  value: JsonValue,
): NonNullable<Usage["output_tokens_details"]> {
  const details = object(value, "usage.output_tokens_details");
  return {
    reasoning_tokens: token(
      details.reasoning_tokens,
      "usage.output_tokens_details.reasoning_tokens",
    ),
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
  if (!supportedSpecVersion(root.specVersion)) fail("unsupported specVersion");
  return root;
}

function supportedSpecVersion(value: unknown): value is "0.1.0" | "0.2.0" {
  return value === "0.1.0" || value === "0.2.0";
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
  let result: bigint;
  try {
    result = BigInt(value.token);
  } catch {
    fail(`${name} must be an integer`);
  }
  if (result < 0n || result > 9_223_372_036_854_775_807n)
    fail(`${name} must be a non-negative signed int64 integer`);
  return result;
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

function object(value: JsonValue | undefined, name: string): JsonObject {
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

function nonEmptyString(value: JsonValue | undefined, name: string): string {
  const result = string(value, name);
  if (result.length === 0) fail(`${name} must not be empty`);
  return result;
}

function reasoningEffort(
  value: JsonValue | undefined,
  name: string,
): NonNullable<Params["reasoning_effort"]> {
  const result = string(value, name);
  if (
    result !== "minimal" &&
    result !== "low" &&
    result !== "medium" &&
    result !== "high"
  )
    fail(`${name} is unsupported`);
  return result;
}

function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
}

function fail(message: string): never {
  throw new OxaError("type-violation", message);
}

function invalid(message: string): never {
  throw new OxaError("invalid-input", message);
}
