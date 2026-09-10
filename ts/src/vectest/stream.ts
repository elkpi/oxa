import {
  assertEventSequence,
  type Block,
  type EventStream,
  type MessageDelta,
} from "../ir/index.js";

interface NormalizedTextBlock {
  readonly type: "text";
  readonly text: string;
}
interface NormalizedToolUseBlock {
  readonly type: "tool_use";
  readonly id: string;
  readonly name: string;
  readonly input: string;
}
type NormalizedBlock = NormalizedTextBlock | NormalizedToolUseBlock;
interface NormalizedStream {
  readonly id: string;
  readonly model: string;
  readonly blocks: readonly NormalizedBlock[];
  readonly terminal: MessageDelta;
}

/** Compares valid IR streams while ignoring legal text and argument chunking. */
export function compareStreams(
  expected: EventStream,
  actual: EventStream,
): string | undefined {
  const left = normalize(expected);
  const right = normalize(actual);
  if (left.id !== right.id || left.model !== right.model)
    return "message start differs";
  if (left.blocks.length !== right.blocks.length)
    return "content block count differs";
  for (let index = 0; index < left.blocks.length; index += 1) {
    const difference = compareBlock(left.blocks[index]!, right.blocks[index]!);
    if (difference !== undefined) return `block ${index}: ${difference}`;
  }
  if (left.terminal.stop_reason !== right.terminal.stop_reason)
    return "stop reason differs";
  if (left.terminal.stop_sequence !== right.terminal.stop_sequence)
    return "stop sequence differs";
  if (
    left.terminal.usage.input_tokens !== right.terminal.usage.input_tokens ||
    left.terminal.usage.output_tokens !== right.terminal.usage.output_tokens
  )
    return "usage differs";
  return undefined;
}

function normalize(stream: EventStream): NormalizedStream {
  assertEventSequence(stream.events);
  const start = stream.events[0]!;
  if (start.type !== "message_start")
    throw new Error("checked stream lacks message start");
  const blocks: NormalizedBlock[] = [];
  let open: { block: Block; text: string } | undefined;
  let terminal: MessageDelta | undefined;
  for (const event of stream.events.slice(1)) {
    if (event.type === "content_block_start") {
      open = {
        block: event.block,
        text: event.block.type === "text" ? event.block.text : "",
      };
      continue;
    }
    if (event.type === "content_block_delta") {
      if (event.delta.type === "text_delta") open!.text += event.delta.text;
      continue;
    }
    if (event.type === "content_block_stop") {
      if (open!.block.type === "text")
        blocks.push({ type: "text", text: open!.text });
      else if (open!.block.type === "tool_use") {
        blocks.push({
          type: "tool_use",
          id: open!.block.id,
          name: open!.block.name,
          input: open!.block.input,
        });
      } else {
        throw new Error(`unsupported normalized block ${open!.block.type}`);
      }
      open = undefined;
      continue;
    }
    if (event.type === "message_delta") terminal = event;
  }
  if (terminal === undefined)
    throw new Error("checked stream lacks message delta");
  return { id: start.id, model: start.model, blocks, terminal };
}

function compareBlock(
  expected: NormalizedBlock,
  actual: NormalizedBlock,
): string | undefined {
  if (expected.type !== actual.type) return "type differs";
  if (expected.type === "text" && actual.type === "text")
    return expected.text === actual.text ? undefined : "text differs";
  if (expected.type === "tool_use" && actual.type === "tool_use") {
    if (expected.id !== actual.id) return "tool id differs";
    if (expected.name !== actual.name) return "tool name differs";
    return expected.input === actual.input ? undefined : "tool input differs";
  }
  return "block differs";
}
