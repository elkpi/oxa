import { OxaError } from "../../error.js";
import type { Event, StopReason, Usage } from "../../ir/index.js";
import { jsonText } from "../../json/index.js";
import type { Loss } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";

export interface ChatCompletionsChunk {
  readonly id: string;
  readonly model: string;
  readonly choices: readonly ChatCompletionsChoice[];
  readonly usage?: ChatCompletionsUsage;
}
export interface ChatCompletionsChoice {
  readonly delta: ChatCompletionsDelta;
  readonly finish_reason: string | null;
}
export interface ChatCompletionsDelta {
  readonly role?: string;
  readonly content?: string;
  readonly tool_calls?: readonly ChatCompletionsToolCallDelta[];
}
export interface ChatCompletionsToolCallDelta {
  readonly index: number;
  readonly id?: string;
  readonly type?: string;
  readonly function?: { readonly name?: string; readonly arguments?: string };
}
export interface ChatCompletionsUsage {
  readonly prompt_tokens: number;
  readonly completion_tokens: number;
  readonly total_tokens: number;
}
export interface ChatCompletionsStreamDecoderOptions {
  readonly modelMapper?: ModelMapper;
}
interface ToolCall {
  readonly nativeIndex: number;
  id: string | undefined;
  name: string;
  readonly fragments: string[];
  skipped: boolean;
}

/** Incrementally converts Chat Completions chunks to IR stream events. */
export class ChatCompletionsStreamDecoder {
  readonly #modelMapper: ModelMapper | undefined;
  readonly #losses: Loss[] = [];
  readonly #toolCalls: ToolCall[] = [];
  #started = false;
  #flushed = false;
  #finishSeen = false;
  #textOpen = false;
  #textIndex = 0;
  #nextIrIndex = 0;
  #stopReason: StopReason = "other";
  #usage: Usage = { input_tokens: 0n, output_tokens: 0n };

  constructor(options: ChatCompletionsStreamDecoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Feed(chunk: ChatCompletionsChunk): readonly Event[] {
    if (this.#flushed) this.#lifecycle("chunk fed after stream flush");
    if (chunk.usage !== undefined)
      this.#usage = {
        input_tokens: BigInt(chunk.usage.prompt_tokens),
        output_tokens: BigInt(chunk.usage.completion_tokens),
      };
    const choice = chunk.choices[0];
    if (choice === undefined) return [];
    if (this.#started && choice.delta.role !== undefined)
      this.#lifecycle(
        this.#finishSeen
          ? "chunk stream restarted after finish_reason"
          : "chunk stream already started",
      );
    const events: Event[] = [];
    if (!this.#started) {
      this.#started = true;
      events.push({
        type: "message_start",
        id: chunk.id,
        model: mapModel(this.#modelMapper, chunk.model),
      });
    }
    this.#recordToolCalls(choice.delta.tool_calls ?? []);
    if (choice.delta.content !== undefined) {
      if (!this.#textOpen) {
        this.#textOpen = true;
        this.#textIndex = this.#nextIrIndex;
        this.#nextIrIndex += 1;
        events.push({
          type: "content_block_start",
          index: this.#textIndex,
          block: { type: "text", text: "" },
        });
      }
      events.push({
        type: "content_block_delta",
        index: this.#textIndex,
        delta: { type: "text_delta", text: choice.delta.content },
      });
    }
    if (choice.finish_reason !== null) {
      if (this.#finishSeen) this.#lifecycle("duplicate finish_reason");
      this.#stopReason = this.#finish(choice.finish_reason);
      this.#finishSeen = true;
    }
    return events;
  }

  Flush(): readonly Event[] {
    if (this.#flushed) this.#lifecycle("stream flushed twice");
    if (!this.#finishSeen)
      this.#lifecycle("stream ended without finish_reason");
    this.#flushed = true;
    const events: Event[] = [];
    if (this.#textOpen) {
      events.push({ type: "content_block_stop", index: this.#textIndex });
      this.#textOpen = false;
    }
    let index = this.#nextIrIndex;
    for (const call of this.#toolCalls) {
      if (call.skipped) continue;
      if (call.id === undefined)
        this.#lifecycle(`tool_calls[${call.nativeIndex}] is missing final ID`);
      if (call.name === "")
        this.#lifecycle(
          `tool_calls[${call.nativeIndex}] is missing final function name`,
        );
      events.push({
        type: "content_block_start",
        index,
        block: {
          type: "tool_use",
          id: call.id,
          name: call.name,
          input: jsonText(call.fragments.join("")),
        },
      });
      for (const fragment of call.fragments)
        events.push({
          type: "content_block_delta",
          index,
          delta: { type: "input_json_delta", partial_json: jsonText(fragment) },
        });
      events.push({ type: "content_block_stop", index });
      index += 1;
      this.#nextIrIndex = index;
    }
    events.push(
      {
        type: "message_delta",
        stop_reason: this.#stopReason,
        usage: this.#usage,
      },
      { type: "message_done" },
    );
    return events;
  }

  Losses(): readonly Loss[] {
    return this.#losses;
  }

  #recordToolCalls(calls: readonly ChatCompletionsToolCallDelta[]): void {
    for (const delta of calls) {
      if (
        !Number.isInteger(delta.index) ||
        delta.index < 0 ||
        delta.index > this.#toolCalls.length
      )
        this.#lifecycle(
          `tool_calls index ${delta.index} is not the next consecutive native index`,
        );
      let call = this.#toolCalls[delta.index];
      if (call === undefined) {
        call = {
          nativeIndex: delta.index,
          id: undefined,
          name: "",
          fragments: [],
          skipped: false,
        };
        this.#toolCalls.push(call);
      }
      if (delta.id !== undefined && delta.id !== "") {
        if (call.id !== undefined && call.id !== delta.id)
          this.#lifecycle(`tool_calls[${delta.index}] has conflicting IDs`);
        call.id = delta.id;
      }
      if (
        delta.type !== undefined &&
        delta.type !== "function" &&
        !call.skipped
      ) {
        this.#losses.push({
          path: `choices[0].delta.tool_calls[${delta.index}]`,
          field: "type",
          reason: "unsupported-semantic",
          detail: `Chat Completions streamed tool type ${JSON.stringify(delta.type)} has no IR equivalent`,
        });
        call.skipped = true;
      }
      if (call.skipped || delta.function === undefined) continue;
      if (delta.function.name !== undefined) call.name += delta.function.name;
      if (delta.function.arguments !== undefined)
        call.fragments.push(delta.function.arguments);
    }
  }

  #finish(reason: string): StopReason {
    switch (reason) {
      case "stop":
        return "end_turn";
      case "length":
        return "max_tokens";
      case "content_filter":
        return "refusal";
      case "tool_calls":
        return "tool_use";
      default:
        this.#losses.push({
          path: "choices[0]",
          field: "finish_reason",
          reason: "unmapped-value",
          detail: `Chat Completions finish_reason ${JSON.stringify(reason)} maps to other`,
        });
        return "other";
    }
  }
  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `chatcompletions: ${message}`);
  }
}
