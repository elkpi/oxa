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
  type ApplyPatchOperation,
  type Block,
  type Message,
  type Params,
  type Request,
  type Response,
  type ResponseFormat,
  type Tool,
  type ToolChoice,
  type ToolResultBlock,
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
          tools: request.tools.map((tool) =>
            tool.type === "apply_patch"
              ? { type: "apply_patch" }
              : tool.type === "custom"
                ? {
                    type: "custom",
                    name: tool.name,
                    ...(tool.description === undefined
                      ? {}
                      : { description: tool.description }),
                    ...(tool.format === undefined
                      ? {}
                      : { format: encodeCustomToolFormat(tool.format) }),
                  }
                : {
                    ...(tool.type === undefined ? {} : { type: "function" }),
                    name: tool.name,
                    ...(tool.description === undefined
                      ? {}
                      : { description: tool.description }),
                    input_schema: tool.input_schema,
                  },
          ),
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
  const params =
    root.params === undefined ? undefined : decodeParams(root.params);
  const request: Request = {
    model: nonEmptyString(root.model, "request.model"),
    ...(root.system === undefined
      ? {}
      : {
          system: array(root.system, "request.system").map((entry) => {
            const block = decodeBlock(entry);
            if (block.type !== "text")
              fail("system blocks must be text blocks");
            return block;
          }),
        }),
    messages: array(root.messages, "request.messages").map((value) => {
      const message = object(value, "request.message");
      const role = string(message.role, "request.message.role");
      if (role !== "user" && role !== "assistant")
        fail("message.role must be user or assistant");
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
            const type =
              tool.type === undefined
                ? "function"
                : string(tool.type, "request.tool.type");
            if (type === "apply_patch") {
              if (
                tool.name !== undefined ||
                tool.description !== undefined ||
                tool.input_schema !== undefined ||
                tool.format !== undefined
              ) {
                fail("apply_patch declaration must only contain type");
              }
              return { type: "apply_patch" as const };
            }
            const name = nonEmptyString(tool.name, "request.tool.name");
            const description =
              tool.description === undefined
                ? undefined
                : string(tool.description, "request.tool.description");
            if (type === "custom") {
              if (tool.input_schema !== undefined)
                fail("custom tool must not contain input_schema");
              return {
                type,
                name,
                ...(description === undefined ? {} : { description }),
                ...(tool.format === undefined
                  ? {}
                  : { format: decodeCustomToolFormat(tool.format) }),
              };
            }
            if (type !== "function" || tool.format !== undefined)
              fail("invalid function tool shape");
            return {
              ...(tool.type === undefined ? {} : { type: "function" as const }),
              name,
              ...(description === undefined ? {} : { description }),
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
    ...(params === undefined ? {} : { params }),
    ...(root.metadata === undefined
      ? {}
      : {
          metadata: Object.fromEntries(
            Object.entries(object(root.metadata, "request.metadata")).map(
              ([key, value]) => [key, string(value, `request.metadata.${key}`)],
            ),
          ),
        }),
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
    for (const block of message.content) {
      if (
        (block.type === "tool_use" ||
          block.type === "custom_tool_use" ||
          block.type === "apply_patch_tool_use") &&
        block.id === ""
      )
        invalid("tool call id and name must not be empty");
      if (
        (block.type === "tool_use" || block.type === "custom_tool_use") &&
        block.name === ""
      )
        invalid("tool call id and name must not be empty");
      if (
        (block.type === "tool_result" ||
          block.type === "custom_tool_result" ||
          block.type === "apply_patch_tool_result") &&
        block.tool_use_id === ""
      )
        invalid("tool result tool_use_id must not be empty");
    }
    validateToolTurn(request.messages, index);
  }
}

function validateToolTurn(messages: Request["messages"], index: number): void {
  const message = messages[index]!;
  const toolUses =
    message.role === "assistant"
      ? message.content.filter(
          (block) =>
            block.type === "tool_use" ||
            block.type === "custom_tool_use" ||
            block.type === "apply_patch_tool_use",
        )
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
      toolUses.some((toolUse, toolIndex) => {
        const res = followingResults[toolIndex];
        if (res === undefined || res.tool_use_id !== toolUse.id) return true;
        if (toolUse.type === "tool_use") return res.type !== "tool_result";
        if (toolUse.type === "custom_tool_use")
          return res.type !== "custom_tool_result";
        if (toolUse.type === "apply_patch_tool_use")
          return res.type !== "apply_patch_tool_result";
        return true;
      })
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
      !preceding.content.some(
        (block) =>
          block.type === "tool_use" ||
          block.type === "custom_tool_use" ||
          block.type === "apply_patch_tool_use",
      )
    )
      invalid("INV-3: orphan or split tool results");
  }
}

function topLevelToolResults(
  content: readonly Block[],
): readonly (
  | Extract<Block, { readonly type: "tool_result" }>
  | Extract<Block, { readonly type: "custom_tool_result" }>
  | Extract<Block, { readonly type: "apply_patch_tool_result" }>
)[] {
  const results: (
    | Extract<Block, { readonly type: "tool_result" }>
    | Extract<Block, { readonly type: "custom_tool_result" }>
    | Extract<Block, { readonly type: "apply_patch_tool_result" }>
  )[] = [];
  collectToolResults(content, 0, results);
  return results;
}

function collectToolResults(
  content: readonly Block[],
  depth: number,
  results: (
    | Extract<Block, { readonly type: "tool_result" }>
    | Extract<Block, { readonly type: "custom_tool_result" }>
    | Extract<Block, { readonly type: "apply_patch_tool_result" }>
  )[],
): void {
  for (const block of content) {
    if (
      block.type !== "tool_result" &&
      block.type !== "custom_tool_result" &&
      block.type !== "apply_patch_tool_result"
    )
      continue;
    if (depth > 0)
      invalid("INV-3: nested tool results cannot answer a tool use");
    results.push(block);
    if (block.type === "tool_result")
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
    id: nonEmptyString(root.id, "response.id"),
    model: nonEmptyString(root.model, "response.model"),
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
    case "custom_tool_use":
      return {
        type: block.type,
        id: block.id,
        name: block.name,
        input: block.input,
      };
    case "custom_tool_result":
      return {
        type: block.type,
        tool_use_id: block.tool_use_id,
        output:
          typeof block.output === "string"
            ? block.output
            : block.output.map(encodeCustomOutput),
      };
    case "apply_patch_tool_use":
      return {
        type: block.type,
        id: block.id,
        status: block.status,
        operation: encodeApplyPatchOperation(block.operation),
      };
    case "apply_patch_tool_result":
      return {
        type: block.type,
        tool_use_id: block.tool_use_id,
        status: block.status,
        ...(block.output === undefined ? {} : { output: block.output }),
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

function encodeApplyPatchOperation(op: ApplyPatchOperation): JsonObject {
  switch (op.type) {
    case "create_file":
    case "update_file":
      return { type: op.type, path: op.path, diff: op.diff };
    case "delete_file":
      return { type: op.type, path: op.path };
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
        id: nonEmptyString(block.id, "block.id"),
        name: nonEmptyString(block.name, "block.name"),
        input: string(block.input, "block.input") as JsonText,
      };
    case "custom_tool_use":
      return {
        type: block.type,
        id: nonEmptyString(block.id, "block.id"),
        name: nonEmptyString(block.name, "block.name"),
        input: string(block.input, "block.input"),
      };
    case "custom_tool_result": {
      const output = block.output;
      return {
        type: block.type,
        tool_use_id: nonEmptyString(block.tool_use_id, "block.tool_use_id"),
        output:
          typeof output === "string"
            ? output
            : array(output, "block.output").map(decodeCustomOutput),
      };
    }
    case "apply_patch_tool_use":
      return {
        type: block.type,
        id: nonEmptyString(block.id, "block.id"),
        status: decodeApplyPatchCallStatus(block.status, "block.status"),
        operation: decodeApplyPatchOperation(
          block.operation,
          "block.operation",
        ),
      };
    case "apply_patch_tool_result": {
      if (block.output !== undefined && typeof block.output !== "string") {
        fail("apply_patch_tool_result output must be a string if defined");
      }
      return {
        type: block.type,
        tool_use_id: nonEmptyString(block.tool_use_id, "block.tool_use_id"),
        status: decodeApplyPatchResultStatus(block.status, "block.status"),
        ...(block.output === undefined ? {} : { output: block.output }),
      };
    }
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

function decodeApplyPatchOperation(
  value: JsonValue | undefined,
  path: string,
): ApplyPatchOperation {
  const op = object(value, path);
  const type = string(op.type, `${path}.type`);
  const filePath = string(op.path, `${path}.path`);
  switch (type) {
    case "create_file":
    case "update_file":
      return {
        type,
        path: filePath,
        diff: string(op.diff, `${path}.diff`),
      };
    case "delete_file":
      if (op.diff !== undefined) {
        fail(`${path}: delete_file must not carry diff`);
      }
      return {
        type,
        path: filePath,
      };
    default:
      fail(`unsupported apply_patch operation type: ${type}`);
  }
}

function decodeApplyPatchCallStatus(
  value: JsonValue | undefined,
  path: string,
): "in_progress" | "completed" {
  const s = string(value, path);
  if (s !== "in_progress" && s !== "completed") {
    fail(`unknown apply_patch call status: ${s}`);
  }
  return s;
}

function decodeApplyPatchResultStatus(
  value: JsonValue | undefined,
  path: string,
): "completed" | "failed" {
  const s = string(value, path);
  if (s !== "completed" && s !== "failed") {
    fail(`unknown apply_patch result status: ${s}`);
  }
  return s;
}

function encodeCustomOutput(
  block: import("./types.js").TextBlock | import("./types.js").ImageBlock,
): JsonObject {
  return block.type === "text"
    ? { type: "text", text: block.text }
    : {
        type: "image",
        ...(block.media_type === undefined
          ? {}
          : { media_type: block.media_type }),
        ...(block.data === undefined ? {} : { data: block.data }),
        ...(block.url === undefined ? {} : { url: block.url }),
      };
}
function decodeCustomOutput(
  b: JsonValue,
): import("./types.js").TextBlock | import("./types.js").ImageBlock {
  const block = object(b, "block.output[]");
  if (block.type === "text")
    return { type: "text", text: string(block.text, "block.output[].text") };
  if (block.type === "image")
    return {
      type: "image",
      ...(block.media_type === undefined
        ? {}
        : {
            media_type: string(block.media_type, "block.output[].media_type"),
          }),
      ...(block.data === undefined
        ? {}
        : { data: string(block.data, "block.output[].data") }),
      ...(block.url === undefined
        ? {}
        : { url: string(block.url, "block.output[].url") }),
    };
  fail("custom tool output only supports text and image blocks");
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
    ...(params.response_format === undefined
      ? {}
      : { response_format: encodeResponseFormat(params.response_format) }),
  };
}

function encodeCustomToolFormat(
  format: import("./types.js").CustomToolFormat,
): JsonObject {
  return format.type === "text"
    ? { type: "text" }
    : { type: "grammar", syntax: format.syntax, definition: format.definition };
}
function decodeCustomToolFormat(
  value: JsonValue,
): import("./types.js").CustomToolFormat {
  const f = object(value, "tool.format");
  if (f.type === "text") return { type: "text" };
  if (f.type === "grammar") {
    const syntax = string(f.syntax, "tool.format.syntax");
    if (syntax !== "lark" && syntax !== "regex")
      fail("tool.format.syntax is unsupported");
    return {
      type: "grammar",
      syntax,
      definition: string(f.definition, "tool.format.definition"),
    };
  }
  fail("tool.format.type is unsupported");
}
function encodeResponseFormat(format: ResponseFormat): JsonObject {
  switch (format.type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema":
      return {
        type: "json_schema",
        name: format.name,
        ...(format.description === undefined
          ? {}
          : { description: format.description }),
        schema: { ...format.schema },
        ...(format.strict === undefined ? {} : { strict: format.strict }),
      };
  }
}

function decodeResponseFormat(value: JsonValue): ResponseFormat {
  const rf = object(value, "params.response_format");
  const type = string(rf.type, "params.response_format.type");
  switch (type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema":
      return {
        type: "json_schema",
        name: nonEmptyString(rf.name, "params.response_format.name"),
        ...(rf.description === undefined
          ? {}
          : {
              description: string(
                rf.description,
                "params.response_format.description",
              ),
            }),
        schema: object(rf.schema, "params.response_format.schema"),
        ...(rf.strict === undefined
          ? {}
          : { strict: boolean(rf.strict, "params.response_format.strict") }),
      };
    default:
      fail(`unsupported response_format type: ${type}`);
  }
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
    ...(params.response_format === undefined
      ? {}
      : {
          response_format: decodeResponseFormat(params.response_format),
        }),
  };
}

function paramsSet(params: Params): boolean {
  return (
    params.temperature !== undefined ||
    params.top_p !== undefined ||
    params.max_tokens !== undefined ||
    (params.stop_sequences !== undefined && params.stop_sequences.length > 0) ||
    params.reasoning_effort !== undefined ||
    params.response_format !== undefined
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
      : {
          input_tokens_details: encodeInputTokensDetails(
            usage.input_tokens_details,
          ),
        }),
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
  if (mode === "tool") {
    const toolType =
      choice.tool_type === undefined
        ? undefined
        : string(choice.tool_type, "request.tool_choice.tool_type");
    if (toolType === "apply_patch") {
      if (choice.name !== undefined)
        fail("apply_patch tool choice must not have name");
      return {
        mode: "tool",
        tool_type: "apply_patch",
      };
    }
    if (
      toolType !== undefined &&
      toolType !== "function" &&
      toolType !== "custom"
    )
      fail("request.tool_choice.tool_type is unsupported");
    return {
      mode,
      name: nonEmptyString(choice.name, "request.tool_choice.name"),
      ...(toolType === undefined ? {} : { tool_type: toolType }),
    };
  }
  if (mode === "auto" || mode === "any" || mode === "none") {
    if (choice.tool_type !== undefined)
      fail("tool_type is only valid for mode:tool");
    return { mode };
  }
  fail("request.tool_choice.mode is unsupported");
}

function documentRoot(document: JsonValue): JsonObject {
  const root = object(document, "IR document");
  if (!supportedSpecVersion(root.specVersion)) fail("unsupported specVersion");
  return root;
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

function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
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

function reasoningEffort(
  value: JsonValue,
  name: string,
): import("./types.js").ReasoningEffort {
  const str = string(value, name);
  if (str !== "minimal" && str !== "low" && str !== "medium" && str !== "high")
    fail(`${name} must be minimal, low, medium, or high`);
  return str;
}

function supportedSpecVersion(value: unknown): value is "0.1.0" | "0.2.0" {
  return value === "0.1.0" || value === "0.2.0";
}

function fail(message: string): never {
  throw new OxaError("type-violation", message);
}

function invalid(message: string): never {
  throw new OxaError("invalid-input", message);
}
