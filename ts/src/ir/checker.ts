import { OxaError } from "../error.js";
import type { Block, Delta, Event } from "./types.js";

/** Enforces the IR event grammar and contiguous block-index invariant. */
export function assertEventSequence(events: readonly Event[]): void {
  if (events.length < 3 || events[0]?.type !== "message_start")
    fail("stream must start with message_start");
  let expectedIndex = 0;
  let open:
    | {
        readonly index: number;
        readonly block: Block;
        partialJson: string;
        sawSignature: boolean;
      }
    | undefined;
  let sawTerminal = false;

  for (let position = 1; position < events.length; position += 1) {
    const event = events[position]!;
    if (sawTerminal) {
      if (event.type !== "message_done" || position !== events.length - 1)
        fail("message_done must terminate stream");
      return;
    }
    if (open !== undefined) {
      if (event.type === "content_block_delta") {
        if (event.index !== open.index || !matches(open.block, event.delta))
          fail("delta does not match open block");
        if (open.block.type === "thinking") {
          if (event.delta.type === "signature_delta") {
            if (open.sawSignature) fail("thinking block has multiple signatures");
            open = { ...open, sawSignature: true };
          } else if (
            event.delta.type === "thinking_delta" &&
            open.sawSignature
          ) {
            fail("thinking delta must precede signature delta");
          }
        }
        if (event.delta.type === "input_json_delta")
          open = {
            ...open,
            partialJson: open.partialJson + event.delta.partial_json,
          };
        continue;
      }
      if (event.type === "content_block_stop" && event.index === open.index) {
        if (
          open.block.type === "tool_use" &&
          open.partialJson !== open.block.input
        )
          fail(
            "tool input does not equal concatenated input_json_delta fragments",
          );
        open = undefined;
        expectedIndex += 1;
        continue;
      }
      fail("only matching delta or stop is allowed while a block is open");
    }
    if (event.type === "content_block_start") {
      if (!Number.isInteger(event.index) || event.index !== expectedIndex)
        fail("block indexes must be contiguous");
      open = {
        index: event.index,
        block: event.block,
        partialJson: "",
        sawSignature:
          event.block.type === "thinking" && event.block.signature !== undefined,
      };
      continue;
    }
    if (event.type === "message_delta") {
      sawTerminal = true;
      continue;
    }
    fail("expected a content block start or message_delta");
  }
  fail("stream must end with message_delta and message_done");
}

function matches(block: Block, delta: Delta): boolean {
  return (
    (block.type === "text" && delta.type === "text_delta") ||
    (block.type === "thinking" &&
      (delta.type === "thinking_delta" || delta.type === "signature_delta")) ||
    (block.type === "tool_use" && delta.type === "input_json_delta")
  );
}

function fail(message: string): never {
  throw new OxaError("ir-invariant", message);
}
