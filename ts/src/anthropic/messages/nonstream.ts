import { OxaError } from "../../error.js";
import {
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  jsonText,
  parseJson,
  sourceTextOf,
  stringifyJson,
  withSourceText,
  type JsonObject,
  type JsonText,
  type JsonValue,
} from "../../json/index.js";
import type { ConversionResult, Loss, LossReason } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import { validateRequest } from "../../ir/index.js";
import type {
  Block,
  ImageBlock,
  Request,
  Response,
  ToolChoice,
} from "../../ir/index.js";

export interface NonstreamOptions {
  readonly modelMapper?: ModelMapper;
}

export function decodeRequest(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const maxTokens = whole(wire.max_tokens, "max_tokens");
  if (maxTokens <= 0n) fail("max_tokens must be positive");
  const losses: Loss[] = [];
  if (wire.metadata !== undefined)
    losses.push(loss("metadata", "metadata", "unmapped-field"));
  const system: { readonly type: "text"; readonly text: string }[] = [];
  if (typeof wire.system === "string") {
    system.push({ type: "text", text: wire.system });
  } else if (wire.system !== undefined && wire.system !== null) {
    const blocks = array(wire.system, "system");
    for (let index = 0; index < blocks.length; index += 1) {
      const block = object(blocks[index], `system[${index}]`);
      if (block.type !== "text") fail(`system[${index}].type must be text`);
      system.push({
        type: "text",
        text: string(block.text, `system[${index}].text`),
      });
      if (block.cache_control !== undefined)
        losses.push(
          loss(
            `system[${index}].cache_control`,
            "cache_control",
            "unmapped-field",
          ),
        );
    }
  }
  const messages = array(wire.messages, "messages").map((entry, index) => {
    const message = object(entry, `messages[${index}]`);
    const role = string(message.role, `messages[${index}].role`);
    if (role !== "user" && role !== "assistant")
      fail(`messages[${index}]: unknown role ${role}`);
    const normalizedRole: "user" | "assistant" = role;
    const content = decodeContent(
      message.content,
      `messages[${index}].content`,
      losses,
    );
    if (content.length === 0) content.push({ type: "text", text: "" });
    return { role: normalizedRole, content };
  });
  const tools = optionalArray(wire.tools, "tools").map((entry, index) => {
    const tool = object(entry, `tools[${index}]`);
    return {
      name: string(tool.name, `tools[${index}].name`),
      ...(tool.description === undefined
        ? {}
        : {
            description: string(
              tool.description,
              `tools[${index}].description`,
            ),
          }),
      input_schema: object(tool.input_schema, `tools[${index}].input_schema`),
    };
  });
  const toolChoice = decodeToolChoice(wire.tool_choice, losses);
  const params = {
    max_tokens: maxTokens,
    ...(wire.temperature === undefined
      ? {}
      : { temperature: number(wire.temperature, "temperature") }),
    ...(wire.top_p === undefined ? {} : { top_p: number(wire.top_p, "top_p") }),
    ...(wire.stop_sequences === undefined
      ? {}
      : {
          stop_sequences: strings(wire.stop_sequences, "stop_sequences"),
        }),
  };
  const request: Request = {
    model: mapModel(options.modelMapper, string(wire.model, "model")),
    ...(system.length === 0 ? {} : { system }),
    messages,
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    params,
  };
  validateRequest(request);
  return { value: request, losses };
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const losses: Loss[] = [];
  const content = decodeContent(wire.content, "content", losses);
  const nativeStop = string(wire.stop_reason, "stop_reason");
  let stopReason: Response["stop_reason"];
  if (
    nativeStop === "end_turn" ||
    nativeStop === "max_tokens" ||
    nativeStop === "stop_sequence" ||
    nativeStop === "tool_use" ||
    nativeStop === "refusal"
  )
    stopReason = nativeStop;
  else {
    stopReason = "other";
    losses.push(loss("stop_reason", "stop_reason", "unmapped-value"));
  }
  const usage = object(wire.usage, "usage");
  return {
    value: {
      id: string(wire.id, "id"),
      model: mapModel(options.modelMapper, string(wire.model, "model")),
      content,
      stop_reason: stopReason,
      ...(stopReason === "stop_sequence" && wire.stop_sequence !== undefined
        ? {
            stop_sequence: string(wire.stop_sequence, "stop_sequence"),
          }
        : {}),
      usage: {
        input_tokens: whole(usage.input_tokens, "usage.input_tokens"),
        output_tokens: whole(usage.output_tokens, "usage.output_tokens"),
      },
    },
    losses,
  };
}

export function encodeRequest(
  request: Request,
  options: NonstreamOptions = {},
): ConversionResult<JsonObject> {
  validateRequest(request);
  const losses: Loss[] = [];
  if (
    request.metadata !== undefined &&
    Object.keys(request.metadata).length > 0
  )
    losses.push(loss("metadata", "metadata", "unmapped-field"));
  const maxTokens = request.params?.max_tokens ?? 4096n;
  if (request.params?.max_tokens === undefined)
    losses.push(loss("params.max_tokens", "max_tokens", "degraded"));
  const shorthand =
    (request.system === undefined || request.system.length === 0) &&
    request.messages.length === 1 &&
    request.messages[0]!.content.length === 1 &&
    request.messages[0]!.content[0]!.type === "text";
  const messages = request.messages.map((message, index) => ({
    role: message.role,
    content: shorthand
      ? (message.content[0] as { readonly text: string }).text
      : encodeContent(message.content, `messages[${index}].content`, losses),
  }));
  const tools =
    request.tools?.map((tool) => ({
      name: tool.name,
      ...(tool.description === undefined
        ? {}
        : { description: tool.description }),
      input_schema: tool.input_schema,
    })) ?? [];
  const toolChoice = encodeToolChoice(request.tool_choice);
  const params = request.params;
  return {
    value: {
      model: mapModel(options.modelMapper, request.model),
      max_tokens: integer(maxTokens),
      ...(request.system === undefined || request.system.length === 0
        ? {}
        : {
            system: request.system.map((block) => ({
              type: "text",
              text: block.text,
            })),
          }),
      messages,
      ...(params?.temperature === undefined
        ? {}
        : { temperature: fromValue(params.temperature) }),
      ...(params?.top_p === undefined
        ? {}
        : { top_p: fromValue(params.top_p) }),
      ...(params?.stop_sequences === undefined ||
      params.stop_sequences.length === 0
        ? {}
        : { stop_sequences: [...params.stop_sequences] }),
      ...(tools.length === 0 ? {} : { tools }),
      ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    },
    losses,
  };
}

export function encodeResponse(
  response: Response,
  options: NonstreamOptions = {},
): ConversionResult<JsonObject> {
  if (
    response.stop_reason !== "end_turn" &&
    response.stop_reason !== "max_tokens" &&
    response.stop_reason !== "stop_sequence" &&
    response.stop_reason !== "tool_use" &&
    response.stop_reason !== "refusal"
  )
    fail(`stop reason ${response.stop_reason} is unsupported`);
  const losses: Loss[] = [];
  return {
    value: {
      id: response.id,
      type: "message",
      role: "assistant",
      model: mapModel(options.modelMapper, response.model),
      content: encodeContent(response.content, "content", losses),
      stop_reason: response.stop_reason,
      ...(response.stop_reason === "stop_sequence" &&
      response.stop_sequence !== undefined
        ? { stop_sequence: response.stop_sequence }
        : {}),
      usage: {
        input_tokens: integer(response.usage.input_tokens),
        output_tokens: integer(response.usage.output_tokens),
      },
    },
    losses,
  };
}

function decodeContent(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): Block[] {
  if (typeof value === "string") return [{ type: "text", text: value }];
  const blocks: Block[] = [];
  for (let index = 0; index < array(value, path).length; index += 1) {
    const block = object(array(value, path)[index], `${path}[${index}]`);
    const type = string(block.type, `${path}[${index}].type`);
    if (type === "text") {
      blocks.push({
        type: "text",
        text: string(block.text, `${path}[${index}].text`),
      });
    } else if (type === "image") {
      const source = object(block.source, `${path}[${index}].source`);
      if (source.type === "base64")
        blocks.push({
          type: "image",
          media_type: string(
            source.media_type,
            `${path}[${index}].source.media_type`,
          ),
          data: string(source.data, `${path}[${index}].source.data`),
        });
      else if (source.type === "url")
        blocks.push({
          type: "image",
          url: string(source.url, `${path}[${index}].source.url`),
        });
      else {
        losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
        continue;
      }
    } else if (type === "tool_use") {
      const inputPath = `${path}[${index}].input`;
      const input = object(block.input, inputPath);
      const inputText =
        block.inputText === undefined
          ? (sourceTextOf(input) ?? jsonText(stringifyJson(input)))
          : toolInputText(block.inputText, `${path}[${index}].inputText`);
      blocks.push({
        type: "tool_use",
        id: string(block.id, `${path}[${index}].id`),
        name: string(block.name, `${path}[${index}].name`),
        input: inputText,
      });
    } else if (type === "tool_result") {
      blocks.push({
        type: "tool_result",
        tool_use_id: string(block.tool_use_id, `${path}[${index}].tool_use_id`),
        content: decodeContent(
          block.content,
          `${path}[${index}].content`,
          losses,
        ),
        ...(block.is_error === undefined
          ? {}
          : {
              is_error: boolean(block.is_error, `${path}[${index}].is_error`),
            }),
      });
    } else {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
      continue;
    }
    if (block.cache_control !== undefined)
      losses.push(
        loss(
          `${path}[${index}].cache_control`,
          "cache_control",
          "unmapped-field",
        ),
      );
  }
  return blocks;
}

function encodeContent(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
): JsonValue[] {
  const output: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    if (block.type === "text") output.push({ type: "text", text: block.text });
    else if (block.type === "image") {
      const image = encodeImage(block);
      if (image === undefined)
        losses.push(loss(`${path}[${index}]`, "image", "unsupported-semantic"));
      else output.push(image);
    } else if (block.type === "tool_use") {
      const value = parseJson(block.input);
      const inputPath = `${path}[${index}].input`;
      output.push({
        type: "tool_use",
        id: block.id,
        name: block.name,
        input: withSourceText(object(value, inputPath), block.input),
      });
    } else {
      const content: JsonValue[] = [];
      for (let child = 0; child < block.content.length; child += 1) {
        const nested = block.content[child]!;
        if (nested.type === "text")
          content.push({ type: "text", text: nested.text });
        else if (nested.type === "image") {
          const image = encodeImage(nested);
          if (image === undefined)
            losses.push(
              loss(
                `${path}[${index}].content[${child}]`,
                "image",
                "unsupported-semantic",
              ),
            );
          else content.push(image);
        } else
          losses.push(
            loss(
              `${path}[${index}].content[${child}]`,
              "content",
              "unsupported-semantic",
            ),
          );
      }
      output.push({
        type: "tool_result",
        tool_use_id: block.tool_use_id,
        content,
        ...(block.is_error === undefined ? {} : { is_error: block.is_error }),
      });
    }
  }
  return output;
}

function encodeImage(image: ImageBlock): JsonObject | undefined {
  if (image.data !== undefined && image.url !== undefined) return undefined;
  if (image.data !== undefined && image.media_type !== undefined)
    return {
      type: "image",
      source: {
        type: "base64",
        media_type: image.media_type,
        data: image.data,
      },
    };
  if (image.url !== undefined && image.media_type === undefined)
    return {
      type: "image",
      source: { type: "url", url: image.url },
    };
  return undefined;
}

function decodeToolChoice(
  value: JsonValue | undefined,
  losses: Loss[],
): ToolChoice | undefined {
  if (value === undefined || value === null) return undefined;
  const choice = object(value, "tool_choice");
  if (choice.disable_parallel_tool_use !== undefined)
    losses.push(
      loss(
        "tool_choice.disable_parallel_tool_use",
        "disable_parallel_tool_use",
        "unmapped-field",
      ),
    );
  const type = string(choice.type, "tool_choice.type");
  if (type === "auto" || type === "any" || type === "none")
    return { mode: type };
  if (type === "tool")
    return {
      mode: "tool",
      name: string(choice.name, "tool_choice.name"),
    };
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function encodeToolChoice(
  value: ToolChoice | undefined,
): JsonObject | undefined {
  if (value === undefined) return undefined;
  if (value.mode === "tool") {
    if (value.name === "") fail("tool_choice.name must not be empty");
    return { type: "tool", name: value.name };
  }
  return { type: value.mode };
}

function loss(path: string, field: string, reason: LossReason): Loss {
  return { path, field, reason };
}
function optionalArray(
  value: JsonValue | undefined,
  name: string,
): readonly JsonValue[] {
  return value === undefined || value === null ? [] : array(value, name);
}
function strings(
  value: JsonValue | undefined,
  name: string,
): readonly string[] {
  return array(value, name).map((entry) => string(entry, name));
}
function toolInputText(value: JsonValue, name: string): JsonText {
  const source = string(value, name);
  try {
    object(parseJson(source), name);
  } catch {
    fail(`${name} must encode an object`);
  }
  return jsonText(source);
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
function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
}
function number(value: JsonValue | undefined, name: string): number {
  if (!isJsonNumber(value)) fail(`${name} must be a number`);
  const result = Number(value.token);
  if (!Number.isFinite(result)) fail(`${name} must be finite`);
  return result;
}
function whole(value: JsonValue | undefined, name: string): bigint {
  if (!isJsonNumber(value) || !value.isInteger)
    fail(`${name} must be an integer`);
  return BigInt(value.token);
}
function fail(message: string): never {
  throw new OxaError("type-violation", `anthropic: ${message}`);
}
