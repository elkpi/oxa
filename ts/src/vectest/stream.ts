import {
  assertEventSequence,
  type Block,
  type EventStream,
  type MessageDelta,
  type Usage,
} from "../ir/index.js";

interface NormalizedTextBlock {
  readonly type: "text";
  readonly text: string;
}
interface NormalizedThinkingBlock {
  readonly type: "thinking";
  readonly thinking: string;
  readonly signature?: string;
}
interface NormalizedToolUseBlock {
  readonly type: "tool_use";
  readonly id: string;
  readonly name: string;
  readonly input: string;
}
type NormalizedBlock =
  | NormalizedTextBlock
  | NormalizedThinkingBlock
  | NormalizedToolUseBlock;
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
  const usageDifference = compareUsage(
    left.terminal.usage,
    right.terminal.usage,
  );
  if (usageDifference !== undefined) return usageDifference;
  return undefined;
}

function compareUsage(expected: Usage, actual: Usage): string | undefined {
  if (
    expected.input_tokens !== actual.input_tokens ||
    expected.output_tokens !== actual.output_tokens ||
    expected.cache_read_input_tokens !== actual.cache_read_input_tokens ||
    expected.cache_creation_input_tokens !==
      actual.cache_creation_input_tokens ||
    expected.input_tokens_details?.cached_tokens !==
      actual.input_tokens_details?.cached_tokens ||
    expected.output_tokens_details?.reasoning_tokens !==
      actual.output_tokens_details?.reasoning_tokens
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
  let open:
    | {
        block: Block;
        text: string;
        thinking: string;
        signature: string | undefined;
      }
    | undefined;
  let terminal: MessageDelta | undefined;
  for (const event of stream.events.slice(1)) {
    if (event.type === "content_block_start") {
      open = {
        block: event.block,
        text: event.block.type === "text" ? event.block.text : "",
        thinking: event.block.type === "thinking" ? event.block.thinking : "",
        signature:
          event.block.type === "thinking" ? event.block.signature : undefined,
      };
      continue;
    }
    if (event.type === "content_block_delta") {
      if (event.delta.type === "text_delta") open!.text += event.delta.text;
      else if (event.delta.type === "thinking_delta")
        open!.thinking += event.delta.text;
      else if (event.delta.type === "signature_delta")
        open!.signature = event.delta.signature;
      continue;
    }
    if (event.type === "content_block_stop") {
      if (open!.block.type === "text")
        blocks.push({ type: "text", text: open!.text });
      else if (open!.block.type === "thinking") {
        blocks.push({
          type: "thinking",
          thinking: open!.thinking,
          ...(open!.signature === undefined
            ? {}
            : { signature: open!.signature }),
        });
      } else if (open!.block.type === "tool_use") {
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
  if (expected.type === "thinking" && actual.type === "thinking") {
    if (expected.thinking !== actual.thinking) return "thinking differs";
    return expected.signature === actual.signature
      ? undefined
      : "thinking signature differs";
  }
  if (expected.type === "tool_use" && actual.type === "tool_use") {
    if (expected.id !== actual.id) return "tool id differs";
    if (expected.name !== actual.name) return "tool name differs";
    return expected.input === actual.input ? undefined : "tool input differs";
  }
  return "block differs";
}
