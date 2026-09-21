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
import { validateRequest } from "../../ir/index.js";
import type {
  Block,
  ImageBlock,
  Message,
  ReasoningEffort,
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
  for (const field of [
    "parallel_tool_calls",
    "functions",
    "function_call",
    "response_format",
    "logprobs",
    "top_logprobs",
    "metadata",
  ])
    if (wire[field] !== undefined)
      losses.push(loss(field, field, "unmapped-field"));

  const tools = optionalArray(wire.tools, "tools").flatMap((value, index) => {
    const tool = object(value, `tools[${index}]`);
    if (tool.type !== "function") {
      losses.push(loss(`tools[${index}]`, "type", "unsupported-semantic"));
      return [];
    }
    const fn = object(tool.function, `tools[${index}].function`);
    return [
      {
        name: string(fn.name, `tools[${index}].function.name`),
        ...(fn.description === undefined
          ? {}
          : {
              description: string(
                fn.description,
                `tools[${index}].function.description`,
              ),
            }),
        input_schema: object(
          fn.parameters,
          `tools[${index}].function.parameters`,
        ),
      },
    ];
  });
  const toolChoice = decodeToolChoice(wire.tool_choice, losses);
  const messages: Message[] = [];
  const system: Request["system"] extends readonly (infer T)[] | undefined
    ? T[]
    : never = [];
  const nativeMessages = array(wire.messages, "messages");
  for (let index = 0; index < nativeMessages.length; index += 1) {
    const message = object(nativeMessages[index], `messages[${index}]`);
    const role = string(message.role, `messages[${index}].role`);
    if (role === "tool") {
      const content: Block[] = [];
      while (index < nativeMessages.length) {
        const item = object(nativeMessages[index], `messages[${index}]`);
        if (item.role !== "tool") break;
        const decoded = decodeContent(
          item.content,
          `messages[${index}].content`,
          losses,
        );
        content.push({
          type: "tool_result",
          tool_use_id: string(
            item.tool_call_id,
            `messages[${index}].tool_call_id`,
          ),
          content: decoded,
        });
        if (item.function_call !== undefined)
          losses.push(
            loss(
              `messages[${index}].function_call`,
              "function_call",
              "unmapped-field",
            ),
          );
        index += 1;
      }
      index -= 1;
      messages.push({ role: "user", content });
      continue;
    }
    let content = decodeContent(
      message.content,
      `messages[${index}].content`,
      losses,
    );
    if (role === "system") {
      for (let blockIndex = 0; blockIndex < content.length; blockIndex += 1) {
        const block = content[blockIndex]!;
        if (block.type === "text") system.push(block);
        else
          losses.push(
            loss(
              `messages[${index}].content[${blockIndex}]`,
              "content",
              "unsupported-semantic",
            ),
          );
      }
    } else if (role === "user") {
      if (content.length === 0) content = [{ type: "text", text: "" }];
      messages.push({ role, content });
    } else if (role === "assistant") {
      if (
        message.reasoning_content !== undefined &&
        message.reasoning_content !== null
      ) {
        const reasoning = string(
          message.reasoning_content,
          `messages[${index}].reasoning_content`,
        );
        if (reasoning !== "")
          content = [{ type: "thinking", thinking: reasoning }, ...content];
      }
      const calls = decodeToolCalls(
        message.tool_calls,
        `messages[${index}].tool_calls`,
        losses,
      );
      if (message.content === null && calls.length > 0) content = [];
      content.push(...calls);
      if (content.length === 0) content.push({ type: "text", text: "" });
      messages.push({ role, content });
    } else {
      fail(`messages[${index}]: unknown role ${role}`);
    }
    if (message.function_call !== undefined)
      losses.push(
        loss(
          `messages[${index}].function_call`,
          "function_call",
          "unmapped-field",
        ),
      );
  }
  const params = {
    ...(wire.temperature === undefined
      ? {}
      : { temperature: number(wire.temperature, "temperature") }),
    ...(wire.top_p === undefined ? {} : { top_p: number(wire.top_p, "top_p") }),
    ...(wire.max_tokens === undefined
      ? {}
      : { max_tokens: whole(wire.max_tokens, "max_tokens") }),
    ...(wire.stop === undefined
      ? {}
      : { stop_sequences: strings(wire.stop, "stop") }),
    ...decodeReasoningEffort(wire.reasoning_effort, losses),
  };
  const request: Request = {
    model: mapModel(options.modelMapper, string(wire.model, "model")),
    ...(system.length === 0 ? {} : { system }),
    messages,
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    ...(Object.keys(params).length === 0 ? {} : { params }),
  };
  validateRequest(request);
  return { value: request, losses };
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const choices = array(wire.choices, "choices");
  if (choices.length === 0) fail("response carries no choices");
  const choice = object(choices[0], "choices[0]");
  const message = object(choice.message, "choices[0].message");
  const losses: Loss[] = [];
  let content = decodeContent(
    message.content,
    "choices[0].message.content",
    losses,
  );
  if (
    message.reasoning_content !== undefined &&
    message.reasoning_content !== null
  ) {
    const reasoning = string(
      message.reasoning_content,
      "choices[0].message.reasoning_content",
    );
    if (reasoning !== "")
      content = [{ type: "thinking", thinking: reasoning }, ...content];
  }
  const calls = decodeToolCalls(
    message.tool_calls,
    "choices[0].message.tool_calls",
    losses,
  );
  if (message.content === null && calls.length > 0) content = [];
  content.push(...calls);
  if (message.function_call !== undefined)
    losses.push(
      loss(
        "choices[0].message.function_call",
        "function_call",
        "unmapped-field",
      ),
    );
  const finish = string(choice.finish_reason, "choices[0].finish_reason");
  let stopReason: Response["stop_reason"];
  switch (finish) {
    case "stop":
      stopReason = "end_turn";
      break;
    case "length":
      stopReason = "max_tokens";
      break;
    case "content_filter":
      stopReason = "refusal";
      break;
    case "tool_calls":
      stopReason = "tool_use";
      break;
    default:
      stopReason = "other";
      losses.push(
        loss("choices[0].finish_reason", "finish_reason", "unmapped-value"),
      );
  }
  const usage =
    wire.usage === undefined || wire.usage === null
      ? { input_tokens: 0n, output_tokens: 0n }
      : (() => {
          const value = object(wire.usage, "usage");
          return {
            input_tokens: whole(value.prompt_tokens, "usage.prompt_tokens"),
            output_tokens: whole(
              value.completion_tokens,
              "usage.completion_tokens",
            ),
            ...decodeUsageDetails(value),
          };
        })();
  return {
    value: {
      id: string(wire.id, "id"),
      model: mapModel(options.modelMapper, string(wire.model, "model")),
      content,
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
  validateRequest(request);
  const losses: Loss[] = [];
  if (
    request.metadata !== undefined &&
    Object.keys(request.metadata).length > 0
  )
    losses.push(loss("metadata", "metadata", "unmapped-field"));
  const messages: JsonValue[] = [];
  if (request.system !== undefined && request.system.length > 0)
    messages.push({
      role: "system",
      content: request.system.map((block) => block.text).join(""),
    });
  for (let index = 0; index < request.messages.length; index += 1) {
    const message = request.messages[index]!;
    if (message.role === "assistant") {
      messages.push(
        encodeAssistant(message.content, `messages[${index}].content`, losses),
      );
      continue;
    }
    const normal: Block[] = [];
    const results: {
      readonly block: ToolResultBlock;
      readonly index: number;
    }[] = [];
    let firstNormal = -1;
    let lastResult = -1;
    for (
      let blockIndex = 0;
      blockIndex < message.content.length;
      blockIndex += 1
    ) {
      const block = message.content[blockIndex]!;
      if (block.type === "tool_result") {
        results.push({ block, index: blockIndex });
        lastResult = blockIndex;
      } else {
        if (firstNormal < 0) firstNormal = blockIndex;
        normal.push(block);
      }
    }
    if (firstNormal >= 0 && firstNormal < lastResult)
      losses.push(loss(`messages[${index}]`, "ordering", "degraded"));
    for (const result of results)
      messages.push(
        encodeToolResult(
          result.block,
          `messages[${index}].content[${result.index}]`,
          losses,
        ),
      );
    if (normal.length > 0 || results.length === 0)
      messages.push({
        role: "user",
        content: encodeUserContent(
          normal,
          `messages[${index}].content`,
          losses,
        ),
      });
  }
  const tools =
    request.tools?.map((tool) => ({
      type: "function",
      function: {
        name: tool.name,
        ...(tool.description === undefined
          ? {}
          : { description: tool.description }),
        parameters: tool.input_schema,
      },
    })) ?? [];
  const toolChoice = encodeToolChoice(request.tool_choice, losses);
  const params = request.params;
  return {
    value: {
      model: mapModel(options.modelMapper, request.model),
      messages,
      ...(params?.temperature === undefined
        ? {}
        : { temperature: fromValue(params.temperature) }),
      ...(params?.top_p === undefined
        ? {}
        : { top_p: fromValue(params.top_p) }),
      ...(params?.max_tokens === undefined
        ? {}
        : { max_tokens: integer(params.max_tokens) }),
      ...(params?.stop_sequences === undefined ||
      params.stop_sequences.length === 0
        ? {}
        : { stop: [...params.stop_sequences] }),
      ...(params?.reasoning_effort === undefined
        ? {}
        : { reasoning_effort: params.reasoning_effort }),
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
  const message = encodeAssistant(response.content, "content", losses);
  let finishReason: string;
  switch (response.stop_reason) {
    case "end_turn":
      finishReason = "stop";
      break;
    case "max_tokens":
      finishReason = "length";
      break;
    case "refusal":
      finishReason = "content_filter";
      break;
    case "tool_use":
      finishReason = "tool_calls";
      break;
    case "stop_sequence":
      finishReason = "stop";
      losses.push(loss("", "stop_sequence", "unmapped-value"));
      break;
    default:
      fail(`stop reason ${response.stop_reason} is unsupported`);
  }
  return {
    value: {
      id: response.id,
      object: "chat.completion",
      created: integer(0n),
      model: mapModel(options.modelMapper, response.model),
      choices: [
        {
          index: integer(0n),
          message,
          finish_reason: finishReason,
        },
      ],
      usage: {
        prompt_tokens: integer(response.usage.input_tokens),
        completion_tokens: integer(response.usage.output_tokens),
        total_tokens: integer(
          response.usage.input_tokens + response.usage.output_tokens,
        ),
        ...(response.usage.input_tokens_details === undefined
          ? {}
          : {
              prompt_tokens_details: {
                cached_tokens: integer(
                  response.usage.input_tokens_details.cached_tokens,
                ),
              },
            }),
        ...(response.usage.output_tokens_details === undefined
          ? {}
          : {
              completion_tokens_details: {
                reasoning_tokens: integer(
                  response.usage.output_tokens_details.reasoning_tokens,
                ),
              },
            }),
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
  if (value === undefined || value === null)
    return [{ type: "text", text: "" }];
  if (typeof value === "string") return [{ type: "text", text: value }];
  const parts = array(value, path);
  const blocks: Block[] = [];
  for (let index = 0; index < parts.length; index += 1) {
    const part = object(parts[index], `${path}[${index}]`);
    const type = string(part.type, `${path}[${index}].type`);
    if (type === "text") {
      blocks.push({
        type: "text",
        text:
          part.text === undefined
            ? ""
            : string(part.text, `${path}[${index}].text`),
      });
    } else if (type === "image_url") {
      const image = object(part.image_url, `${path}[${index}].image_url`);
      const decoded = decodeImage(
        string(image.url, `${path}[${index}].image_url.url`),
      );
      if (decoded === undefined)
        losses.push(
          loss(
            `${path}[${index}].image_url`,
            "image_url",
            "unsupported-semantic",
          ),
        );
      else blocks.push(decoded);
    } else {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
    }
  }
  return blocks;
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

function decodeToolCalls(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): Block[] {
  return optionalArray(value, path).flatMap((entry, index) => {
    const call = object(entry, `${path}[${index}]`);
    if (call.type !== "function") {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
      return [];
    }
    const fn = object(call.function, `${path}[${index}].function`);
    return [
      {
        type: "tool_use" as const,
        id: string(call.id, `${path}[${index}].id`),
        name: string(fn.name, `${path}[${index}].function.name`),
        input: jsonText(
          string(fn.arguments, `${path}[${index}].function.arguments`),
        ),
      },
    ];
  });
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
    value !== null &&
    !isJsonArray(value) &&
    !isJsonNumber(value)
  ) {
    const fn =
      value.function === undefined
        ? undefined
        : object(value.function, "tool_choice.function");
    if (
      value.type === "function" &&
      fn !== undefined &&
      typeof fn.name === "string"
    )
      return { mode: "tool", name: fn.name };
  }
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
    return { type: "function", function: { name: value.name } };
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function encodeAssistant(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
): JsonObject {
  let content = "";
  let reasoning: string | undefined;
  const toolCalls: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    if (block.type === "text") content += block.text;
    else if (block.type === "thinking") {
      reasoning = block.thinking;
      if (block.signature !== undefined)
        losses.push(
          loss(`${path}[${index}].signature`, "signature", "unmapped-field"),
        );
    } else if (block.type === "tool_use")
      toolCalls.push({
        id: block.id,
        type: "function",
        function: {
          name: block.name,
          arguments: block.input,
        },
      });
    else
      losses.push(loss(`${path}[${index}]`, "content", "unsupported-semantic"));
  }
  return {
    role: "assistant",
    content,
    ...(reasoning === undefined ? {} : { reasoning_content: reasoning }),
    ...(toolCalls.length === 0 ? {} : { tool_calls: toolCalls }),
  };
}

function encodeToolResult(
  result: ToolResultBlock,
  path: string,
  losses: Loss[],
): JsonObject {
  let content = "";
  for (let index = 0; index < result.content.length; index += 1) {
    const block = result.content[index]!;
    if (block.type === "text") content += block.text;
    else
      losses.push(
        loss(`${path}.content[${index}]`, "content", "unsupported-semantic"),
      );
  }
  if (result.is_error === true)
    losses.push(loss(`${path}.is_error`, "is_error", "unmapped-field"));
  return {
    role: "tool",
    tool_call_id: result.tool_use_id,
    content,
  };
}

function encodeUserContent(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
): JsonValue {
  let text = "";
  let hasImage = false;
  const parts: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    if (block.type === "text") {
      text += block.text;
      parts.push({ type: "text", text: block.text });
    } else if (block.type === "image") {
      const image = encodeImage(block);
      if (image === undefined)
        losses.push(loss(`${path}[${index}]`, "image", "unsupported-semantic"));
      else {
        hasImage = true;
        parts.push({ type: "image_url", image_url: { url: image } });
      }
    } else
      losses.push(loss(`${path}[${index}]`, "content", "unsupported-semantic"));
  }
  return hasImage ? parts : text;
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

function loss(path: string, field: string, reason: LossReason): Loss {
  return { path, field, reason };
}

function decodeReasoningEffort(
  value: JsonValue | undefined,
  losses: Loss[],
): { reasoning_effort?: ReasoningEffort } {
  if (value === undefined || value === null) return {};
  const effort = string(value, "reasoning_effort");
  if (
    effort === "minimal" ||
    effort === "low" ||
    effort === "medium" ||
    effort === "high"
  )
    return { reasoning_effort: effort };
  losses.push(loss("reasoning_effort", "reasoning_effort", "unmapped-value"));
  return {};
}

function decodeUsageDetails(
  value: JsonObject,
): Pick<
  Response["usage"],
  "input_tokens_details" | "output_tokens_details"
> {
  const promptDetails = value.prompt_tokens_details;
  const completionDetails = value.completion_tokens_details;
  return {
    ...(promptDetails === undefined || promptDetails === null
      ? {}
      : {
          input_tokens_details: {
            cached_tokens: whole(
              object(promptDetails, "usage.prompt_tokens_details").cached_tokens,
              "usage.prompt_tokens_details.cached_tokens",
            ),
          },
        }),
    ...(completionDetails === undefined || completionDetails === null
      ? {}
      : {
          output_tokens_details: {
            reasoning_tokens: whole(
              object(
                completionDetails,
                "usage.completion_tokens_details",
              ).reasoning_tokens,
              "usage.completion_tokens_details.reasoning_tokens",
            ),
          },
        }),
  };
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
  throw new OxaError("type-violation", `chatcompletions: ${message}`);
}
