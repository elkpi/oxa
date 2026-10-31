import { OxaError } from "../../error.js";
import {
  assertSourceMatches,
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  jsonText,
  parseJson,
  type JsonObject,
  type JsonValue,
} from "../../json/index.js";
import type { ConversionResult, Loss, LossReason } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import {
  parseUsageInteger,
  validateRequest,
  omittedNativeRequestLosses,
  STREAM_OPTIONS_LOSS_DETAIL,
  type NativeAnnotation,
} from "../../ir/index.js";
import { auditWire, mergeWireLosses } from "../../json/wire-audit.js";
import { stringMetadata } from "../../json/metadata.js";
import { CHAT_REQUEST, CHAT_RESPONSE } from "./audit.js";
import type {
  Block,
  CustomToolResultBlock,
  CustomToolUseBlock,
  ImageBlock,
  Message,
  ReasoningEffort,
  Request,
  Response,
  ResponseFormat,
  TextBlock,
  Tool,
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
  const audit = auditWire(wire, CHAT_REQUEST);
  const result = decodeRequestObject(wire, options);
  return { value: result.value, losses: mergeWireLosses(result.losses, audit) };
}

export function decodeRequestJSON(
  raw: string,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  return decodeRequest(objectOrThrow(parseJson(raw), "request"), options);
}

function decodeRequestObject(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const losses: Loss[] = [];
  for (const field of [
    "parallel_tool_calls",
    "functions",
    "function_call",
    "logprobs",
    "top_logprobs",
  ])
    if (wire[field] !== undefined)
      losses.push(loss(field, field, "unmapped-field"));

  const tools = optionalArray(wire.tools, "tools").flatMap<Tool>(
    (value, index) => {
      const tool = object(value, `tools[${index}]`);
      if (tool.type === "custom") {
        const c = object(tool.custom, `tools[${index}].custom`);
        const fmt =
          c.format !== undefined
            ? object(c.format, `tools[${index}].custom.format`)
            : undefined;
        let format:
          | {
              readonly type: "grammar";
              readonly syntax: "lark" | "regex";
              readonly definition: string;
            }
          | { readonly type: "text" }
          | undefined;
        if (fmt !== undefined) {
          if (fmt.type === "grammar") {
            if (fmt.syntax === "lark" || fmt.syntax === "regex") {
              format = {
                type: "grammar",
                syntax: fmt.syntax,
                definition: string(
                  fmt.definition,
                  `tools[${index}].custom.format.definition`,
                ),
              };
            } else {
              losses.push(
                loss(
                  `tools[${index}].custom.format`,
                  "syntax",
                  "unsupported-semantic",
                ),
              );
            }
          } else if (fmt.type === "text") {
            format = { type: "text" };
          } else {
            losses.push(
              loss(
                `tools[${index}].custom.format`,
                "type",
                "unsupported-semantic",
              ),
            );
          }
        }
        return [
          {
            type: "custom" as const,
            name: string(c.name, `tools[${index}].custom.name`),
            ...(c.description === undefined
              ? {}
              : {
                  description: string(
                    c.description,
                    `tools[${index}].custom.description`,
                  ),
                }),
            ...(format === undefined ? {} : { format }),
          },
        ];
      }
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
          ...(fn.strict === undefined || fn.strict === null
            ? {}
            : {
                strict: boolean(fn.strict, `tools[${index}].function.strict`),
              }),
        },
      ];
    },
  );
  const toolChoice = decodeToolChoice(wire.tool_choice, losses);
  const messages: Message[] = [];
  const system: Request["system"] extends readonly (infer T)[] | undefined
    ? T[]
    : never = [];
  const nativeMessages = array(wire.messages, "messages");
  const customCallIds = new Set<string>();
  for (let i = 0; i < nativeMessages.length; i += 1) {
    const msg = object(nativeMessages[i], `messages[${i}]`);
    if (msg.role === "assistant" && Array.isArray(msg.tool_calls)) {
      for (const call of msg.tool_calls) {
        if (
          typeof call === "object" &&
          call !== null &&
          (call as any).type === "custom"
        ) {
          customCallIds.add(String((call as any).id));
        }
      }
    }
  }
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
        const toolCallId = string(
          item.tool_call_id,
          `messages[${index}].tool_call_id`,
        );
        if (customCallIds.has(toolCallId)) {
          content.push({
            type: "custom_tool_result",
            tool_use_id: toolCallId,
            output:
              typeof item.content === "string"
                ? item.content
                : decoded.filter(
                    (b): b is TextBlock | ImageBlock =>
                      b.type === "text" || b.type === "image",
                  ),
          });
        } else {
          content.push({
            type: "tool_result",
            tool_use_id: toolCallId,
            content: decoded,
          });
        }
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
    if (role === "system" || role === "developer") {
      for (let blockIndex = 0; blockIndex < content.length; blockIndex += 1) {
        const block = content[blockIndex]!;
        if (block.type === "text")
          system.push(
            role === "developer" ? { ...block, role: "developer" } : block,
          );
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
      if (message.content === null && calls.length > 0)
        content = content.filter(
          (block) => block.type !== "text" || block.text !== "",
        );
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
  if (
    wire.max_tokens !== undefined &&
    wire.max_completion_tokens !== undefined
  ) {
    fail(
      "chatcompletions: cannot specify both max_tokens and max_completion_tokens",
    );
  }
  const maxTokens =
    wire.max_tokens !== undefined
      ? whole(wire.max_tokens, "max_tokens")
      : wire.max_completion_tokens !== undefined
        ? whole(wire.max_completion_tokens, "max_completion_tokens")
        : undefined;
  if (
    wire.max_completion_tokens !== undefined &&
    maxTokens !== undefined &&
    maxTokens <= 0n
  ) {
    fail("max_completion_tokens must be a positive integer");
  }

  const native: NativeAnnotation[] = [];
  if (wire.max_completion_tokens !== undefined) {
    native.push({
      source: "openai",
      kind: "max_completion_tokens",
      value: "true",
    });
  }
  if (wire.store !== undefined) {
    if (wire.store !== null && typeof wire.store !== "boolean") {
      fail("store must be a boolean or null");
    }
    native.push({
      source: "openai",
      kind: "store",
      value: wire.store === null ? "null" : wire.store ? "true" : "false",
    });
  }
  if (wire.stream !== undefined) {
    if (wire.stream !== null && typeof wire.stream !== "boolean") {
      fail("stream must be a boolean or null");
    }
    native.push({
      source: "openai",
      kind: "stream",
      value: wire.stream === null ? "null" : wire.stream ? "true" : "false",
    });
  }
  if (wire.stream_options !== undefined) {
    if (wire.stream_options !== null) {
      if (
        typeof wire.stream_options !== "object" ||
        Array.isArray(wire.stream_options)
      ) {
        fail("stream_options must be an object or null");
      }
      const so = wire.stream_options as Record<string, unknown>;
      if (
        so.include_usage !== undefined &&
        typeof so.include_usage !== "boolean"
      ) {
        fail("stream_options.include_usage must be a boolean");
      }
      if (
        so.include_obfuscation !== undefined &&
        typeof so.include_obfuscation !== "boolean"
      ) {
        fail("stream_options.include_obfuscation must be a boolean");
      }
      const cleanSo: Record<string, boolean> = {};
      if (typeof so.include_obfuscation === "boolean")
        cleanSo.include_obfuscation = so.include_obfuscation;
      if (typeof so.include_usage === "boolean")
        cleanSo.include_usage = so.include_usage;
      native.push({
        source: "openai",
        kind: "stream_options",
        value: JSON.stringify(cleanSo),
        ...(cleanSo.include_usage === true
          ? {
              omission: {
                field: "stream_options",
                reason: "degraded",
                detail: STREAM_OPTIONS_LOSS_DETAIL,
              },
            }
          : {}),
      });
    } else {
      native.push({
        source: "openai",
        kind: "stream_options",
        value: "null",
      });
    }
  }

  const responseFormat = decodeResponseFormat(wire.response_format, losses);
  const params = {
    ...(wire.temperature === undefined
      ? {}
      : { temperature: number(wire.temperature, "temperature") }),
    ...(wire.top_p === undefined ? {} : { top_p: number(wire.top_p, "top_p") }),
    ...(maxTokens === undefined ? {} : { max_tokens: maxTokens }),
    ...(wire.stop === undefined
      ? {}
      : { stop_sequences: strings(wire.stop, "stop") }),
    ...decodeReasoningEffort(wire.reasoning_effort, losses),
    ...decodeVerbosity(wire.verbosity, "verbosity", losses),
    ...(responseFormat === undefined
      ? {}
      : { response_format: responseFormat }),
  };
  const request: Request = {
    model: mapModel(options.modelMapper, string(wire.model, "model")),
    ...(system.length === 0 ? {} : { system }),
    messages,
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    ...(Object.keys(params).length === 0 ? {} : { params }),
    ...(native.length === 0 ? {} : { native }),
    ...stringMetadata(wire.metadata, "chatcompletions"),
  };
  validateRequest(request);
  return { value: request, losses };
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const audit = auditWire(wire, CHAT_RESPONSE);
  const result = decodeResponseObject(wire, options);
  return { value: result.value, losses: mergeWireLosses(result.losses, audit) };
}

export function decodeResponseJSON(
  raw: string,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  return decodeResponse(objectOrThrow(parseJson(raw), "response"), options);
}

function decodeResponseObject(
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
  if (message.content === null && calls.length > 0)
    content = content.filter(
      (block) => block.type !== "text" || block.text !== "",
    );
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
  const messages: JsonValue[] = [];
  if (request.system !== undefined && request.system.length > 0) {
    if (request.system.some((block) => block.role === "developer")) {
      for (const block of request.system)
        messages.push({
          role: block.role === "developer" ? "developer" : "system",
          content: block.text,
        });
    } else
      messages.push({
        role: "system",
        content: request.system.map((block) => block.text).join(""),
      });
  }
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
      readonly block: ToolResultBlock | CustomToolResultBlock;
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
      if (block.type === "tool_result" || block.type === "custom_tool_result") {
        results.push({ block, index: blockIndex });
        lastResult = blockIndex;
      } else if (block.type === "apply_patch_tool_result") {
        losses.push(
          loss(
            `messages[${index}].content[${blockIndex}]`,
            "type",
            "unsupported-semantic",
          ),
        );
      } else {
        if (firstNormal < 0) firstNormal = blockIndex;
        normal.push(block);
      }
    }
    if (firstNormal >= 0 && firstNormal < lastResult)
      losses.push(loss(`messages[${index}]`, "ordering", "degraded"));
    for (const result of results) {
      if (result.block.type === "custom_tool_result") {
        messages.push(
          encodeCustomToolResult(
            result.block,
            `messages[${index}].content[${result.index}]`,
            losses,
          ),
        );
      } else {
        messages.push(
          encodeToolResult(
            result.block,
            `messages[${index}].content[${result.index}]`,
            losses,
          ),
        );
      }
    }
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
  const tools: JsonObject[] =
    request.tools?.flatMap<JsonObject>((tool, index) => {
      if (tool.type === "native") {
        losses.push({
          path: `tools[${index}]`,
          field: "type",
          reason: "unsupported-semantic",
          detail: `native tool "${tool.name}" from "${tool.source}" cannot be represented by the target protocol`,
        });
        return [];
      }
      if (tool.type === "custom") {
        return [
          {
            type: "custom",
            custom: {
              name: tool.name,
              ...(tool.description === undefined
                ? {}
                : { description: tool.description }),
              ...(tool.format === undefined
                ? {}
                : {
                    format:
                      tool.format.type === "grammar"
                        ? {
                            type: "grammar",
                            syntax: tool.format.syntax,
                            definition: tool.format.definition,
                          }
                        : { type: "text" },
                  }),
            },
          },
        ];
      }
      if (tool.type === "apply_patch") {
        losses.push(loss(`tools[${index}]`, "type", "unsupported-semantic"));
        return [];
      }
      return [
        {
          type: "function",
          function: {
            name: tool.name,
            ...(tool.description === undefined
              ? {}
              : { description: tool.description }),
            parameters: tool.input_schema,
            ...(tool.strict === undefined ? {} : { strict: tool.strict }),
          },
        },
      ];
    }) ?? [];
  const choiceName =
    request.tool_choice?.mode === "tool" && "name" in request.tool_choice
      ? request.tool_choice.name
      : undefined;
  const nativeNamedChoiceDropped =
    choiceName !== undefined &&
    ((request.tool_choice?.mode === "tool" &&
      request.tool_choice.tool_type === "native") ||
      request.tools?.some(
        (t) => t.type === "native" && t.name === choiceName,
      )) &&
    !tools.some(
      (t) =>
        (t as any).function?.name === choiceName ||
        (t as any).custom?.name === choiceName,
    );
  const toolChoice =
    request.tool_choice?.mode === "any" &&
    request.tools !== undefined &&
    request.tools.length > 0 &&
    tools.length === 0
      ? (losses.push(
          loss("tool_choice", "tool_choice", "unsupported-semantic"),
        ),
        undefined)
      : nativeNamedChoiceDropped
        ? (losses.push({
            path: "tool_choice",
            field: "tool_choice",
            reason: "unsupported-semantic",
            detail: `native tool choice "${choiceName}" cannot be represented by the target protocol`,
          }),
          undefined)
        : encodeToolChoice(request.tool_choice, losses);
  const params = request.params;
  const format = params?.response_format;
  const unnamedSchema = format?.type === "json_schema" && !format.name;
  if (unnamedSchema)
    losses.push(loss("params.response_format", "name", "unsupported-semantic"));
  const hasMaxCompletionTokens = request.native?.some(
    (n) => n.source === "openai" && n.kind === "max_completion_tokens",
  );
  const storeAnn = request.native?.find(
    (n) => n.source === "openai" && n.kind === "store",
  );
  const streamAnn = request.native?.find(
    (n) => n.source === "openai" && n.kind === "stream",
  );
  const streamOptionsAnn = request.native?.find(
    (n) => n.source === "openai" && n.kind === "stream_options",
  );

  let storeVal: boolean | null | undefined = undefined;
  if (storeAnn) {
    try {
      const parsed = JSON.parse(storeAnn.value);
      if (typeof parsed === "boolean" || parsed === null) {
        storeVal = parsed;
      }
    } catch {}
  }
  let streamVal: boolean | null | undefined = undefined;
  if (streamAnn) {
    try {
      const parsed = JSON.parse(streamAnn.value);
      if (typeof parsed === "boolean" || parsed === null) {
        streamVal = parsed;
      }
    } catch {}
  }
  let streamOptionsVal: JsonValue | undefined = undefined;
  if (streamOptionsAnn) {
    try {
      const parsed = JSON.parse(streamOptionsAnn.value);
      if (
        parsed === null ||
        (typeof parsed === "object" && !Array.isArray(parsed))
      ) {
        streamOptionsVal = parsed as JsonValue;
      }
    } catch {}
  }

  const outValue: JsonObject = {
    model: mapModel(options.modelMapper, request.model),
    messages,
    ...(params?.temperature === undefined
      ? {}
      : { temperature: fromValue(params.temperature) }),
    ...(params?.top_p === undefined ? {} : { top_p: fromValue(params.top_p) }),
    ...(params?.max_tokens === undefined
      ? {}
      : hasMaxCompletionTokens
        ? { max_completion_tokens: integer(params.max_tokens) }
        : { max_tokens: integer(params.max_tokens) }),
    ...(storeVal !== undefined ? { store: storeVal } : {}),
    ...(streamVal !== undefined ? { stream: streamVal } : {}),
    ...(streamOptionsVal !== undefined
      ? { stream_options: streamOptionsVal }
      : {}),
    ...(params?.stop_sequences === undefined ||
    params.stop_sequences.length === 0
      ? {}
      : { stop: [...params.stop_sequences] }),
    ...(params?.reasoning_effort === undefined
      ? {}
      : { reasoning_effort: params.reasoning_effort }),
    ...(params?.verbosity === undefined ? {} : { verbosity: params.verbosity }),
    ...(format === undefined || unnamedSchema
      ? {}
      : { response_format: encodeResponseFormat(format) }),
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    ...(request.metadata === undefined ? {} : { metadata: request.metadata }),
  };
  const omissionLosses = omittedNativeRequestLosses(request, "chatcompletions");
  losses.push(...omissionLosses);
  return {
    value: outValue,
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
      if (
        response.content.some(
          (block) =>
            block.type === "tool_use" || block.type === "custom_tool_use",
        )
      )
        finishReason = "tool_calls";
      else {
        finishReason = "stop";
        losses.push(loss("stop_reason", "stop_reason", "degraded"));
      }
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
    if (type === "text")
      blocks.push({
        type: "text",
        text: string(part.text, `${path}[${index}].text`),
      });
    else if (type === "image_url") {
      const image = decodeImage(
        object(part.image_url, `${path}[${index}].image_url`),
        `${path}[${index}].image_url`,
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
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
  }
  return blocks;
}

function decodeToolCalls(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): Block[] {
  if (value === undefined || value === null) return [];
  const calls = array(value, path);
  const blocks: Block[] = [];
  for (let index = 0; index < calls.length; index += 1) {
    const call = object(calls[index], `${path}[${index}]`);
    const type = string(call.type, `${path}[${index}].type`);
    if (type === "custom") {
      const c = object(call.custom, `${path}[${index}].custom`);
      blocks.push({
        type: "custom_tool_use",
        id: string(call.id, `${path}[${index}].id`),
        name: string(c.name, `${path}[${index}].custom.name`),
        input: string(c.input, `${path}[${index}].custom.input`),
      });
      continue;
    }
    if (type !== "function") {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
      continue;
    }
    const fn = object(call.function, `${path}[${index}].function`);
    blocks.push({
      type: "tool_use",
      id: string(call.id, `${path}[${index}].id`),
      name: string(fn.name, `${path}[${index}].function.name`),
      input: jsonText(
        string(fn.arguments, `${path}[${index}].function.arguments`),
      ),
    });
  }
  return blocks;
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
      if (block.citations && block.citations.length > 0) {
        let hasAnthropic = false;
        let hasOpenAI = false;
        for (const c of block.citations) {
          if (c.source === "openai") hasOpenAI = true;
          else hasAnthropic = true;
        }
        if (hasAnthropic) {
          losses.push({
            path: `${path}[${index}].citations`,
            field: "citations",
            reason: "unsupported-semantic",
            detail:
              "anthropic text citations; the target face cannot preserve source locations and replay metadata",
          });
        }
        if (hasOpenAI) {
          losses.push({
            path: `${path}[${index}].citations`,
            field: "citations",
            reason: "unsupported-semantic",
            detail:
              "openai text annotations; the target face cannot preserve source locations and replay metadata",
          });
        }
      }
    } else if (block.type === "redacted_thinking") {
      losses.push({
        path: `${path}[${index}]`,
        field: "type",
        reason: "unsupported-semantic",
        detail:
          "anthropic encrypted thinking replay state; the target face cannot preserve this block",
      });
    } else if (block.type === "image") {
      const image = encodeImage(block, `${path}[${index}]`);
      if (image === undefined)
        losses.push(
          loss(`${path}[${index}]`, "media_type", "unsupported-semantic"),
        );
      else {
        hasImage = true;
        parts.push({ type: "image_url", image_url: image });
      }
    } else {
      losses.push(loss(`${path}[${index}]`, "content", "unsupported-semantic"));
    }
  }
  return !hasImage ? text : parts;
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
    if (block.type === "text") {
      content += block.text;
      if (block.citations && block.citations.length > 0) {
        let hasAnthropic = false;
        let hasOpenAI = false;
        for (const c of block.citations) {
          if (c.source === "openai") hasOpenAI = true;
          else hasAnthropic = true;
        }
        if (hasAnthropic) {
          losses.push({
            path: `${path}[${index}].citations`,
            field: "citations",
            reason: "unsupported-semantic",
            detail:
              "anthropic text citations; the target face cannot preserve source locations and replay metadata",
          });
        }
        if (hasOpenAI) {
          losses.push({
            path: `${path}[${index}].citations`,
            field: "citations",
            reason: "unsupported-semantic",
            detail:
              "openai text annotations; the target face cannot preserve source locations and replay metadata",
          });
        }
      }
    } else if (block.type === "redacted_thinking") {
      losses.push({
        path: `${path}[${index}]`,
        field: "type",
        reason: "unsupported-semantic",
        detail:
          "anthropic encrypted thinking replay state; the target face cannot preserve this block",
      });
    } else if (block.type === "thinking") {
      reasoning = (reasoning ?? "") + block.thinking;
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
    else if (block.type === "web_search_call")
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
    else if (block.type === "custom_tool_use") {
      toolCalls.push({
        id: block.id,
        type: "custom",
        custom: {
          name: block.name,
          input: block.input,
        },
      });
    } else
      losses.push(loss(`${path}[${index}]`, "content", "unsupported-semantic"));
  }
  return {
    role: "assistant",
    content,
    ...(reasoning === undefined ? {} : { reasoning_content: reasoning }),
    ...(toolCalls.length === 0 ? {} : { tool_calls: toolCalls }),
  };
}

function encodeCustomToolResult(
  result: CustomToolResultBlock,
  path: string,
  losses: Loss[],
): JsonObject {
  let content: JsonValue;
  if (typeof result.output === "string") {
    content = result.output;
  } else {
    const parts: JsonValue[] = [];
    for (let index = 0; index < result.output.length; index += 1) {
      const block = result.output[index]!;
      if (block.type === "text") {
        parts.push({ type: "text", text: block.text });
      } else if (block.type === "image") {
        const image = encodeImage(block, `${path}.output[${index}]`);
        if (image === undefined)
          losses.push(
            loss(
              `${path}.output[${index}]`,
              "media_type",
              "unsupported-semantic",
            ),
          );
        else {
          parts.push({ type: "image_url", image_url: image });
        }
      } else {
        losses.push(
          loss(`${path}.output[${index}]`, "content", "unsupported-semantic"),
        );
      }
    }
    content = parts;
  }
  return {
    role: "tool",
    tool_call_id: result.tool_use_id,
    content,
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
    value.type === "custom" &&
    typeof object(value.custom, "tool_choice.custom").name === "string"
  ) {
    return {
      mode: "tool",
      name: string(
        object(value.custom, "tool_choice.custom").name,
        "tool_choice.custom.name",
      ),
      tool_type: "custom",
    };
  } else if (
    typeof value === "object" &&
    !isJsonArray(value) &&
    !isJsonNumber(value) &&
    value.type === "function" &&
    typeof object(value.function, "tool_choice.function").name === "string"
  )
    return {
      mode: "tool",
      name: string(
        object(value.function, "tool_choice.function").name,
        "tool_choice.function.name",
      ),
    };
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
  if (value.mode === "tool" && value.name !== "") {
    if (value.tool_type === "custom") {
      return { type: "custom", custom: { name: value.name } };
    }
    if (value.tool_type === "apply_patch") {
      losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
      return undefined;
    }
    return { type: "function", function: { name: value.name } };
  }
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function decodeResponseFormat(
  value: JsonValue | undefined,
  losses: Loss[],
): ResponseFormat | undefined {
  if (value === undefined || value === null) return undefined;
  const format = object(value, "response_format");
  const type = string(format.type, "response_format.type");
  switch (type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema": {
      const schema = object(format.json_schema, "response_format.json_schema");
      const name = string(schema.name, "response_format.json_schema.name");
      if (!name) fail("response_format.json_schema.name must be non-empty");
      return {
        type: "json_schema",
        name,
        ...(schema.description === undefined
          ? {}
          : {
              description: string(
                schema.description,
                "response_format.json_schema.description",
              ),
            }),
        schema: object(schema.schema, "response_format.json_schema.schema"),
        ...(schema.strict === undefined
          ? {}
          : {
              strict: boolean(
                schema.strict,
                "response_format.json_schema.strict",
              ),
            }),
      };
    }
    default:
      losses.push(loss("response_format.type", "type", "unmapped-value"));
      return undefined;
  }
}

function encodeResponseFormat(format: ResponseFormat): JsonObject {
  switch (format.type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema":
      assertSourceMatches(format.schema);
      return {
        type: "json_schema",
        json_schema: {
          name: format.name!,
          ...(format.description === undefined
            ? {}
            : { description: format.description }),
          schema: format.schema,
          ...(format.strict === undefined ? {} : { strict: format.strict }),
        },
      };
  }
}

function decodeReasoningEffort(
  value: JsonValue | undefined,
  losses: Loss[],
): { readonly reasoning_effort?: ReasoningEffort } {
  if (value === undefined || value === null) return {};
  const effort = string(value, "reasoning_effort");
  if (
    effort === "minimal" ||
    effort === "low" ||
    effort === "medium" ||
    effort === "high" ||
    effort === "xhigh" ||
    effort === "max"
  )
    return { reasoning_effort: effort };
  losses.push(loss("reasoning_effort", "reasoning_effort", "unmapped-value"));
  return {};
}

function decodeUsageDetails(usage: JsonObject): {
  readonly input_tokens_details?: { readonly cached_tokens: bigint };
  readonly output_tokens_details?: { readonly reasoning_tokens: bigint };
} {
  const promptDetails = usage.prompt_tokens_details;
  const completionDetails = usage.completion_tokens_details;
  return {
    ...(promptDetails === undefined || promptDetails === null
      ? {}
      : {
          input_tokens_details: {
            cached_tokens: parseUsageInteger(
              whole(
                object(promptDetails, "usage.prompt_tokens_details")
                  .cached_tokens,
                "usage.prompt_tokens_details.cached_tokens",
              ),
              "usage.prompt_tokens_details.cached_tokens",
            ),
          },
        }),
    ...(completionDetails === undefined || completionDetails === null
      ? {}
      : {
          output_tokens_details: {
            reasoning_tokens: parseUsageInteger(
              whole(
                object(completionDetails, "usage.completion_tokens_details")
                  .reasoning_tokens,
                "usage.completion_tokens_details.reasoning_tokens",
              ),
              "usage.completion_tokens_details.reasoning_tokens",
            ),
          },
        }),
  };
}

function decodeImage(image: JsonObject, path: string): ImageBlock | undefined {
  const url = string(image.url, `${path}.url`);
  if (url.startsWith("https:")) {
    try {
      const parsed = new URL(url);
      if (parsed.protocol === "https:" && parsed.host !== "")
        return { type: "image", url };
    } catch {}
    return undefined;
  }
  if (!url.startsWith("data:")) return undefined;
  const comma = url.indexOf(",");
  if (comma < 0) return undefined;
  const metadata = url.slice(5, comma);
  if (!metadata.endsWith(";base64")) return undefined;
  const mediaType = metadata.slice(0, -7);
  if (!mediaType.toLowerCase().startsWith("image/") || mediaType === "image/")
    return undefined;
  return {
    type: "image",
    media_type: mediaType,
    data: url.slice(comma + 1),
  };
}

function encodeImage(image: ImageBlock, path: string): JsonObject | undefined {
  if (image.data !== undefined && image.url !== undefined) return undefined;
  if (image.data !== undefined) {
    if (
      image.media_type === undefined ||
      !image.media_type.toLowerCase().startsWith("image/") ||
      image.media_type === "image/"
    )
      return undefined;
    return { url: `data:${image.media_type};base64,${image.data}` };
  }
  if (image.url !== undefined) {
    try {
      const url = new URL(image.url);
      if (url.protocol === "https:" && url.host !== "")
        return { url: image.url };
    } catch {}
  }
  return undefined;
}

function decodeVerbosity(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): { verbosity?: "low" | "medium" | "high" } {
  if (value === undefined || value === null) return {};
  const v = string(value, path);
  if (v === "low" || v === "medium" || v === "high") return { verbosity: v };
  losses.push(loss(path, "verbosity", "unmapped-value"));
  return {};
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
function objectOrThrow(value: JsonValue, name: string): JsonObject {
  return object(value, name);
}
function string(value: JsonValue | undefined, name: string): string {
  if (typeof value !== "string") fail(`${name} must be a string`);
  return value;
}
function strings(
  value: JsonValue | undefined,
  name: string,
): readonly string[] {
  if (typeof value === "string") return [value];
  return array(value, name).map((item, index) =>
    string(item, `${name}[${index}]`),
  );
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
function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
}
function fail(message: string): never {
  throw new OxaError("type-violation", `chatcompletions: ${message}`);
}
