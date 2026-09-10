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
import {
  specVersion,
  type Block,
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
  if (root.specVersion !== specVersion) fail("unsupported specVersion");
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
  if (block.type === "text") return { type: "text", text: block.text };
  if (block.type === "tool_use")
    return {
      type: "tool_use",
      id: block.id,
      name: block.name,
      input: block.input,
    };
  throw new OxaError(
    "ir-invariant",
    `codec does not yet encode block ${block.type}`,
  );
}
function decodeBlock(value: JsonValue | undefined): Block {
  const block = object(value, "block");
  if (block.type === "text")
    return { type: "text", text: string(block.text, "block.text") };
  if (block.type === "tool_use")
    return {
      type: "tool_use",
      id: string(block.id, "block.id"),
      name: string(block.name, "block.name"),
      input: string(block.input, "block.input") as JsonText,
    };
  fail("unsupported block type");
}
function encodeDelta(delta: Delta): JsonObject {
  if (delta.type === "text_delta")
    return { type: "text_delta", text: delta.text };
  if (delta.type === "input_json_delta")
    return { type: "input_json_delta", partial_json: delta.partial_json };
  throw new OxaError("ir-invariant", "codec does not yet encode delta");
}
function decodeDelta(value: JsonValue | undefined): Delta {
  const delta = object(value, "delta");
  if (delta.type === "text_delta")
    return { type: "text_delta", text: string(delta.text, "delta.text") };
  if (delta.type === "input_json_delta")
    return {
      type: "input_json_delta",
      partial_json: string(
        delta.partial_json,
        "delta.partial_json",
      ) as JsonText,
    };
  fail("unsupported delta type");
}
function encodeUsage(usage: Usage): JsonObject {
  return {
    input_tokens: integer(usage.input_tokens),
    output_tokens: integer(usage.output_tokens),
  };
}
function decodeUsage(value: JsonValue | undefined): Usage {
  const usage = object(value, "usage");
  return {
    input_tokens: token(usage.input_tokens, "usage.input_tokens"),
    output_tokens: token(usage.output_tokens, "usage.output_tokens"),
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
  try {
    return BigInt(value.token);
  } catch {
    fail(`${name} must be an integer`);
  }
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
function fail(message: string): never {
  throw new OxaError("type-violation", message);
}
