import { OxaError } from "../../error.js";
import {
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  jsonText,
  type JsonObject,
  type JsonValue,
} from "../../json/index.js";
import type { ConversionResult, Loss, LossReason } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import type {
  Block,
  ImageBlock,
  Message,
  Request,
  Response,
  ToolChoice,
  ToolResultBlock,
} from "../../ir/index.js";

export interface NonstreamOptions {
  readonly modelMapper?: ModelMapper;
}

export function decodeRequest(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const losses: Loss[] = [];
  if (wire.metadata !== undefined)
    losses.push(loss("metadata", "metadata", "unmapped-field"));
  if (wire.text !== undefined) {
    const text = object(wire.text, "text");
    if (text.verbosity !== undefined)
      losses.push(loss("text.verbosity", "verbosity", "unmapped-field"));
    if (text.format !== undefined)
      losses.push(loss("text.format", "format", "unmapped-field"));
  }
  if (wire.reasoning !== undefined)
    losses.push(loss("reasoning", "reasoning", "unmapped-field"));
  if (wire.parallel_tool_calls !== undefined)
    losses.push(
      loss("parallel_tool_calls", "parallel_tool_calls", "unmapped-field"),
    );

  const system: { readonly type: "text"; readonly text: string }[] = [];
  if (wire.instructions !== undefined)
    system.push({
      type: "text",
      text: string(wire.instructions, "instructions"),
    });
  const tools = optionalArray(wire.tools, "tools").flatMap((entry, index) => {
    const tool = object(entry, `tools[${index}]`);
    if (tool.type !== "function") {
      losses.push(loss(`tools[${index}]`, "type", "unsupported-semantic"));
      return [];
    }
    if (tool.strict !== undefined)
      losses.push(
        loss(`tools[${index}].strict`, "strict", "unmapped-field"),
      );
    return [
      {
        name: string(tool.name, `tools[${index}].name`),
        ...(tool.description === undefined
          ? {}
          : {
              description: string(
                tool.description,
                `tools[${index}].description`,
              ),
            }),
        input_schema: object(
          tool.parameters,
          `tools[${index}].parameters`,
        ),
      },
    ];
  });
  const toolChoice = decodeToolChoice(wire.tool_choice, losses);
  const messages: Message[] = [];
  if (typeof wire.input === "string") {
    messages.push({
      role: "user",
      content: [{ type: "text", text: wire.input }],
    });
  } else {
    const items = array(wire.input, "input");
    for (let index = 0; index < items.length; ) {
      const item = object(items[index], `input[${index}]`);
      const type = item.type === undefined ? "message" : string(item.type, `input[${index}].type`);
      if (type === "function_call" || (type === "message" && item.role === "assistant")) {
        const texts: Block[] = [];
        const calls: Block[] = [];
        while (index < items.length) {
          const current = object(items[index], `input[${index}]`);
          const currentType =
            current.type === undefined
              ? "message"
              : string(current.type, `input[${index}].type`);
          if (currentType === "function_call") {
            calls.push({
              type: "tool_use",
              id: string(current.call_id, `input[${index}].call_id`),
              name: string(current.name, `input[${index}].name`),
              input: jsonText(
                string(current.arguments, `input[${index}].arguments`),
              ),
            });
            index += 1;
            continue;
          }
          if (currentType !== "message" || current.role !== "assistant") break;
          texts.push(
            ...decodeContent(
              current.content,
              `input[${index}].content`,
              losses,
            ),
          );
          index += 1;
        }
        const content = [...texts, ...calls];
        if (content.length > 0) messages.push({ role: "assistant", content });
        continue;
      }
      if (type === "function_call_output") {
        const content: Block[] = [];
        while (index < items.length) {
          const current = object(items[index], `input[${index}]`);
          if (current.type !== "function_call_output") break;
          content.push({
            type: "tool_result",
            tool_use_id: string(
              current.call_id,
              `input[${index}].call_id`,
            ),
            content: [
              {
                type: "text",
                text: string(current.output, `input[${index}].output`),
              },
            ],
          });
          index += 1;
        }
        messages.push({ role: "user", content });
        continue;
      }
      if (type === "message") {
        const role = string(item.role, `input[${index}].role`);
        const content = decodeContent(
          item.content,
          `input[${index}].content`,
          losses,
        );
        if (role === "system") {
          for (let child = 0; child < content.length; child += 1) {
            const block = content[child]!;
            if (block.type === "text") system.push(block);
            else
              losses.push(
                loss(
                  `input[${index}].content[${child}]`,
                  "content",
                  "unsupported-semantic",
                ),
              );
          }
        } else if (role === "user") messages.push({ role, content });
        else fail(`input[${index}]: unknown role ${role}`);
        index += 1;
        continue;
      }
      losses.push(
        loss(`input[${index}]`, "type", "unsupported-semantic"),
      );
      index += 1;
    }
  }
  if (messages.length === 0) fail("request carries no conversation input");
  const params = {
    ...(wire.temperature === undefined
      ? {}
      : { temperature: number(wire.temperature, "temperature") }),
    ...(wire.top_p === undefined
      ? {}
      : { top_p: number(wire.top_p, "top_p") }),
    ...(wire.max_output_tokens === undefined
      ? {}
      : {
          max_tokens: whole(
            wire.max_output_tokens,
            "max_output_tokens",
          ),
        }),
  };
  return {
    value: {
      model: mapModel(options.modelMapper, string(wire.model, "model")),
      ...(system.length === 0 ? {} : { system }),
      messages,
      ...(tools.length === 0 ? {} : { tools }),
      ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
      ...(Object.keys(params).length === 0 ? {} : { params }),
    },
    losses,
  };
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const losses: Loss[] = [];
  const texts: Block[] = [];
  const calls: Block[] = [];
  for (let index = 0; index < array(wire.output, "output").length; index += 1) {
    const item = object(array(wire.output, "output")[index], `output[${index}]`);
    if (item.type === "message") {
      const content = array(item.content, `output[${index}].content`);
      for (let child = 0; child < content.length; child += 1) {
        const part = object(
          content[child],
          `output[${index}].content[${child}]`,
        );
        if (part.type !== "output_text") {
          losses.push(
            loss(
              `output[${index}].content[${child}]`,
              "type",
              "unsupported-semantic",
            ),
          );
          continue;
        }
        if (
          part.annotations !== undefined &&
          array(
            part.annotations,
            `output[${index}].content[${child}].annotations`,
          ).length > 0
        )
          losses.push(
            loss(
              `output[${index}].content[${child}].annotations`,
              "annotations",
              "unmapped-field",
            ),
          );
        texts.push({
          type: "text",
          text: string(
            part.text,
            `output[${index}].content[${child}].text`,
          ),
        });
      }
    } else if (item.type === "function_call") {
      calls.push({
        type: "tool_use",
        id: string(item.call_id, `output[${index}].call_id`),
        name: string(item.name, `output[${index}].name`),
        input: jsonText(
          string(item.arguments, `output[${index}].arguments`),
        ),
      });
    } else {
      losses.push(
        loss(`output[${index}]`, "type", "unsupported-semantic"),
      );
    }
  }
  const status = string(wire.status, "status");
  let stopReason: Response["stop_reason"];
  if (wire.error !== undefined && wire.error !== null) {
    stopReason = "other";
    losses.push(loss("error", "error", "unsupported-semantic"));
  } else if (status === "completed") {
    stopReason = calls.length > 0 ? "tool_use" : "end_turn";
  } else if (status === "incomplete") {
    const details =
      wire.incomplete_details === undefined ||
      wire.incomplete_details === null
        ? undefined
        : object(wire.incomplete_details, "incomplete_details");
    if (details?.reason === "max_output_tokens") stopReason = "max_tokens";
    else {
      stopReason = "other";
      losses.push(
        loss(
          "incomplete_details.reason",
          "reason",
          "unmapped-value",
        ),
      );
    }
  } else if (status === "failed") {
    stopReason = "other";
    losses.push(loss("error", "error", "unsupported-semantic"));
  } else fail(`status ${status} has no IR equivalent`);
  const usage =
    wire.usage === undefined || wire.usage === null
      ? { input_tokens: 0n, output_tokens: 0n }
      : (() => {
          const value = object(wire.usage, "usage");
          return {
            input_tokens: whole(value.input_tokens, "usage.input_tokens"),
            output_tokens: whole(value.output_tokens, "usage.output_tokens"),
          };
        })();
  return {
    value: {
      id: string(wire.id, "id"),
      model: mapModel(options.modelMapper, string(wire.model, "model")),
      content: [...texts, ...calls],
      stop_reason: stopReason,
      usage,
    },
    losses,
  };
}

export function encodeRequest(
  request: Request,
  options: NonstreamOptions = {},
): ConversionResult<JsonObject> {
  const losses: Loss[] = [];
  if (request.metadata !== undefined && Object.keys(request.metadata).length > 0)
    losses.push(loss("metadata", "metadata", "unmapped-field"));
  const items: JsonValue[] = [];
  for (let index = 0; index < request.messages.length; index += 1) {
    const message = request.messages[index]!;
    if (message.role === "assistant") {
      let text = "";
      let hasText = false;
      const calls: JsonValue[] = [];
      for (let child = 0; child < message.content.length; child += 1) {
        const block = message.content[child]!;
        if (block.type === "text") {
          text += block.text;
          hasText = true;
        } else if (block.type === "tool_use") {
          calls.push({
            type: "function_call",
            call_id: block.id,
            name: block.name,
            arguments: block.input,
          });
        } else
          losses.push(
            loss(
              `messages[${index}].content[${child}]`,
              "content",
              "unsupported-semantic",
            ),
          );
      }
      if (hasText) items.push({ role: "assistant", content: text });
      items.push(...calls);
      continue;
    }
    encodeUserMessage(items, message, index, losses);
  }
  const shorthand =
    (request.system === undefined || request.system.length === 0) &&
    items.length === 1 &&
    object(items[0], "input[0]").role === "user" &&
    typeof object(items[0], "input[0]").content === "string"
      ? object(items[0], "input[0]").content
      : undefined;
  const tools =
    request.tools?.map((tool) => ({
      type: "function",
      name: tool.name,
      ...(tool.description === undefined
        ? {}
        : { description: tool.description }),
      parameters: tool.input_schema,
    })) ?? [];
  const toolChoice = encodeToolChoice(request.tool_choice, losses);
  const params = request.params;
  if (
    params?.stop_sequences !== undefined &&
    params.stop_sequences.length > 0
  )
    losses.push(
      loss("params.stop_sequences", "stop_sequences", "unmapped-field"),
    );
  return {
    value: {
      model: mapModel(options.modelMapper, request.model),
      input: shorthand ?? items,
      ...(request.system === undefined || request.system.length === 0
        ? {}
        : {
            instructions: request.system
              .map((block) => block.text)
              .join(""),
          }),
      ...(params?.temperature === undefined
        ? {}
        : { temperature: fromValue(params.temperature) }),
      ...(params?.top_p === undefined
        ? {}
        : { top_p: fromValue(params.top_p) }),
      ...(params?.max_tokens === undefined
        ? {}
        : { max_output_tokens: integer(params.max_tokens) }),
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
  const losses: Loss[] = [];
  let text = "";
  let hasText = false;
  const calls: JsonValue[] = [];
  for (let index = 0; index < response.content.length; index += 1) {
    const block = response.content[index]!;
    if (block.type === "text") {
      text += block.text;
      hasText = true;
    } else if (block.type === "tool_use") {
      calls.push({
        type: "function_call",
        id: "fc_abc123",
        status: "completed",
        call_id: block.id,
        name: block.name,
        arguments: block.input,
      });
    } else
      losses.push(
        loss(`content[${index}]`, "content", "unsupported-semantic"),
      );
  }
  const output: JsonValue[] = [];
  if (hasText || response.content.length === 0)
    output.push({
      type: "message",
      id: "msg_abc123",
      status: "completed",
      role: "assistant",
      content: [{ type: "output_text", text, annotations: [] }],
    });
  output.push(...calls);
  let status = "completed";
  let incompleteDetails: JsonObject | undefined;
  let error: JsonObject | undefined;
  switch (response.stop_reason) {
    case "end_turn":
    case "tool_use":
      break;
    case "max_tokens":
      status = "incomplete";
      incompleteDetails = { reason: "max_output_tokens" };
      break;
    case "stop_sequence":
      losses.push(loss("", "stop_sequence", "unmapped-value"));
      break;
    case "refusal":
      status = "failed";
      error = { code: "refusal", message: "" };
      break;
    default:
      fail(`stop reason ${response.stop_reason} is unsupported`);
  }
  return {
    value: {
      id: response.id,
      object: "response",
      status,
      ...(error === undefined ? {} : { error }),
      ...(incompleteDetails === undefined
        ? {}
        : { incomplete_details: incompleteDetails }),
      model: mapModel(options.modelMapper, response.model),
      output,
      usage: {
        input_tokens: integer(response.usage.input_tokens),
        output_tokens: integer(response.usage.output_tokens),
        total_tokens: integer(
          response.usage.input_tokens + response.usage.output_tokens,
        ),
      },
    },
    losses,
  };
}

function encodeUserMessage(
  items: JsonValue[],
  message: Message,
  messageIndex: number,
  losses: Loss[],
): void {
  const normal: Block[] = [];
  const results: { readonly block: ToolResultBlock; readonly index: number }[] =
    [];
  let firstNormal = -1;
  let lastResult = -1;
  for (let index = 0; index < message.content.length; index += 1) {
    const block = message.content[index]!;
    if (block.type === "tool_result") {
      results.push({ block, index });
      lastResult = index;
    } else {
      if (firstNormal < 0) firstNormal = index;
      normal.push(block);
    }
  }
  if (firstNormal >= 0 && firstNormal < lastResult)
    losses.push(
      loss(`messages[${messageIndex}]`, "ordering", "degraded"),
    );
  for (const result of results) {
    let output = "";
    for (let index = 0; index < result.block.content.length; index += 1) {
      const block = result.block.content[index]!;
      if (block.type === "text") output += block.text;
      else
        losses.push(
          loss(
            `messages[${messageIndex}].content[${result.index}].content[${index}]`,
            "content",
            "unsupported-semantic",
          ),
        );
    }
    if (result.block.is_error === true)
      losses.push(
        loss(
          `messages[${messageIndex}].content[${result.index}].is_error`,
          "is_error",
          "unmapped-field",
        ),
      );
    items.push({
      type: "function_call_output",
      call_id: result.block.tool_use_id,
      output,
    });
  }
  if (normal.length > 0 || results.length === 0)
    items.push({
      role: "user",
      content: encodeUserContent(
        normal,
        `messages[${messageIndex}].content`,
        losses,
      ),
    });
}

function decodeContent(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): Block[] {
  if (value === undefined || value === null)
    return [{ type: "text", text: "" }];
  if (typeof value === "string") return [{ type: "text", text: value }];
  const blocks: Block[] = [];
  const parts = array(value, path);
  for (let index = 0; index < parts.length; index += 1) {
    const part = object(parts[index], `${path}[${index}]`);
    if (part.type === "input_text" || part.type === "output_text")
      blocks.push({
        type: "text",
        text:
          part.text === undefined
            ? ""
            : string(part.text, `${path}[${index}].text`),
      });
    else if (part.type === "input_image") {
      const image = decodeImage(
        string(part.image_url, `${path}[${index}].image_url`),
      );
      if (image === undefined)
        losses.push(
          loss(
            `${path}[${index}].image_url`,
            "image_url",
            "unsupported-semantic",
          ),
        );
      else blocks.push(image);
    } else
      losses.push(
        loss(`${path}[${index}]`, "type", "unsupported-semantic"),
      );
  }
  return blocks;
}

function encodeUserContent(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
): JsonValue {
  let text = "";
  let textBlocks = 0;
  let otherBlocks = 0;
  const parts: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    if (block.type === "text") {
      text += block.text;
      textBlocks += 1;
      parts.push({ type: "input_text", text: block.text });
    } else if (block.type === "image") {
      const image = encodeImage(block);
      if (image === undefined)
        losses.push(
          loss(`${path}[${index}]`, "image", "unsupported-semantic"),
        );
      else {
        otherBlocks += 1;
        parts.push({ type: "input_image", image_url: image });
      }
    } else {
      otherBlocks += 1;
      losses.push(
        loss(`${path}[${index}]`, "content", "unsupported-semantic"),
      );
    }
  }
  return textBlocks <= 1 && otherBlocks === 0 ? text : parts;
}

function decodeImage(value: string): ImageBlock | undefined {
  if (value.startsWith("https:")) {
    try {
      const url = new URL(value);
      if (url.protocol === "https:" && url.host !== "")
        return { type: "image", url: value };
    } catch {}
    return undefined;
  }
  if (!value.startsWith("data:")) return undefined;
  const comma = value.indexOf(",");
  if (comma < 0) return undefined;
  const metadata = value.slice(5, comma);
  if (!metadata.endsWith(";base64")) return undefined;
  const mediaType = metadata.slice(0, -7);
  if (!mediaType.toLowerCase().startsWith("image/") || mediaType === "image/")
    return undefined;
  return {
    type: "image",
    media_type: mediaType,
    data: value.slice(comma + 1),
  };
}

function encodeImage(image: ImageBlock): string | undefined {
  if (image.data !== undefined && image.url !== undefined) return undefined;
  if (image.data !== undefined) {
    if (
      image.media_type === undefined ||
      !image.media_type.toLowerCase().startsWith("image/") ||
      image.media_type === "image/"
    )
      return undefined;
    return `data:${image.media_type};base64,${image.data}`;
  }
  if (image.url !== undefined) {
    try {
      const url = new URL(image.url);
      if (url.protocol === "https:" && url.host !== "") return image.url;
    } catch {}
  }
  return undefined;
}

function decodeToolChoice(
  value: JsonValue | undefined,
  losses: Loss[],
): ToolChoice | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string") {
    if (value === "auto" || value === "none") return { mode: value };
    if (value === "required") return { mode: "any" };
  } else if (
    typeof value === "object" &&
    !isJsonArray(value) &&
    !isJsonNumber(value) &&
    value.type === "function" &&
    typeof value.name === "string" &&
    value.name !== ""
  )
    return { mode: "tool", name: value.name };
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function encodeToolChoice(
  value: ToolChoice | undefined,
  losses: Loss[],
): JsonValue | undefined {
  if (value === undefined) return undefined;
  if (value.mode === "auto" || value.mode === "none") return value.mode;
  if (value.mode === "any") return "required";
  if (value.mode === "tool" && value.name !== "")
    return { type: "function", name: value.name };
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function loss(path: string, field: string, reason: LossReason): Loss {
  return { path, field, reason };
}
function optionalArray(value: JsonValue | undefined, name: string): readonly JsonValue[] {
  return value === undefined || value === null ? [] : array(value, name);
}
function array(value: JsonValue | undefined, name: string): readonly JsonValue[] {
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
  throw new OxaError("type-violation", `responses: ${message}`);
}
