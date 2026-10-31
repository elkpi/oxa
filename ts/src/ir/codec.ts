import { OxaError } from "../error.js";
import {
  integer,
  isJsonArray,
  isJsonNumber,
  type JsonObject,
  type JsonText,
  type JsonValue,
} from "../json/index.js";
import { assertEventSequence } from "./checker.js";
import { validateCitation, validateCitationValue } from "./nonstream.js";
import {
  specVersion,
  type ApplyPatchOperation,
  type Block,
  type TextCitation,
  type Delta,
  type Event,
  type EventStream,
  type MessageDelta,
  type MessageStart,
  type Usage,
} from "./types.js";

export function encodeEventStream(stream: EventStream): JsonObject {
  assertEventSequence(stream.events);
  return { specVersion, events: stream.events.map(encodeEvent) };
}

export function decodeEventStream(document: JsonValue): EventStream {
  const root = object(document, "event stream");
  if (!supportedSpecVersion(root.specVersion)) fail("unsupported specVersion");
  if (!isJsonArray(root.events)) fail("events must be an array");
  const events = root.events.map(decodeEvent);
  assertEventSequence(events);
  return { events };
}

function encodeEvent(event: Event): JsonObject {
  switch (event.type) {
    case "message_start":
      return { type: event.type, id: event.id, model: event.model };
    case "content_block_start":
      return {
        type: event.type,
        index: integer(BigInt(event.index)),
        block: encodeBlock(event.block),
      };
    case "content_block_delta":
      return {
        type: event.type,
        index: integer(BigInt(event.index)),
        delta: encodeDelta(event.delta),
      };
    case "content_block_stop":
      return { type: event.type, index: integer(BigInt(event.index)) };
    case "message_delta":
      return {
        type: event.type,
        stop_reason: event.stop_reason,
        ...(event.stop_sequence === undefined
          ? {}
          : { stop_sequence: event.stop_sequence }),
        usage: encodeUsage(event.usage),
      };
    case "message_done":
      return { type: event.type };
  }
}

function decodeEvent(value: JsonValue): Event {
  const event = object(value, "event");
  const type = string(event.type, "event.type");
  switch (type) {
    case "message_start":
      return {
        type,
        id: string(event.id, "event.id"),
        model: string(event.model, "event.model"),
      } satisfies MessageStart;
    case "content_block_start":
      return {
        type,
        index: index(event.index),
        block: decodeBlock(event.block),
      };
    case "content_block_delta":
      return {
        type,
        index: index(event.index),
        delta: decodeDelta(event.delta),
      };
    case "content_block_stop":
      return { type, index: index(event.index) };
    case "message_delta":
      return {
        type,
        stop_reason: string(
          event.stop_reason,
          "event.stop_reason",
        ) as MessageDelta["stop_reason"],
        ...(event.stop_sequence === undefined
          ? {}
          : {
              stop_sequence: string(event.stop_sequence, "event.stop_sequence"),
            }),
        usage: decodeUsage(event.usage),
      } satisfies MessageDelta;
    case "message_done":
      return { type };
    default:
      fail(`unsupported event type ${type}`);
  }
}

function encodeBlock(block: Block): JsonObject {
  if (block.type === "apply_patch_tool_use")
    return {
      type: block.type,
      id: block.id,
      status: block.status,
      operation: encodeApplyPatchOperation(block.operation),
    };
  if (block.type === "apply_patch_tool_result")
    return {
      type: block.type,
      tool_use_id: block.tool_use_id,
      status: block.status,
      ...(block.output === undefined ? {} : { output: block.output }),
    };
  if (block.type === "custom_tool_use")
    return {
      type: block.type,
      id: block.id,
      name: block.name,
      input: block.input,
    };
  if (block.type === "text")
    return {
      type: "text",
      text: block.text,
      ...(block.citations && block.citations.length > 0
        ? {
            citations: block.citations.map((c) => ({
              source: c.source,
              value: c.value,
            })),
          }
        : {}),
    };
  if (block.type === "thinking")
    return {
      type: "thinking",
      thinking: block.thinking,
      ...(block.signature === undefined ? {} : { signature: block.signature }),
    };
  if (block.type === "redacted_thinking")
    return {
      type: "redacted_thinking",
      data: block.data,
    };
  if (block.type === "tool_use")
    return {
      type: "tool_use",
      id: block.id,
      name: block.name,
      input: block.input,
    };
  if (block.type === "refusal")
    return {
      type: "refusal",
      refusal: block.refusal,
      ...(block.category === undefined ? {} : { category: block.category }),
    };
  throw new OxaError(
    "ir-invariant",
    `codec does not yet encode block ${block.type}`,
  );
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

function decodeBlock(value: JsonValue | undefined): Block {
  const block = object(value, "block");
  if (block.type === "apply_patch_tool_use")
    return {
      type: block.type,
      id: nonEmptyString(block.id, "block.id"),
      status: decodeApplyPatchCallStatus(block.status, "block.status"),
      operation: decodeApplyPatchOperation(block.operation, "block.operation"),
    };
  if (block.type === "apply_patch_tool_result") {
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
  if (block.type === "custom_tool_use")
    return {
      type: block.type,
      id: nonEmptyString(block.id, "block.id"),
      name: nonEmptyString(block.name, "block.name"),
      input: string(block.input, "block.input"),
    };
  if (block.type === "text") {
    let cites: TextCitation[] | undefined;
    if (block.citations !== undefined) {
      if (!Array.isArray(block.citations)) fail("citations must be an array");
      cites = block.citations.map((c) => {
        const obj = object(c, "citation");
        const source = string(obj.source, "citation.source");
        if (source !== "anthropic" && source !== "openai")
          fail(`unknown citation source: ${source}`);
        const val = string(obj.value, "citation.value");
        validateCitation(source, val);
        return { source: source as "anthropic" | "openai", value: val };
      });
    }
    return {
      type: "text",
      text: string(block.text, "block.text"),
      ...(cites && cites.length > 0 ? { citations: cites } : {}),
    };
  }
  if (block.type === "thinking")
    return {
      type: "thinking",
      thinking: string(block.thinking, "block.thinking"),
      ...(block.signature === undefined
        ? {}
        : { signature: nonEmptyString(block.signature, "block.signature") }),
    };
  if (block.type === "redacted_thinking")
    return {
      type: "redacted_thinking",
      data: string(block.data, "block.data"),
    };
  if (block.type === "tool_use")
    return {
      type: "tool_use",
      id: string(block.id, "block.id"),
      name: string(block.name, "block.name"),
      input: string(block.input, "block.input") as JsonText,
    };
  if (block.type === "refusal")
    return {
      type: "refusal",
      refusal: string(block.refusal, "block.refusal"),
      ...(block.category === undefined
        ? {}
        : { category: string(block.category, "block.category") }),
    };
  fail("unsupported block type");
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

function encodeDelta(delta: Delta): JsonObject {
  if (delta.type === "custom_tool_input_delta")
    return { type: delta.type, text: delta.text };
  if (delta.type === "text_delta")
    return { type: "text_delta", text: delta.text };
  if (delta.type === "thinking_delta")
    return { type: "thinking_delta", text: delta.text };
  if (delta.type === "signature_delta")
    return { type: "signature_delta", signature: delta.signature };
  if (delta.type === "input_json_delta")
    return { type: "input_json_delta", partial_json: delta.partial_json };
  if (delta.type === "citation_delta")
    return {
      type: "citation_delta",
      citation: { source: delta.citation.source, value: delta.citation.value },
    };
  throw new OxaError("ir-invariant", "codec does not yet encode delta");
}

function decodeDelta(value: JsonValue | undefined): Delta {
  const delta = object(value, "delta");
  if (delta.type === "custom_tool_input_delta")
    return { type: delta.type, text: string(delta.text, "delta.text") };
  if (delta.type === "text_delta")
    return { type: "text_delta", text: string(delta.text, "delta.text") };
  if (delta.type === "thinking_delta")
    return {
      type: "thinking_delta",
      text: string(delta.text, "delta.text"),
    };
  if (delta.type === "signature_delta")
    return {
      type: "signature_delta",
      signature: nonEmptyString(delta.signature, "delta.signature"),
    };
  if (delta.type === "input_json_delta")
    return {
      type: "input_json_delta",
      partial_json: string(
        delta.partial_json,
        "delta.partial_json",
      ) as JsonText,
    };
  if (delta.type === "citation_delta") {
    const c = object(delta.citation, "delta.citation");
    const source = string(c.source, "delta.citation.source");
    if (source !== "anthropic" && source !== "openai")
      fail(`unknown citation source: ${source}`);
    const val = string(c.value, "delta.citation.value");
    validateCitation(source, val);
    return {
      type: "citation_delta",
      citation: { source: source as "anthropic" | "openai", value: val },
    };
  }
  fail("unsupported delta type");
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
          input_tokens_details: {
            cached_tokens: integer(usage.input_tokens_details.cached_tokens),
          },
        }),
    ...(usage.output_tokens_details === undefined
      ? {}
      : {
          output_tokens_details: {
            reasoning_tokens: integer(
              usage.output_tokens_details.reasoning_tokens,
            ),
          },
        }),
  };
}
function decodeUsage(value: JsonValue | undefined): Usage {
  const usage = object(value, "usage");
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
          input_tokens_details: {
            cached_tokens: token(
              object(usage.input_tokens_details, "usage.input_tokens_details")
                .cached_tokens,
              "usage.input_tokens_details.cached_tokens",
            ),
          },
        }),
    ...(usage.output_tokens_details === undefined
      ? {}
      : {
          output_tokens_details: {
            reasoning_tokens: token(
              object(usage.output_tokens_details, "usage.output_tokens_details")
                .reasoning_tokens,
              "usage.output_tokens_details.reasoning_tokens",
            ),
          },
        }),
  };
}
function index(value: JsonValue | undefined): number {
  const result = token(value, "event.index");
  if (result < 0n || result > BigInt(Number.MAX_SAFE_INTEGER))
    fail("event.index is out of range");
  return Number(result);
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
function supportedSpecVersion(value: unknown): value is "0.1.0" | "0.2.0" {
  return value === "0.1.0" || value === "0.2.0";
}
function fail(message: string): never {
  throw new OxaError("type-violation", message);
}
