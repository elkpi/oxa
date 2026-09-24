import { OxaError } from "../../error.js";
import {
  encodeUsageInteger,
  parseUsageInteger,
  type Event,
  type StopReason,
  type Usage,
} from "../../ir/index.js";
import { jsonText, type JsonNumber } from "../../json/index.js";
import type { ConversionResult, Loss } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";

export interface ChatCompletionsChunk {
  readonly object?: "chat.completion.chunk";
  readonly created?: number;
  readonly id: string;
  readonly model: string;
  readonly choices: readonly ChatCompletionsChoice[];
  readonly usage?: ChatCompletionsUsage;
}
export interface ChatCompletionsChoice {
  readonly index?: number;
  readonly delta: ChatCompletionsDelta;
  readonly finish_reason: string | null;
}
export interface ChatCompletionsDelta {
  readonly role?: string;
  readonly content?: string;
  readonly reasoning_content?: string;
  readonly tool_calls?: readonly ChatCompletionsToolCallDelta[];
}
export interface ChatCompletionsToolCallDelta {
  readonly index: number;
  readonly id?: string;
  readonly type?: string;
  readonly function?: { readonly name?: string; readonly arguments?: string };
}
export interface ChatCompletionsUsage {
  readonly prompt_tokens: bigint | JsonNumber;
  readonly completion_tokens: bigint | JsonNumber;
  readonly total_tokens: bigint | JsonNumber;
  readonly prompt_tokens_details?: {
    readonly cached_tokens: bigint | JsonNumber;
  };
  readonly completion_tokens_details?: {
    readonly reasoning_tokens: bigint | JsonNumber;
  };
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
  #id = "";
  #nativeModel = "";
  #flushed = false;
  #finishSeen = false;
  #textOpen = false;
  #textIndex = 0;
  #thinkingOpen = false;
  #thinkingIndex = 0;
  #nextIrIndex = 0;
  #stopReason: StopReason = "other";
  #usage: Usage = { input_tokens: 0n, output_tokens: 0n };

  constructor(options: ChatCompletionsStreamDecoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Feed(chunk: ChatCompletionsChunk): readonly Event[] {
    if (this.#flushed) this.#lifecycle("chunk fed after stream flush");
    if (
      this.#started &&
      (chunk.id !== this.#id || chunk.model !== this.#nativeModel)
    )
      this.#lifecycle("chunk has a conflicting stream identity");
    if (chunk.usage !== undefined)
      this.#usage = {
        input_tokens: parseUsageInteger(
          chunk.usage.prompt_tokens,
          "usage.prompt_tokens",
        ),
        output_tokens: parseUsageInteger(
          chunk.usage.completion_tokens,
          "usage.completion_tokens",
        ),
        ...(chunk.usage.prompt_tokens_details === undefined
          ? {}
          : {
              input_tokens_details: {
                cached_tokens: parseUsageInteger(
                  chunk.usage.prompt_tokens_details.cached_tokens,
                  "usage.prompt_tokens_details.cached_tokens",
                ),
              },
            }),
        ...(chunk.usage.completion_tokens_details === undefined
          ? {}
          : {
              output_tokens_details: {
                reasoning_tokens: parseUsageInteger(
                  chunk.usage.completion_tokens_details.reasoning_tokens,
                  "usage.completion_tokens_details.reasoning_tokens",
                ),
              },
            }),
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
      this.#id = chunk.id;
      this.#nativeModel = chunk.model;
      events.push({
        type: "message_start",
        id: chunk.id,
        model: mapModel(this.#modelMapper, chunk.model),
      });
    }
    this.#recordToolCalls(choice.delta.tool_calls ?? []);
    if (choice.delta.reasoning_content !== undefined) {
      if (this.#textOpen) {
        events.push({ type: "content_block_stop", index: this.#textIndex });
        this.#textOpen = false;
      }
      if (!this.#thinkingOpen) {
        this.#thinkingOpen = true;
        this.#thinkingIndex = this.#nextIrIndex;
        this.#nextIrIndex += 1;
        events.push({
          type: "content_block_start",
          index: this.#thinkingIndex,
          block: { type: "thinking", thinking: "" },
        });
      }
      if (choice.delta.reasoning_content !== "")
        events.push({
          type: "content_block_delta",
          index: this.#thinkingIndex,
          delta: {
            type: "thinking_delta",
            text: choice.delta.reasoning_content,
          },
        });
    }
    if (choice.delta.content !== undefined) {
      if (this.#thinkingOpen) {
        events.push({ type: "content_block_stop", index: this.#thinkingIndex });
        this.#thinkingOpen = false;
      }
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
    if (this.#thinkingOpen) {
      events.push({ type: "content_block_stop", index: this.#thinkingIndex });
      this.#thinkingOpen = false;
    }
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

export interface ChatCompletionsStreamEncoderOptions {
  readonly modelMapper?: ModelMapper;
}

type EncoderBlock =
  | { readonly kind: "text"; readonly index: number }
  | {
      readonly kind: "thinking";
      readonly index: number;
      sawSignatureDelta: boolean;
    }
  | {
      readonly kind: "tool";
      readonly index: number;
      readonly id: string;
      readonly name: string;
      readonly input: string;
      readonly nativeIndex: number;
      readonly fragments: string[];
      started: boolean;
    };

/** Incrementally converts IR stream events to Chat Completions chunks. */
export class ChatCompletionsStreamEncoder {
  readonly #modelMapper: ModelMapper | undefined;
  #id = "";
  #model = "";
  #started = false;
  #finished = false;
  #done = false;
  #active: EncoderBlock | undefined;
  #nextIrIndex = 0;
  #nextNativeTool = 0;
  #toolSeen = false;
  #orderingDegrade = false;
  #pendingTools: ChatCompletionsChunk[] = [];

  constructor(options: ChatCompletionsStreamEncoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Apply(event: Event): ConversionResult<readonly ChatCompletionsChunk[]> {
    if (this.#done || (this.#finished && event.type !== "message_done"))
      this.#lifecycle("event applied after stream termination");
    switch (event.type) {
      case "message_start":
        if (this.#started) this.#lifecycle("duplicate message_start");
        this.#started = true;
        this.#id = event.id;
        this.#model = mapModel(this.#modelMapper, event.model);
        return this.#result([this.#chunk({ role: "assistant" })]);
      case "content_block_start":
        return this.#startBlock(event);
      case "content_block_delta":
        return this.#delta(event);
      case "content_block_stop":
        return this.#stopBlock(event.index);
      case "message_delta":
        return this.#terminal(event.stop_reason, event.usage);
      case "message_done":
        if (!this.#finished)
          this.#lifecycle("message_done out of grammar order");
        this.#done = true;
        return this.#result([]);
    }
  }

  #startBlock(
    event: Extract<Event, { type: "content_block_start" }>,
  ): ConversionResult<readonly ChatCompletionsChunk[]> {
    if (!this.#started || this.#active !== undefined)
      this.#lifecycle("content_block_start out of grammar order");
    if (event.index !== this.#nextIrIndex)
      this.#lifecycle(
        `content_block_start index ${event.index}, want ${this.#nextIrIndex}`,
      );
    this.#nextIrIndex += 1;
    if (event.block.type === "text") {
      if (this.#toolSeen) this.#orderingDegrade = true;
      this.#active = { kind: "text", index: event.index };
      return this.#result([]);
    }
    if (event.block.type === "thinking") {
      this.#active = {
        kind: "thinking",
        index: event.index,
        sawSignatureDelta: false,
      };
      if (event.block.signature === undefined) return this.#result([]);
      return {
        value: [],
        losses: [
          {
            path: `events[${event.index}].signature`,
            field: "signature",
            reason: "unmapped-field",
            detail:
              "Chat Completions reasoning_content has no signature field; the opaque ThinkingBlock signature is lost",
          },
        ],
      };
    }
    if (event.block.type !== "tool_use")
      this.#lifecycle(`unsupported content block ${event.block.type}`);
    if (event.block.id === "" || event.block.name === "")
      this.#lifecycle("tool_use requires nonempty id and name");
    this.#active = {
      kind: "tool",
      index: event.index,
      id: event.block.id,
      name: event.block.name,
      input: event.block.input,
      nativeIndex: this.#nextNativeTool++,
      fragments: [],
      started: false,
    };
    this.#toolSeen = true;
    return this.#result([]);
  }

  #delta(
    event: Extract<Event, { type: "content_block_delta" }>,
  ): ConversionResult<readonly ChatCompletionsChunk[]> {
    if (this.#active === undefined || event.index !== this.#active.index)
      this.#lifecycle("content_block_delta out of grammar order");
    if (this.#active.kind === "text") {
      if (event.delta.type !== "text_delta")
        this.#lifecycle("text block received non-text delta");
      return this.#result([this.#chunk({ content: event.delta.text })]);
    }
    if (this.#active.kind === "thinking") {
      if (event.delta.type === "thinking_delta") {
        if (this.#active.sawSignatureDelta)
          this.#lifecycle("thinking_delta after signature_delta");
        return this.#result([
          this.#chunk({ reasoning_content: event.delta.text }),
        ]);
      }
      if (event.delta.type === "signature_delta") {
        if (this.#active.sawSignatureDelta)
          this.#lifecycle("duplicate signature_delta");
        this.#active.sawSignatureDelta = true;
        return {
          value: [],
          losses: [
            {
              path: `events[${event.index}].signature`,
              field: "signature",
              reason: "unmapped-field",
              detail:
                "Chat Completions reasoning_content has no signature field; the opaque signature delta is lost",
            },
          ],
        };
      }
      this.#lifecycle("thinking block received non-thinking delta");
    }
    if (event.delta.type !== "input_json_delta")
      this.#lifecycle("tool block received non-input-json delta");
    const fragment = event.delta.partial_json as string;
    this.#active.fragments.push(fragment);
    this.#queueToolArguments(this.#active, fragment);
    return this.#result([]);
  }

  #stopBlock(index: number): ConversionResult<readonly ChatCompletionsChunk[]> {
    if (this.#active === undefined || index !== this.#active.index)
      this.#lifecycle("content_block_stop out of grammar order");
    if (this.#active.kind === "tool") {
      if (this.#active.fragments.length === 0) {
        this.#active.fragments.push(this.#active.input);
        this.#queueToolArguments(this.#active, this.#active.input);
      }
      if (this.#active.fragments.join("") !== this.#active.input)
        this.#lifecycle(
          "tool_use input does not equal concatenated input_json_delta fragments",
        );
    }
    this.#active = undefined;
    return this.#result([]);
  }

  #terminal(
    stopReason: StopReason,
    usage: Usage,
  ): ConversionResult<readonly ChatCompletionsChunk[]> {
    if (!this.#started || this.#active !== undefined)
      this.#lifecycle("message_delta out of grammar order");
    const inputTokens = encodeUsageInteger(
      usage.input_tokens,
      "usage.input_tokens",
    );
    const outputTokens = encodeUsageInteger(
      usage.output_tokens,
      "usage.output_tokens",
    );
    this.#finished = true;
    const losses: Loss[] = [];
    const finishReason = this.#finishReason(stopReason, losses);
    if (this.#orderingDegrade)
      losses.push({
        path: "events",
        field: "ordering",
        reason: "degraded",
        detail:
          "N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved",
      });
    const chunks = this.#pendingTools;
    this.#pendingTools = [];
    chunks.push({
      id: this.#id,
      object: "chat.completion.chunk",
      created: 0,
      model: this.#model,
      choices: [{ index: 0, delta: {}, finish_reason: finishReason }],
      usage: {
        prompt_tokens: inputTokens,
        completion_tokens: outputTokens,
        total_tokens: inputTokens + outputTokens,
        ...(usage.input_tokens_details === undefined
          ? {}
          : {
              prompt_tokens_details: {
                cached_tokens: encodeUsageInteger(
                  usage.input_tokens_details.cached_tokens,
                  "usage.input_tokens_details.cached_tokens",
                ),
              },
            }),
        ...(usage.output_tokens_details === undefined
          ? {}
          : {
              completion_tokens_details: {
                reasoning_tokens: encodeUsageInteger(
                  usage.output_tokens_details.reasoning_tokens,
                  "usage.output_tokens_details.reasoning_tokens",
                ),
              },
            }),
      },
    });
    return { value: chunks, losses };
  }

  #queueToolArguments(
    block: Extract<EncoderBlock, { kind: "tool" }>,
    arguments_: string,
  ): void {
    const toolCall: ChatCompletionsToolCallDelta = !block.started
      ? {
          index: block.nativeIndex,
          id: block.id,
          type: "function",
          function: { name: block.name, arguments: arguments_ },
        }
      : { index: block.nativeIndex, function: { arguments: arguments_ } };
    block.started = true;
    this.#pendingTools.push(this.#chunk({ tool_calls: [toolCall] }));
  }

  #chunk(delta: ChatCompletionsDelta): ChatCompletionsChunk {
    return {
      id: this.#id,
      object: "chat.completion.chunk",
      created: 0,
      model: this.#model,
      choices: [{ index: 0, delta, finish_reason: null }],
    };
  }

  #result(
    value: readonly ChatCompletionsChunk[],
  ): ConversionResult<readonly ChatCompletionsChunk[]> {
    return { value, losses: [] };
  }

  #finishReason(stopReason: StopReason, losses: Loss[]): string {
    switch (stopReason) {
      case "end_turn":
        return "stop";
      case "max_tokens":
        return "length";
      case "refusal":
        return "content_filter";
      case "tool_use":
        return "tool_calls";
      case "stop_sequence":
        losses.push({
          path: "",
          field: "stop_sequence",
          reason: "unmapped-value",
          detail:
            'Chat Completions finish_reason "stop" does not identify the matched stop sequence',
        });
        return "stop";
      default:
        this.#lifecycle(
          `stop reason ${JSON.stringify(stopReason)} has no Chat Completions equivalent`,
        );
    }
  }

  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `chatcompletions: ${message}`);
  }
}
