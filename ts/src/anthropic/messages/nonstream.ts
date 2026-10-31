import { OxaError } from "../../error.js";
import {
  assertSourceMatches,
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
import {
  parseUsageInteger,
  validateCitationValue,
  validateRequest,
} from "../../ir/index.js";
import { auditWire, mergeWireLosses } from "../../json/wire-audit.js";
import { ANTHROPIC_REQUEST, ANTHROPIC_RESPONSE } from "./audit.js";
import {
  decodeRequestNative,
  encodeRequestNative,
  getBlockCacheControl,
  attachBlockCacheControl,
  getToolDeferLoading,
  getToolEagerInputStreaming,
  DEFER_LOADING_LOSS_DETAIL,
  NATIVE_KIND_DEFER_LOADING,
  NATIVE_KIND_EAGER_INPUT_STREAMING,
  EAGER_INPUT_STREAMING_LOSS_DETAIL,
} from "./native.js";
import {
  NATIVE_SOURCE_ANTHROPIC,
  DOCUMENT_TITLE_LOSS_DETAIL,
  DOCUMENT_CONTEXT_LOSS_DETAIL,
  DOCUMENT_CITATIONS_LOSS_DETAIL,
} from "../../ir/native.js";
import type {
  Block,
  CustomToolResultBlock,
  CustomToolUseBlock,
  DocumentBlock,
  ImageBlock,
  Message,
  NativeAnnotation,
  ReasoningEffort,
  Request,
  Response,
  TextBlock,
  TextCitation,
  ThinkingBlock,
  RedactedThinkingBlock,
  Tool,
  ToolChoice,
} from "../../ir/index.js";

function isCustomToolInputSchema(schema: JsonObject): boolean {
  if (schema.type !== "object") return false;
  const props = schema.properties;
  if (typeof props !== "object" || props === null || Array.isArray(props))
    return false;
  const keys = Object.keys(props);
  if (keys.length !== 1 || keys[0] !== "input") return false;
  const inputProp = (props as Record<string, unknown>).input;
  if (
    typeof inputProp !== "object" ||
    inputProp === null ||
    Array.isArray(inputProp)
  )
    return false;
  return (inputProp as Record<string, unknown>).type === "string";
}

function extractCustomInput(input: JsonObject): string | undefined {
  const keys = Object.keys(input);
  if (
    keys.length === 1 &&
    keys[0] === "input" &&
    typeof input.input === "string"
  ) {
    return input.input;
  }
  return undefined;
}

export interface NonstreamOptions {
  readonly modelMapper?: ModelMapper;
}

export function decodeRequest(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const audit = auditWire(wire, ANTHROPIC_REQUEST);
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
  const maxTokens = whole(wire.max_tokens, "max_tokens");
  if (maxTokens <= 0n) fail("max_tokens must be positive");
  const losses: Loss[] = [];
  const metadata = anthropicMetadata(wire.metadata, losses);
  const system: {
    readonly type: "text";
    readonly text: string;
    readonly native?: readonly NativeAnnotation[];
  }[] = [];
  if (typeof wire.system === "string") {
    system.push({ type: "text", text: wire.system });
  } else if (wire.system !== undefined && wire.system !== null) {
    const blocks = array(wire.system, "system");
    for (let index = 0; index < blocks.length; index += 1) {
      const block = object(blocks[index], `system[${index}]`);
      if (block.type !== "text") fail(`system[${index}].type must be text`);
      const native = attachBlockCacheControl(block, `system[${index}]`, losses);
      system.push({
        type: "text",
        text: string(block.text, `system[${index}].text`),
        ...(native ? { native } : {}),
      });
    }
  }
  const declaredTools = new Set<string>();
  const customTools = new Set<string>();
  const nativeTools = new Set<string>();
  const customCallIds = new Set<string>();
  const tools = optionalArray(wire.tools, "tools").map<Tool>((entry, index) => {
    const tool = object(entry, `tools[${index}]`);
    if (tool.name === undefined || tool.name === null) {
      fail(`tools[${index}].name is required`);
    }
    if (typeof tool.name !== "string") {
      fail(`tools[${index}].name must be a string`);
    }
    const name = tool.name;
    if (name.length === 0) {
      fail(`tools[${index}].name cannot be empty`);
    }
    declaredTools.add(name);

    if (tool.type !== undefined) {
      if (typeof tool.type !== "string") {
        fail(`tools[${index}].type must be a string`);
      }
      if (
        tool.type !== "" &&
        tool.type !== "function" &&
        tool.type !== "custom"
      ) {
        nativeTools.add(name);
        const definition = (sourceTextOf(tool) ??
          stringifyJson(tool)) as string;
        return {
          type: "native" as const,
          name,
          source: "anthropic",
          definition,
        };
      }
    }

    if ("defer_loading" in tool && tool.defer_loading !== undefined) {
      if (typeof tool.defer_loading !== "boolean") {
        fail(`tools[${index}].defer_loading must be a boolean`);
      }
      if (tool.defer_loading === true && tool.cache_control !== undefined) {
        fail(
          `tools[${index}]: cannot specify both defer_loading and cache_control`,
        );
      }
    }

    const schema = object(tool.input_schema, `tools[${index}].input_schema`);
    const native: NativeAnnotation[] = [];
    if (tool.defer_loading === true) {
      native.push({
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_DEFER_LOADING,
        value: "true",
        omission: {
          field: "defer_loading",
          reason: "degraded" as const,
          detail: DEFER_LOADING_LOSS_DETAIL,
        },
      });
    } else {
      const cc = attachBlockCacheControl(tool, `tools[${index}]`, losses);
      if (cc) {
        native.push(...cc);
      }
    }
    if (
      "eager_input_streaming" in tool &&
      tool.eager_input_streaming !== undefined
    ) {
      if (typeof tool.eager_input_streaming !== "boolean") {
        fail(`tools[${index}].eager_input_streaming must be a boolean`);
      }
      native.push({
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_EAGER_INPUT_STREAMING,
        value: tool.eager_input_streaming ? "true" : "false",
        ...(tool.eager_input_streaming
          ? {
              omission: {
                field: "eager_input_streaming",
                reason: "degraded" as const,
                detail: EAGER_INPUT_STREAMING_LOSS_DETAIL,
              },
            }
          : {}),
      });
    }
    if (tool.strict === undefined && isCustomToolInputSchema(schema)) {
      customTools.add(name);
      return {
        type: "custom" as const,
        name,
        ...(tool.description === undefined
          ? {}
          : {
              description: string(
                tool.description,
                `tools[${index}].description`,
              ),
            }),
        ...(native.length > 0 ? { native } : {}),
      };
    }
    return {
      name,
      ...(tool.description === undefined
        ? {}
        : {
            description: string(
              tool.description,
              `tools[${index}].description`,
            ),
          }),
      input_schema: schema,
      ...(tool.strict === undefined
        ? {}
        : { strict: boolean(tool.strict, `tools[${index}].strict`) }),
      ...(native.length > 0 ? { native } : {}),
    };
  });
  const toolChoice = decodeToolChoice(
    wire.tool_choice,
    losses,
    customTools,
    nativeTools,
  );
  const rawMessages = array(wire.messages, "messages").map((entry, index) => {
    const msg = object(entry, `messages[${index}]`);
    const role = string(msg.role, `messages[${index}].role`);
    return { msg, role, index };
  });

  for (let i = 0; i < rawMessages.length; i++) {
    const { role } = rawMessages[i]!;
    if (role === "system") {
      if (i === 0) {
        fail("messages[0]: system message cannot appear at messages[0]");
      }
      const prevRole = rawMessages[i - 1]!.role;
      if (prevRole !== "user" && prevRole !== "system") {
        fail(`messages[${i}]: system message must follow a user turn`);
      }
      let lastInRun = i;
      while (
        lastInRun + 1 < rawMessages.length &&
        rawMessages[lastInRun + 1]!.role === "system"
      ) {
        lastInRun++;
      }
      if (lastInRun + 1 < rawMessages.length) {
        const nextRole = rawMessages[lastInRun + 1]!.role;
        if (nextRole !== "assistant") {
          fail(
            `messages[${i}]: system message must be followed by an assistant turn or be the last message`,
          );
        }
      }
    } else if (role !== "user" && role !== "assistant") {
      fail(`messages[${i}]: unknown role ${role}`);
    }
  }

  const messages: Message[] = [];
  for (const { msg, role, index } of rawMessages) {
    if (role === "system") {
      if (msg.content === undefined || msg.content === null) {
        fail(`messages[${index}]: content is missing`);
      }
      if (typeof msg.content === "string") {
        system.push({ type: "text", text: msg.content });
      } else if (Array.isArray(msg.content)) {
        for (let j = 0; j < msg.content.length; j++) {
          const block = object(
            msg.content[j],
            `messages[${index}].content[${j}]`,
          );
          const blockType = string(
            block.type,
            `messages[${index}].content[${j}].type`,
          );
          if (blockType !== "text") {
            fail(`messages[${index}]: system content must be text`);
          }
          const text = string(
            block.text,
            `messages[${index}].content[${j}].text`,
          );
          const native = attachBlockCacheControl(
            block,
            `messages[${index}].content[${j}]`,
            losses,
          );
          system.push({
            type: "text",
            text,
            ...(native ? { native } : {}),
          });
        }
      } else {
        fail(`messages[${index}]: system content must be text`);
      }
      losses.push({
        path: `messages[${index}].role`,
        field: "role",
        reason: "degraded",
        detail:
          "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.",
      });
      continue;
    }

    const normalizedRole = role as "user" | "assistant";
    const content = decodeContent(
      msg.content,
      `messages[${index}].content`,
      losses,
      customTools,
      customCallIds,
      declaredTools,
      true,
    );
    if (content.length === 0) content.push({ type: "text", text: "" });
    if (normalizedRole === "user") {
      content.forEach((b, bIdx) => {
        if (b.type === "redacted_thinking") {
          fail(
            `messages[${index}].content[${bIdx}]: redacted_thinking is not permitted in user messages`,
          );
        }
        if (
          b.type === "text" &&
          (b as TextBlock).citations &&
          ((b as TextBlock).citations?.length ?? 0) > 0
        ) {
          fail(
            `messages[${index}].content[${bIdx}].citations: citations are not permitted on user text blocks`,
          );
        }
      });
    }
    messages.push({ role: normalizedRole, content });
  }
  let responseFormat: NonNullable<Request["params"]>["response_format"];
  if (wire.output_config !== undefined && wire.output_config !== null) {
    const config = object(wire.output_config, "output_config");
    if (config.format !== undefined) {
      const format = object(config.format, "output_config.format");
      const kind = string(format.type, "output_config.format.type");
      if (!kind) fail("output_config.format.type must be non-empty");
      if (kind === "json_schema") {
        responseFormat = {
          type: "json_schema",
          schema: object(format.schema, "output_config.format.schema"),
        };
      } else {
        losses.push(
          loss("output_config.format.type", "type", "unmapped-value"),
        );
      }
    }
  }
  const params = {
    ...(responseFormat === undefined
      ? {}
      : { response_format: responseFormat }),
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
  const baseRequest: Request = {
    model: mapModel(options.modelMapper, string(wire.model, "model")),
    ...(system.length === 0 ? {} : { system }),
    messages,
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    params,
    ...(metadata === undefined ? {} : { metadata }),
  };
  const request = decodeRequestNative(wire, baseRequest, losses);
  validateRequest(request);
  return { value: request, losses };
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const audit = auditWire(wire, ANTHROPIC_RESPONSE);
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
  const losses: Loss[] = [];
  const content = decodeContent(
    wire.content,
    "content",
    losses,
    undefined,
    undefined,
    undefined,
    false,
  );
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
        ...(usage.cache_read_input_tokens === undefined
          ? {}
          : {
              cache_read_input_tokens: parseUsageInteger(
                whole(
                  usage.cache_read_input_tokens,
                  "usage.cache_read_input_tokens",
                ),
                "usage.cache_read_input_tokens",
              ),
            }),
        ...(usage.cache_creation_input_tokens === undefined
          ? {}
          : {
              cache_creation_input_tokens: parseUsageInteger(
                whole(
                  usage.cache_creation_input_tokens,
                  "usage.cache_creation_input_tokens",
                ),
                "usage.cache_creation_input_tokens",
              ),
            }),
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
  const metadata = encodeAnthropicMetadata(request.metadata, losses);
  const maxTokens = request.params?.max_tokens ?? 4096n;
  if (request.params?.max_tokens === undefined)
    losses.push(loss("params.max_tokens", "max_tokens", "degraded"));
  const firstBlock = request.messages[0]?.content[0];
  const shorthand =
    (request.system === undefined || request.system.length === 0) &&
    request.messages.length === 1 &&
    request.messages[0]!.content.length === 1 &&
    firstBlock?.type === "text" &&
    (firstBlock.citations === undefined || firstBlock.citations.length === 0) &&
    (firstBlock.native === undefined || firstBlock.native.length === 0);
  const messages = request.messages.map((message, index) => {
    if (message.role === "user") {
      message.content.forEach((b, bIdx) => {
        if (b.type === "redacted_thinking") {
          fail(
            `messages[${index}].content[${bIdx}]: redacted_thinking is not permitted in user messages`,
          );
        }
        if (b.type === "text" && b.citations && b.citations.length > 0) {
          fail(
            `messages[${index}].content[${bIdx}].citations: citations are not permitted on user text blocks`,
          );
        }
      });
    }
    const encoded = shorthand
      ? (message.content[0] as { readonly text: string }).text
      : encodeContent(
          message.content,
          `messages[${index}].content`,
          losses,
          true,
          true,
        );
    return {
      role: message.role,
      content:
        Array.isArray(encoded) && encoded.length === 0
          ? [{ type: "text", text: "" }]
          : encoded,
    };
  });
  request.tools?.forEach((tool, i) => {
    tool.native?.forEach((ann, tAnnIdx) => {
      if (ann.source !== "anthropic" && ann.kind !== "defer_loading") {
        if (ann.omission) {
          losses.push({
            path: `tools[${i}].native[${tAnnIdx}]`,
            field: ann.omission.field,
            reason: ann.omission.reason,
            ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
          });
        } else {
          losses.push({
            path: `tools[${i}].native[${tAnnIdx}]`,
            field: "source",
            reason: "unsupported-semantic",
            detail: `unsupported native option source "${ann.source}"`,
          });
        }
      }
    });
  });

  const tools: JsonObject[] =
    request.tools?.flatMap<JsonObject>((tool, index) => {
      const cc = getBlockCacheControl(tool);
      const dl = getToolDeferLoading(tool);
      const es = getToolEagerInputStreaming(tool);
      if (cc && dl) {
        fail(
          `tools[${index}]: cannot specify both defer_loading and cache_control`,
        );
      }
      if (tool.type === "custom") {
        if (tool.format?.type === "grammar") {
          losses.push(
            loss(`tools[${index}].format`, "format", "unsupported-semantic"),
          );
        }
        return [
          {
            name: tool.name,
            ...(tool.description === undefined
              ? {}
              : { description: tool.description }),
            input_schema: {
              type: "object",
              properties: { input: { type: "string" } },
              required: ["input"],
            },
            ...(cc ? { cache_control: cc } : {}),
            ...(dl ? { defer_loading: true } : {}),
            ...(es !== undefined ? { eager_input_streaming: es } : {}),
          },
        ];
      }
      if (tool.type === "native") {
        if (tool.source !== "anthropic") {
          losses.push({
            path: `tools[${index}]`,
            field: "type",
            reason: "unsupported-semantic",
            detail: `native tool "${tool.name}" from "${tool.source}" cannot be represented by the target protocol`,
          });
          return [];
        }
        const parsed = parseJson(tool.definition);
        if (
          parsed === null ||
          typeof parsed !== "object" ||
          Array.isArray(parsed)
        ) {
          fail("native tool definition must be a JSON object");
        }
        const declaration = parsed as JsonObject;
        if (
          typeof declaration.type !== "string" ||
          declaration.type === "" ||
          declaration.type === "function" ||
          declaration.type === "custom"
        ) {
          fail("native tool definition requires a native type");
        }
        if (declaration.name !== tool.name) {
          fail("native tool definition name must match tool name");
        }
        return [declaration];
      }
      if (tool.type === "apply_patch") {
        losses.push(loss(`tools[${index}]`, "type", "unsupported-semantic"));
        return [];
      }
      return [
        {
          name: tool.name,
          ...(tool.description === undefined
            ? {}
            : { description: tool.description }),
          input_schema: tool.input_schema,
          ...(tool.strict === undefined ? {} : { strict: tool.strict }),
          ...(cc ? { cache_control: cc } : {}),
          ...(dl ? { defer_loading: true } : {}),
          ...(es !== undefined ? { eager_input_streaming: es } : {}),
        },
      ];
    }) ?? [];
  const allToolsDropped =
    (request.tools?.length ?? 0) > 0 && tools.length === 0;
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
    !tools.some((t) => t.name === choiceName);
  const toolChoice =
    request.tool_choice?.mode === "any" && allToolsDropped
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
  const outValue: JsonObject = {
    model: mapModel(options.modelMapper, request.model),
    max_tokens: integer(maxTokens),
    ...(metadata === undefined ? {} : { metadata }),
    ...(request.system === undefined || request.system.length === 0
      ? {}
      : {
          system: request.system.map((block, index) => {
            if (block.role === "developer")
              losses.push({
                path: `system[${index}].role`,
                field: "role",
                reason: "degraded",
                detail:
                  "developer instruction is carried as Anthropic system content; Anthropic has no developer role",
              });
            const cc = getBlockCacheControl(block);
            return {
              type: "text",
              text: block.text,
              ...(cc ? { cache_control: cc } : {}),
            };
          }),
        }),
    messages,
    ...(params?.temperature === undefined
      ? {}
      : { temperature: fromValue(params.temperature) }),
    ...(params?.top_p === undefined ? {} : { top_p: fromValue(params.top_p) }),
    ...(params?.stop_sequences === undefined ||
    params.stop_sequences.length === 0
      ? {}
      : { stop_sequences: [...params.stop_sequences] }),
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
  };
  encodeRequestNative(request, outValue, losses);
  if (request.params?.verbosity !== undefined)
    losses.push(loss("params.verbosity", "verbosity", "unmapped-field"));
  const format = request.params?.response_format;
  let outputConfig: JsonObject | undefined;
  if (format?.type === "json_schema") {
    assertSourceMatches(format.schema);
    const config =
      outValue.output_config === undefined
        ? {}
        : object(outValue.output_config, "output_config");
    outputConfig = {
      ...config,
      format: { type: "json_schema", schema: format.schema },
    };
    if (format.name)
      losses.push(
        loss("params.response_format.name", "name", "unmapped-field"),
      );
    if (format.description)
      losses.push(
        loss(
          "params.response_format.description",
          "description",
          "unmapped-field",
        ),
      );
    if (format.strict !== undefined)
      losses.push(
        loss("params.response_format.strict", "strict", "unmapped-field"),
      );
  } else if (format !== undefined) {
    losses.push(
      loss("params.response_format", "response_format", "unmapped-field"),
    );
  }
  return {
    value:
      outputConfig === undefined
        ? outValue
        : { ...outValue, output_config: outputConfig },
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
  const stopReason =
    response.stop_reason === "tool_use" &&
    !response.content.some(
      (block) => block.type === "tool_use" || block.type === "custom_tool_use",
    )
      ? (losses.push(loss("stop_reason", "stop_reason", "degraded")),
        "end_turn" as const)
      : response.stop_reason;
  return {
    value: {
      id: response.id,
      type: "message",
      role: "assistant",
      model: mapModel(options.modelMapper, response.model),
      content: (() => {
        const encoded = encodeContent(
          response.content,
          "content",
          losses,
          false,
          false,
        );
        return encoded.length === 0 ? [{ type: "text", text: "" }] : encoded;
      })(),
      stop_reason: stopReason,
      ...(stopReason === "stop_sequence" && response.stop_sequence !== undefined
        ? { stop_sequence: response.stop_sequence }
        : {}),
      usage: {
        input_tokens: integer(response.usage.input_tokens),
        output_tokens: integer(response.usage.output_tokens),
        ...(response.usage.cache_read_input_tokens === undefined
          ? {}
          : {
              cache_read_input_tokens: integer(
                response.usage.cache_read_input_tokens,
              ),
            }),
        ...(response.usage.cache_creation_input_tokens === undefined
          ? {}
          : {
              cache_creation_input_tokens: integer(
                response.usage.cache_creation_input_tokens,
              ),
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
  customTools?: ReadonlySet<string>,
  customCallIds?: Set<string>,
  declaredTools?: ReadonlySet<string>,
  isRequest: boolean = false,
): Block[] {
  if (typeof value === "string") return [{ type: "text", text: value }];
  const blocks: Block[] = [];
  for (let index = 0; index < array(value, path).length; index += 1) {
    const block = object(array(value, path)[index], `${path}[${index}]`);
    const type = string(block.type, `${path}[${index}].type`);
    const lossesBeforeBlock = losses.length;
    const native = isRequest
      ? attachBlockCacheControl(block, `${path}[${index}]`, losses)
      : undefined;
    if (type === "text") {
      let cites: TextCitation[] | undefined;
      if (block.citations !== undefined) {
        cites = decodeAnthropicTextCitations(
          block.citations,
          `${path}[${index}].citations`,
          losses,
          isRequest,
        );
      }
      blocks.push({
        type: "text",
        text: string(block.text, `${path}[${index}].text`),
        ...(cites && cites.length > 0 ? { citations: cites } : {}),
        ...(native ? { native } : {}),
      });
    } else if (type === "redacted_thinking") {
      if (block.data === undefined || typeof block.data !== "string") {
        fail(`${path}[${index}].data is required`);
      }
      blocks.push({
        type: "redacted_thinking",
        data: block.data,
        ...(native ? { native } : {}),
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
          ...(native ? { native } : {}),
        });
      else if (source.type === "url")
        blocks.push({
          type: "image",
          url: string(source.url, `${path}[${index}].source.url`),
          ...(native ? { native } : {}),
        });
      else {
        losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
        continue;
      }
    } else if (type === "document") {
      if (!isRequest) {
        losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
        continue;
      }
      const source = object(block.source, `${path}[${index}].source`);
      const sourcePath = `${path}[${index}].source`;
      if (source.type !== undefined && typeof source.type !== "string") {
        fail(`${sourcePath}.type must be a string`);
      }
      if (source.type === undefined || source.type === "") {
        fail(`${sourcePath}.type is required`);
      }
      if (
        (source.type === "base64" &&
          (source.url !== undefined || source.file_id !== undefined)) ||
        (source.type === "url" &&
          (source.media_type !== undefined ||
            source.data !== undefined ||
            source.file_id !== undefined)) ||
        (source.type === "file" &&
          (source.media_type !== undefined ||
            source.data !== undefined ||
            source.url !== undefined))
      ) {
        fail(`${sourcePath} has conflicting source fields`);
      }
      let doc: DocumentBlock;
      if (source.type === "base64") {
        doc = {
          type: "document",
          media_type: nonEmptyString(
            source.media_type,
            `${path}[${index}].source.media_type`,
          ),
          data: nonEmptyString(source.data, `${path}[${index}].source.data`),
        };
      } else if (source.type === "url") {
        doc = {
          type: "document",
          url: nonEmptyString(source.url, `${path}[${index}].source.url`),
        };
      } else if (source.type === "file") {
        doc = {
          type: "document",
          file_id: nonEmptyString(
            source.file_id,
            `${path}[${index}].source.file_id`,
          ),
        };
      } else {
        losses.length = lossesBeforeBlock;
        losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
        continue;
      }
      const docNative: NativeAnnotation[] = [];
      if (block.title !== undefined) {
        docNative.push({
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: "document_title",
          value: JSON.stringify(string(block.title, `${path}[${index}].title`)),
          omission: {
            field: "title",
            reason: "unmapped-field" as const,
            detail: DOCUMENT_TITLE_LOSS_DETAIL,
          },
        });
      }
      if (block.context !== undefined) {
        docNative.push({
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: "document_context",
          value: JSON.stringify(
            string(block.context, `${path}[${index}].context`),
          ),
          omission: {
            field: "context",
            reason: "unmapped-field" as const,
            detail: DOCUMENT_CONTEXT_LOSS_DETAIL,
          },
        });
      }
      if (block.citations !== undefined) {
        const citations = object(
          block.citations,
          `${path}[${index}].citations`,
        );
        if (
          citations.enabled !== undefined &&
          typeof citations.enabled !== "boolean"
        ) {
          fail(`${path}[${index}].citations.enabled must be a boolean`);
        }
        const canonCitations: Record<string, JsonValue> = {};
        for (const k of Object.keys(citations).sort()) {
          canonCitations[k] = citations[k]!;
        }
        docNative.push({
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: "document_citations",
          value: stringifyJson(canonCitations),
          omission: {
            field: "citations",
            reason: "unsupported-semantic" as const,
            detail: DOCUMENT_CITATIONS_LOSS_DETAIL,
          },
        });
      }
      const mergedNative =
        docNative.length > 0 ? [...docNative, ...(native ?? [])] : native;
      blocks.push(mergedNative ? { ...doc, native: mergedNative } : doc);
    } else if (type === "thinking") {
      blocks.push({
        type: "thinking",
        thinking: string(block.thinking, `${path}[${index}].thinking`),
        ...(block.signature === undefined
          ? {}
          : {
              signature: nonEmptyString(
                block.signature,
                `${path}[${index}].signature`,
              ),
            }),
        ...(native ? { native } : {}),
      });
    } else if (type === "tool_use") {
      const inputPath = `${path}[${index}].input`;
      const input = object(block.input, inputPath);
      const id = string(block.id, `${path}[${index}].id`);
      const name = string(block.name, `${path}[${index}].name`);
      const customInput = extractCustomInput(input);
      if (customTools?.has(name)) {
        customCallIds?.add(id);
        blocks.push({
          type: "custom_tool_use",
          id,
          name,
          input: customInput ?? "",
          ...(native ? { native } : {}),
        });
      } else if (declaredTools?.has(name)) {
        const inputText =
          block.inputText === undefined
            ? (sourceTextOf(input) ?? jsonText(stringifyJson(input)))
            : toolInputText(block.inputText, `${path}[${index}].inputText`);
        blocks.push({
          type: "tool_use",
          id,
          name,
          input: inputText,
          ...(native ? { native } : {}),
        });
      } else if (customInput !== undefined) {
        customCallIds?.add(id);
        blocks.push({
          type: "custom_tool_use",
          id,
          name,
          input: customInput,
          ...(native ? { native } : {}),
        });
      } else {
        const inputText =
          block.inputText === undefined
            ? (sourceTextOf(input) ?? jsonText(stringifyJson(input)))
            : toolInputText(block.inputText, `${path}[${index}].inputText`);
        blocks.push({
          type: "tool_use",
          id,
          name,
          input: inputText,
          ...(native ? { native } : {}),
        });
      }
    } else if (type === "tool_result") {
      const toolUseId = string(
        block.tool_use_id,
        `${path}[${index}].tool_use_id`,
      );
      if (customCallIds?.has(toolUseId)) {
        const decoded =
          typeof block.content === "string"
            ? block.content
            : decodeContent(
                block.content,
                `${path}[${index}].content`,
                losses,
                customTools,
                customCallIds,
                declaredTools,
                isRequest,
              );
        if (Array.isArray(decoded)) {
          decoded.forEach((cb, cIdx) => {
            if (cb.type === "redacted_thinking") {
              fail(
                `${path}[${index}].content[${cIdx}]: redacted_thinking is not permitted in tool_result`,
              );
            }
            if (
              cb.type === "text" &&
              (cb as TextBlock).citations &&
              ((cb as TextBlock).citations?.length ?? 0) > 0
            ) {
              fail(
                `${path}[${index}].content[${cIdx}].citations: citations are not permitted in tool_result`,
              );
            }
          });
        }
        blocks.push({
          type: "custom_tool_result",
          tool_use_id: toolUseId,
          output:
            typeof decoded === "string"
              ? decoded
              : (decoded.filter(
                  (b): b is TextBlock | ImageBlock =>
                    b.type === "text" || b.type === "image",
                ) as readonly (TextBlock | ImageBlock)[]),
          ...(native ? { native } : {}),
        });
      } else {
        const decoded = decodeContent(
          block.content,
          `${path}[${index}].content`,
          losses,
          customTools,
          customCallIds,
          declaredTools,
          isRequest,
        );
        decoded.forEach((cb, cIdx) => {
          if (cb.type === "redacted_thinking") {
            fail(
              `${path}[${index}].content[${cIdx}]: redacted_thinking is not permitted in tool_result`,
            );
          }
          if (
            cb.type === "text" &&
            (cb as TextBlock).citations &&
            ((cb as TextBlock).citations?.length ?? 0) > 0
          ) {
            fail(
              `${path}[${index}].content[${cIdx}].citations: citations are not permitted in tool_result`,
            );
          }
        });
        blocks.push({
          type: "tool_result",
          tool_use_id: toolUseId,
          content: decoded,
          ...(block.is_error === undefined
            ? {}
            : {
                is_error: boolean(block.is_error, `${path}[${index}].is_error`),
              }),
          ...(native ? { native } : {}),
        });
      }
    } else {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
      continue;
    }
  }
  return blocks;
}

function encodeContent(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
  replay: boolean,
  isRequest: boolean,
): JsonValue[] {
  const output: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    const cc = getBlockCacheControl(block);
    if (block.type === "text") {
      let rawCitations: JsonValue[] | undefined;
      if (block.citations && block.citations.length > 0) {
        let hasOpenAI = false;
        const validAnthropic = block.citations.filter((c) => {
          if (c.source === "openai") {
            hasOpenAI = true;
            return false;
          }
          return true;
        });
        if (hasOpenAI) {
          losses.push({
            path: `${path}[${index}].citations`,
            field: "citations",
            reason: "unsupported-semantic",
            detail:
              "openai text annotations; the target face cannot preserve source locations and replay metadata",
          });
        }
        if (validAnthropic.length > 0) {
          if (isRequest) {
            rawCitations = validAnthropic.map((c, cIdx) => {
              if (c.source !== "anthropic") {
                throw new OxaError(
                  "invalid-input",
                  `unknown citation source: ${c.source}`,
                );
              }
              validateCitationValue(c.value);
              const parsed = { ...(parseJson(c.value) as JsonObject) };
              if (parsed.file_id !== undefined && parsed.file_id !== null) {
                losses.push({
                  path: `${path}[${index}].citations[${cIdx}].file_id`,
                  field: "file_id",
                  reason: "unmapped-field",
                  detail:
                    "response citation file_id has no request-side representation",
                });
              }
              delete (parsed as Record<string, JsonValue>).file_id;
              return parsed;
            });
          } else {
            rawCitations = validAnthropic.map((c) => {
              if (c.source !== "anthropic") {
                throw new OxaError(
                  "invalid-input",
                  `unknown citation source: ${c.source}`,
                );
              }
              validateCitationValue(c.value);
              return parseJson(c.value);
            });
          }
        }
      }
      output.push({
        type: "text",
        text: block.text,
        ...(rawCitations && rawCitations.length > 0
          ? { citations: rawCitations }
          : {}),
        ...(cc ? { cache_control: cc } : {}),
      });
    } else if (block.type === "redacted_thinking") {
      output.push({
        type: "redacted_thinking",
        data: block.data,
        ...(cc ? { cache_control: cc } : {}),
      });
    } else if (block.type === "thinking") {
      output.push({
        type: "thinking",
        thinking: block.thinking,
        ...(block.signature === undefined
          ? {}
          : { signature: block.signature }),
        ...(cc ? { cache_control: cc } : {}),
      });
      if (replay && block.signature === undefined)
        losses.push({
          path: `${path}[${index}]`,
          field: "signature",
          reason: "degraded",
          detail: "unsigned thinking block; Anthropic may reject on replay",
        });
    } else if (block.type === "image") {
      const image = encodeImage(block);
      if (image === undefined)
        losses.push(loss(`${path}[${index}]`, "image", "unsupported-semantic"));
      else output.push({ ...image, ...(cc ? { cache_control: cc } : {}) });
    } else if (block.type === "document") {
      if (!isRequest) {
        losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
        continue;
      }
      const doc = encodeDocument(block as DocumentBlock);
      if (doc === undefined)
        losses.push(
          loss(`${path}[${index}]`, "document", "unsupported-semantic"),
        );
      else output.push({ ...doc, ...(cc ? { cache_control: cc } : {}) });
    } else if (block.type === "tool_use") {
      const value = parseJson(block.input);
      const inputPath = `${path}[${index}].input`;
      output.push({
        type: "tool_use",
        id: block.id,
        name: block.name,
        input: withSourceText(object(value, inputPath), block.input),
        ...(cc ? { cache_control: cc } : {}),
      });
    } else if (block.type === "tool_result") {
      const content: JsonValue[] = [];
      for (let child = 0; child < block.content.length; child += 1) {
        const nested = block.content[child]!;
        const nestedCc = getBlockCacheControl(nested);
        if (nested.type === "redacted_thinking") {
          fail(
            `${path}[${index}].content[${child}]: redacted_thinking is not permitted in tool_result`,
          );
        }
        if (nested.type === "text") {
          if (nested.citations && nested.citations.length > 0) {
            fail(
              `${path}[${index}].content[${child}].citations: citations are not permitted in tool_result`,
            );
          }
          content.push({
            type: "text",
            text: nested.text,
            ...(nestedCc ? { cache_control: nestedCc } : {}),
          });
        } else if (nested.type === "image") {
          const image = encodeImage(nested);
          if (image === undefined)
            losses.push(
              loss(
                `${path}[${index}].content[${child}]`,
                "image",
                "unsupported-semantic",
              ),
            );
          else
            content.push({
              ...image,
              ...(nestedCc ? { cache_control: nestedCc } : {}),
            });
        } else if (nested.type === "document") {
          const doc = encodeDocument(nested as DocumentBlock);
          if (doc === undefined)
            losses.push(
              loss(
                `${path}[${index}].content[${child}]`,
                "document",
                "unsupported-semantic",
              ),
            );
          else
            content.push({
              ...doc,
              ...(nestedCc ? { cache_control: nestedCc } : {}),
            });
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
        ...(cc ? { cache_control: cc } : {}),
      });
    } else if (block.type === "custom_tool_use") {
      output.push({
        type: "tool_use",
        id: block.id,
        name: block.name,
        input: { input: block.input },
      });
    } else if (block.type === "custom_tool_result") {
      if (typeof block.output === "string") {
        output.push({
          type: "tool_result",
          tool_use_id: block.tool_use_id,
          content: block.output,
          ...(cc ? { cache_control: cc } : {}),
        });
      } else {
        const content: JsonValue[] = [];
        for (let child = 0; child < block.output.length; child += 1) {
          const nested = block.output[child]!;
          const nestedCc = getBlockCacheControl(nested);
          if (nested.type === "text")
            content.push({
              type: "text",
              text: nested.text,
              ...(nestedCc ? { cache_control: nestedCc } : {}),
            });
          else if (nested.type === "image") {
            const image = encodeImage(nested);
            if (image !== undefined)
              content.push({
                ...image,
                ...(nestedCc ? { cache_control: nestedCc } : {}),
              });
          }
        }
        output.push({
          type: "tool_result",
          tool_use_id: block.tool_use_id,
          content,
          ...(cc ? { cache_control: cc } : {}),
        });
      }
    } else if (
      block.type === "apply_patch_tool_use" ||
      block.type === "apply_patch_tool_result" ||
      block.type === "web_search_call"
    ) {
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
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

function encodeDocument(doc: DocumentBlock): JsonObject | undefined {
  let source: JsonObject;
  const hasData = typeof doc.data === "string" && doc.data.length > 0;
  const hasUrl = typeof doc.url === "string" && doc.url.length > 0;
  const hasFile = typeof doc.file_id === "string" && doc.file_id.length > 0;
  const sourceCount = (hasData ? 1 : 0) + (hasUrl ? 1 : 0) + (hasFile ? 1 : 0);
  if (sourceCount !== 1) return undefined;
  if (hasData) {
    if (typeof doc.media_type !== "string" || doc.media_type.length === 0)
      return undefined;
    source = { type: "base64", media_type: doc.media_type, data: doc.data };
  } else if (hasUrl) {
    if (doc.media_type !== undefined && doc.media_type.length > 0)
      return undefined;
    source = { type: "url", url: doc.url };
  } else {
    if (doc.media_type !== undefined && doc.media_type.length > 0)
      return undefined;
    source = { type: "file", file_id: doc.file_id as string };
  }
  let out: JsonObject = { type: "document", source };
  for (const n of doc.native ?? []) {
    if (n.source !== NATIVE_SOURCE_ANTHROPIC) continue;
    try {
      const parsed = JSON.parse(n.value) as unknown;
      if (n.kind === "document_title" && typeof parsed === "string") {
        out = { ...out, title: parsed };
      } else if (n.kind === "document_context" && typeof parsed === "string") {
        out = { ...out, context: parsed };
      } else if (
        n.kind === "document_citations" &&
        typeof parsed === "object" &&
        parsed !== null &&
        !Array.isArray(parsed)
      ) {
        out = { ...out, citations: parsed as JsonValue };
      }
    } catch {
      // malformed annotation values are unreachable for validated IR
    }
  }
  return out;
}

function decodeToolChoice(
  value: JsonValue | undefined,
  losses: Loss[],
  customTools?: ReadonlySet<string>,
  nativeTools?: ReadonlySet<string>,
): ToolChoice | undefined {
  if (value === undefined || value === null) return undefined;
  const choice = object(value, "tool_choice");
  const type = string(choice.type, "tool_choice.type");
  if (choice.disable_parallel_tool_use !== undefined) {
    if (typeof choice.disable_parallel_tool_use !== "boolean") {
      fail("tool_choice.disable_parallel_tool_use must be a boolean");
    }
    if (type === "none") {
      fail(
        "anthropic: cannot specify disable_parallel_tool_use with tool_choice type none",
      );
    }
  }
  if (type === "auto" || type === "any" || type === "none")
    return { mode: type };
  if (type === "tool") {
    const name = string(choice.name, "tool_choice.name");
    return {
      mode: "tool",
      name,
      ...(nativeTools?.has(name)
        ? { tool_type: "native" as const }
        : customTools?.has(name)
          ? { tool_type: "custom" as const }
          : {}),
    };
  }
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function encodeToolChoice(
  value: ToolChoice | undefined,
  losses: Loss[],
): JsonObject | undefined {
  if (value === undefined) return undefined;
  if (value.mode === "tool") {
    if (value.tool_type === "custom" || value.tool_type === "native") {
      if (value.name === "") fail("tool_choice.name must not be empty");
      return { type: "tool", name: value.name };
    }
    if (value.tool_type === "apply_patch") {
      losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
      return undefined;
    }
    if (value.name === "") fail("tool_choice.name must not be empty");
    return { type: "tool", name: value.name };
  }
  return { type: value.mode };
}

function anthropicMetadata(
  value: JsonValue | undefined,
  losses: Loss[],
): Record<string, string> | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "object" || Array.isArray(value) || isJsonNumber(value))
    fail("anthropic: metadata must be an object");
  const out: Record<string, string> = Object.create(null);
  for (const [key, child] of Object.entries(value)) {
    if (key.length === 0) fail("anthropic: metadata key cannot be empty");
    if (key === "user_id") {
      if (typeof child !== "string")
        fail("anthropic: metadata.user_id must be a string");
      out.user_id = child;
      continue;
    }
    losses.push({
      path: `metadata.${key}`,
      field: key,
      reason: "unmapped-field",
      detail:
        "Anthropic request metadata only preserves user_id; this key is dropped.",
    });
  }
  return out.user_id !== undefined ? { user_id: out.user_id } : undefined;
}

function encodeAnthropicMetadata(
  metadata: Readonly<Record<string, string>> | undefined,
  losses: Loss[],
): Record<string, string> | undefined {
  if (metadata === undefined) return undefined;
  let userID: string | undefined;
  for (const key of Object.keys(metadata).sort()) {
    if (key.length === 0) fail("anthropic: metadata key cannot be empty");
    if (key === "user_id") {
      userID = metadata[key];
      continue;
    }
    losses.push({
      path: `metadata.${key}`,
      field: key,
      reason: "unmapped-field",
      detail:
        "Anthropic request metadata only preserves user_id; this IR metadata key is dropped.",
    });
  }
  return userID === undefined ? undefined : { user_id: userID };
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
  return value as JsonObject;
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

function objectOrThrow(value: JsonValue, name: string): JsonObject {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    isJsonNumber(value)
  )
    fail(`${name} must be an object`);
  return value as JsonObject;
}

export function decodeAnthropicTextCitations(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
  isRequest: boolean,
): TextCitation[] | undefined {
  if (value === undefined || value === null) return undefined;
  if (!Array.isArray(value)) {
    fail(`${path} must be an array`);
  }
  if (value.length === 0) return undefined;
  const cites: TextCitation[] = [];
  for (let i = 0; i < value.length; i++) {
    const itemPath = `${path}[${i}]`;
    const obj = object(value[i], itemPath);
    const rawType = obj.type;
    if (rawType === undefined || rawType === null) {
      fail(`${itemPath}.type is required`);
    }
    if (typeof rawType !== "string") {
      fail(`${itemPath}.type must be a string`);
    }

    const cleanObj: Record<string, any> = { type: rawType };
    switch (rawType) {
      case "char_location": {
        const text = string(obj.cited_text, `${itemPath}.cited_text`);
        const docIdx = integerNumber(
          obj.document_index,
          `${itemPath}.document_index`,
        );
        if (docIdx < 0) fail(`${itemPath}.document_index must be non-negative`);
        const title = nullableString(
          obj.document_title,
          `${itemPath}.document_title`,
        );
        const start = integerNumber(
          obj.start_char_index,
          `${itemPath}.start_char_index`,
        );
        if (start < 0)
          fail(`${itemPath}.start_char_index must be non-negative`);
        const end = integerNumber(
          obj.end_char_index,
          `${itemPath}.end_char_index`,
        );
        if (end <= start)
          fail(
            `${itemPath}.end_char_index must be greater than start_char_index`,
          );
        let fid: string | null | undefined;
        if (obj.file_id !== undefined) {
          fid = nullableString(obj.file_id, `${itemPath}.file_id`);
        }
        cleanObj.cited_text = text;
        cleanObj.document_index = docIdx;
        cleanObj.document_title = title;
        cleanObj.start_char_index = start;
        cleanObj.end_char_index = end;
        if (fid !== undefined) cleanObj.file_id = fid;
        break;
      }
      case "page_location": {
        const text = string(obj.cited_text, `${itemPath}.cited_text`);
        const docIdx = integerNumber(
          obj.document_index,
          `${itemPath}.document_index`,
        );
        if (docIdx < 0) fail(`${itemPath}.document_index must be non-negative`);
        const title = nullableString(
          obj.document_title,
          `${itemPath}.document_title`,
        );
        const start = integerNumber(
          obj.start_page_number,
          `${itemPath}.start_page_number`,
        );
        if (start < 1) fail(`${itemPath}.start_page_number must be at least 1`);
        const end = integerNumber(
          obj.end_page_number,
          `${itemPath}.end_page_number`,
        );
        if (end < start)
          fail(`${itemPath}.end_page_number must be >= start_page_number`);
        let fid: string | null | undefined;
        if (obj.file_id !== undefined) {
          fid = nullableString(obj.file_id, `${itemPath}.file_id`);
        }
        cleanObj.cited_text = text;
        cleanObj.document_index = docIdx;
        cleanObj.document_title = title;
        cleanObj.start_page_number = start;
        cleanObj.end_page_number = end;
        if (fid !== undefined) cleanObj.file_id = fid;
        break;
      }
      case "content_block_location": {
        const text = string(obj.cited_text, `${itemPath}.cited_text`);
        const docIdx = integerNumber(
          obj.document_index,
          `${itemPath}.document_index`,
        );
        if (docIdx < 0) fail(`${itemPath}.document_index must be non-negative`);
        const title = nullableString(
          obj.document_title,
          `${itemPath}.document_title`,
        );
        const start = integerNumber(
          obj.start_block_index,
          `${itemPath}.start_block_index`,
        );
        if (start < 0)
          fail(`${itemPath}.start_block_index must be non-negative`);
        const end = integerNumber(
          obj.end_block_index,
          `${itemPath}.end_block_index`,
        );
        if (end <= start)
          fail(
            `${itemPath}.end_block_index must be greater than start_block_index`,
          );
        let fid: string | null | undefined;
        if (obj.file_id !== undefined) {
          fid = nullableString(obj.file_id, `${itemPath}.file_id`);
        }
        cleanObj.cited_text = text;
        cleanObj.document_index = docIdx;
        cleanObj.document_title = title;
        cleanObj.start_block_index = start;
        cleanObj.end_block_index = end;
        if (fid !== undefined) cleanObj.file_id = fid;
        break;
      }
      case "web_search_result_location": {
        cleanObj.cited_text = string(obj.cited_text, `${itemPath}.cited_text`);
        cleanObj.encrypted_index = string(
          obj.encrypted_index,
          `${itemPath}.encrypted_index`,
        );
        cleanObj.title = nullableString(obj.title, `${itemPath}.title`);
        cleanObj.url = string(obj.url, `${itemPath}.url`);
        break;
      }
      case "search_result_location": {
        cleanObj.cited_text = string(obj.cited_text, `${itemPath}.cited_text`);
        const sIdx = integerNumber(
          obj.search_result_index,
          `${itemPath}.search_result_index`,
        );
        if (sIdx < 0)
          fail(`${itemPath}.search_result_index must be non-negative`);
        cleanObj.search_result_index = sIdx;
        cleanObj.source = string(obj.source, `${itemPath}.source`);
        cleanObj.title = nullableString(obj.title, `${itemPath}.title`);
        const start = integerNumber(
          obj.start_block_index,
          `${itemPath}.start_block_index`,
        );
        if (start < 0)
          fail(`${itemPath}.start_block_index must be non-negative`);
        const end = integerNumber(
          obj.end_block_index,
          `${itemPath}.end_block_index`,
        );
        if (end <= start)
          fail(
            `${itemPath}.end_block_index must be greater than start_block_index`,
          );
        cleanObj.start_block_index = start;
        cleanObj.end_block_index = end;
        break;
      }
      default:
        losses.push({
          path: itemPath,
          field: "type",
          reason: "unsupported-semantic",
          detail: `unsupported citation type "${rawType}"`,
        });
        continue;
    }

    const extraKeys = Object.keys(obj)
      .filter((k) => !(k in cleanObj))
      .sort();
    for (const k of extraKeys) {
      losses.push({
        path: `${itemPath}.${k}`,
        field: k,
        reason: "unmapped-field",
      });
    }

    const sortedKeys = Object.keys(cleanObj).sort();
    const sortedObj: Record<string, any> = {};
    for (const k of sortedKeys) {
      sortedObj[k] = cleanObj[k];
    }

    cites.push({
      source: "anthropic",
      value: JSON.stringify(sortedObj),
    });
  }
  return cites.length > 0 ? cites : undefined;
}

function nullableString(
  value: JsonValue | undefined,
  path: string,
): string | null {
  if (value === undefined) fail(`${path} is required`);
  if (value === null) return null;
  if (typeof value !== "string") fail(`${path} must be a string or null`);
  return value;
}

function integerNumber(value: JsonValue | undefined, path: string): number {
  if (value === undefined || value === null) fail(`${path} is required`);
  if (typeof value === "number") {
    if (!Number.isInteger(value)) fail(`${path} must be an integer`);
    return value;
  }
  if (isJsonNumber(value)) {
    if (!value.isInteger) fail(`${path} must be an integer`);
    return Number(value.token);
  }
  fail(`${path} must be an integer`);
}
