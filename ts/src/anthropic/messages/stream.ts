import { OxaError } from "../../error.js";
import {
  encodeUsageInteger,
  parseUsageInteger,
  type Event,
  type StopReason,
  type Usage,
} from "../../ir/index.js";
import {
  isJsonArray,
  isJsonNumber,
  jsonText,
  stringifyJson,
  type JsonObject,
  type JsonText,
} from "../../json/index.js";
import type { ConversionResult, Loss } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import type { AnthropicStreamEvent } from "./types.js";

export interface AnthropicStreamDecoderOptions {
  readonly modelMapper?: ModelMapper;
}

/** Incrementally converts Anthropic Messages event objects to IR events. */
export class AnthropicStreamDecoder {
  readonly #modelMapper: ModelMapper | undefined;
  readonly #losses: Loss[] = [];
  #started = false;
  #stopped = false;
  #flushed = false;
  #nextNativeIndex = 0;
  #nextIrIndex = 0;
  #openNativeIndex: number | undefined;
  #openIrIndex = 0;
  #openKind: "text" | "tool" | "skipped" | undefined;
  #toolId = "";
  #toolName = "";
  #toolStartInput = "";
  #toolFragments: string[] = [];
  #messageDeltaSeen = false;
  #stopReason: StopReason = "other";
  #stopSequence: string | undefined;
  #usage: Usage = { input_tokens: 0n, output_tokens: 0n };

  constructor(options: AnthropicStreamDecoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Feed(event: AnthropicStreamEvent): readonly Event[] {
    if (this.#flushed) this.#lifecycle("event fed after stream flush");
    if (this.#stopped) this.#lifecycle("event fed after message_stop");
    switch (event.type) {
      case "message_start":
        return this.#messageStart(event);
      case "content_block_start":
        return this.#blockStart(event);
      case "content_block_delta":
        return this.#blockDelta(event);
      case "content_block_stop":
        return this.#blockStop(event);
      case "message_delta":
        return this.#messageDelta(event);
      case "message_stop":
        return this.#messageStop();
      default:
        this.#losses.push({
          path: "type",
          field: "type",
          reason: "unsupported-semantic",
          detail: `Anthropic stream event type ${JSON.stringify(event.type)} is not decoded in this milestone`,
        });
        return [];
    }
  }

  Flush(): readonly Event[] {
    if (this.#flushed) this.#lifecycle("stream flushed twice");
    if (!this.#stopped) this.#lifecycle("stream ended without message_stop");
    this.#flushed = true;
    return [];
  }

  Losses(): readonly Loss[] {
    return [...this.#losses];
  }

  #messageStart(event: AnthropicStreamEvent): readonly Event[] {
    if (this.#started) this.#lifecycle("duplicate message_start");
    if (event.message === undefined)
      this.#lifecycle("message_start without message");
    this.#started = true;
    return [
      {
        type: "message_start",
        id: event.message.id,
        model: mapModel(this.#modelMapper, event.message.model),
      },
    ];
  }

  #blockStart(event: AnthropicStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    if (this.#messageDeltaSeen)
      this.#lifecycle("content_block_start after message_delta");
    if (event.content_block === undefined)
      this.#lifecycle("content_block_start without content_block");
    if (this.#openKind !== undefined)
      this.#lifecycle("content_block_start with a block still open");
    const nativeIndex = this.#index(event);
    if (nativeIndex !== this.#nextNativeIndex)
      this.#lifecycle(
        `content_block_start index ${nativeIndex}, want ${this.#nextNativeIndex}`,
      );
    this.#nextNativeIndex += 1;
    this.#openNativeIndex = nativeIndex;
    const block = event.content_block;
    if (block.type === "tool_use") {
      if (block.id === undefined || block.id === "")
        this.#lifecycle(
          `content_block_start[${nativeIndex}].content_block.id is required`,
        );
      if (block.name === undefined || block.name === "")
        this.#lifecycle(
          `content_block_start[${nativeIndex}].content_block.name is required`,
        );
      this.#openKind = "tool";
      this.#openIrIndex = this.#nextIrIndex++;
      this.#toolId = block.id;
      this.#toolName = block.name;
      this.#toolStartInput = this.#inputText(block.input);
      this.#toolFragments = [];
      return [];
    }
    if (block.type !== "text") {
      this.#openKind = "skipped";
      this.#losses.push({
        path: `content_block_start[${nativeIndex}].content_block.type`,
        field: "content_block.type",
        reason: "unsupported-semantic",
        detail: `Anthropic streaming block type ${JSON.stringify(block.type)} is not decodable in M7; the index is skipped`,
      });
      return [];
    }
    this.#openKind = "text";
    this.#openIrIndex = this.#nextIrIndex++;
    return [
      {
        type: "content_block_start",
        index: this.#openIrIndex,
        block: { type: "text", text: block.text ?? "" },
      },
    ];
  }

  #blockDelta(event: AnthropicStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    const nativeIndex = this.#index(event);
    if (event.delta === undefined)
      this.#lifecycle("content_block_delta without delta");
    if (this.#openKind === undefined || nativeIndex !== this.#openNativeIndex)
      this.#lifecycle(
        `content_block_delta index ${nativeIndex} does not match the open block`,
      );
    if (this.#openKind === "skipped") return [];
    if (this.#openKind === "tool") {
      if (event.delta.type === "text_delta")
        this.#lifecycle("text_delta on tool_use block");
      if (event.delta.type === "input_json_delta") {
        if (event.delta.partial_json === undefined)
          this.#lifecycle("input_json_delta without partial_json");
        this.#toolFragments.push(event.delta.partial_json);
        return [];
      }
      this.#unknownDelta(nativeIndex, event.delta.type);
      return [];
    }
    if (event.delta.type === "input_json_delta")
      this.#lifecycle("input_json_delta on non-tool block");
    if (event.delta.type === "text_delta") {
      if (event.delta.text === undefined)
        this.#lifecycle("text_delta without text");
      return [
        {
          type: "content_block_delta",
          index: this.#openIrIndex,
          delta: { type: "text_delta", text: event.delta.text },
        },
      ];
    }
    this.#unknownDelta(nativeIndex, event.delta.type);
    return [];
  }

  #blockStop(event: AnthropicStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    const nativeIndex = this.#index(event);
    if (this.#openKind === undefined || nativeIndex !== this.#openNativeIndex)
      this.#lifecycle(
        `content_block_stop index ${nativeIndex} does not match the open block`,
      );
    if (this.#openKind === "skipped") {
      this.#clearBlock();
      return [];
    }
    if (this.#openKind === "text") {
      const index = this.#openIrIndex;
      this.#clearBlock();
      return [{ type: "content_block_stop", index }];
    }
    const fragments =
      this.#toolFragments.length === 0
        ? [this.#toolStartInput]
        : [...this.#toolFragments];
    const input = jsonText(fragments.join(""));
    const index = this.#openIrIndex;
    const id = this.#toolId;
    const name = this.#toolName;
    this.#clearBlock();
    return [
      {
        type: "content_block_start",
        index,
        block: { type: "tool_use", id, name, input },
      },
      ...fragments.map((partial_json) => ({
        type: "content_block_delta" as const,
        index,
        delta: {
          type: "input_json_delta" as const,
          partial_json: jsonText(partial_json),
        },
      })),
      { type: "content_block_stop", index },
    ];
  }

  #messageDelta(event: AnthropicStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    if (this.#openKind !== undefined)
      this.#lifecycle("message_delta with a block still open");
    if (this.#messageDeltaSeen) this.#lifecycle("duplicate message_delta");
    if (event.delta === undefined)
      this.#lifecycle("message_delta without delta");
    const nativeStop = event.delta.stop_reason;
    if (nativeStop === undefined || nativeStop === "")
      this.#lifecycle("message_delta without stop_reason");
    this.#stopReason = this.#decodeStopReason(nativeStop);
    this.#stopSequence =
      this.#stopReason === "stop_sequence" &&
      typeof event.delta.stop_sequence === "string" &&
      event.delta.stop_sequence !== ""
        ? event.delta.stop_sequence
        : undefined;
    this.#usage = this.#decodeUsage(event);
    this.#messageDeltaSeen = true;
    return [];
  }

  #messageStop(): readonly Event[] {
    this.#requireStarted("message_stop");
    if (this.#openKind !== undefined)
      this.#lifecycle("message_stop with a block still open");
    if (!this.#messageDeltaSeen)
      this.#lifecycle("message_stop without a preceding message_delta");
    this.#stopped = true;
    return [
      {
        type: "message_delta",
        stop_reason: this.#stopReason,
        ...(this.#stopReason === "stop_sequence" &&
        this.#stopSequence !== undefined
          ? { stop_sequence: this.#stopSequence }
          : {}),
        usage: this.#usage,
      },
      { type: "message_done" },
    ];
  }

  #decodeStopReason(native: string): StopReason {
    if (
      native === "end_turn" ||
      native === "max_tokens" ||
      native === "stop_sequence" ||
      native === "tool_use" ||
      native === "refusal"
    )
      return native;
    this.#losses.push({
      path: "stop_reason",
      field: "stop_reason",
      reason: "unmapped-value",
      detail: `Anthropic stop_reason ${JSON.stringify(native)} maps to other`,
    });
    return "other";
  }

  #decodeUsage(event: AnthropicStreamEvent): Usage {
    if (event.usage === undefined)
      return { input_tokens: 0n, output_tokens: 0n };
    return {
      input_tokens: parseUsageInteger(
        event.usage.input_tokens,
        "usage.input_tokens",
      ),
      output_tokens: parseUsageInteger(
        event.usage.output_tokens,
        "usage.output_tokens",
      ),
    };
  }

  #inputText(input: JsonText | JsonObject | undefined): string {
    if (typeof input === "string") return input;
    if (
      input !== undefined &&
      input !== null &&
      typeof input === "object" &&
      !isJsonArray(input) &&
      !isJsonNumber(input)
    )
      return stringifyJson(input);
    this.#lifecycle("tool_use input must be an object or opaque JSON text");
  }

  #unknownDelta(index: number, type: string | undefined): void {
    this.#losses.push({
      path: `content_block_delta[${index}].delta.type`,
      field: "delta.type",
      reason: "unsupported-semantic",
      detail: `Anthropic delta type ${JSON.stringify(type)} has no IR equivalent`,
    });
  }

  #clearBlock(): void {
    this.#openNativeIndex = undefined;
    this.#openKind = undefined;
    this.#toolId = "";
    this.#toolName = "";
    this.#toolStartInput = "";
    this.#toolFragments = [];
  }

  #requireStarted(eventType: string): void {
    if (!this.#started) this.#lifecycle(`${eventType} before message_start`);
  }

  #index(event: AnthropicStreamEvent): number {
    if (
      event.index === undefined ||
      !Number.isInteger(event.index) ||
      event.index < 0
    )
      this.#lifecycle(`${event.type} without a valid index`);
    return event.index;
  }

  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `anthropic: ${message}`);
  }
}

export interface AnthropicStreamEncoderOptions {
  readonly modelMapper?: ModelMapper;
}

type EncoderBlock =
  | { readonly kind: "text"; readonly index: number }
  | {
      readonly kind: "tool";
      readonly index: number;
      readonly input: string;
      readonly fragments: string[];
    };

/** Incrementally converts IR events to typed Anthropic streaming events. */
export class AnthropicStreamEncoder {
  readonly #modelMapper: ModelMapper | undefined;
  #started = false;
  #messageDeltaSeen = false;
  #done = false;
  #nextIndex = 0;
  #openBlock: EncoderBlock | undefined;

  constructor(options: AnthropicStreamEncoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Apply(event: Event): ConversionResult<readonly AnthropicStreamEvent[]> {
    if (this.#done) this.#lifecycle("event applied after message_done");
    switch (event.type) {
      case "message_start":
        if (this.#started) this.#lifecycle("duplicate message_start");
        this.#started = true;
        return this.#result([
          {
            type: "message_start",
            message: {
              id: event.id,
              type: "message",
              role: "assistant",
              model: mapModel(this.#modelMapper, event.model),
              content: [],
              stop_reason: null,
              usage: { input_tokens: 0n, output_tokens: 0n },
            },
          },
        ]);
      case "content_block_start":
        return this.#blockStart(event);
      case "content_block_delta":
        return this.#blockDelta(event);
      case "content_block_stop":
        return this.#blockStop(event.index);
      case "message_delta":
        return this.#messageDelta(event);
      case "message_done":
        if (!this.#messageDeltaSeen)
          this.#lifecycle("message_done out of grammar order");
        this.#done = true;
        return this.#result([{ type: "message_stop" }]);
    }
  }

  #blockStart(
    event: Extract<Event, { type: "content_block_start" }>,
  ): ConversionResult<readonly AnthropicStreamEvent[]> {
    if (
      !this.#started ||
      this.#messageDeltaSeen ||
      this.#openBlock !== undefined
    )
      this.#lifecycle("content_block_start out of grammar order");
    if (event.index !== this.#nextIndex)
      this.#lifecycle(
        `content_block_start index ${event.index}, want ${this.#nextIndex}`,
      );
    this.#nextIndex += 1;
    if (event.block.type === "text") {
      this.#openBlock = { kind: "text", index: event.index };
      return this.#result([
        {
          type: "content_block_start",
          index: event.index,
          content_block: { type: "text", text: event.block.text },
        },
      ]);
    }
    if (event.block.type === "tool_use") {
      if (event.block.id === "" || event.block.name === "")
        this.#lifecycle("tool_use requires nonempty id and name");
      if (typeof event.block.input !== "string")
        this.#lifecycle("tool_use input must be opaque JSON text");
      this.#openBlock = {
        kind: "tool",
        index: event.index,
        input: event.block.input,
        fragments: [],
      };
      return this.#result([
        {
          type: "content_block_start",
          index: event.index,
          content_block: {
            type: "tool_use",
            id: event.block.id,
            name: event.block.name,
            input: {},
          },
        },
      ]);
    }
    this.#lifecycle(`unsupported content block ${event.block.type}`);
  }

  #blockDelta(
    event: Extract<Event, { type: "content_block_delta" }>,
  ): ConversionResult<readonly AnthropicStreamEvent[]> {
    if (this.#openBlock === undefined || event.index !== this.#openBlock.index)
      this.#lifecycle("content_block_delta out of grammar order");
    if (this.#openBlock.kind === "text") {
      if (event.delta.type !== "text_delta")
        this.#lifecycle("text block received non-text delta");
      return this.#result([
        {
          type: "content_block_delta",
          index: event.index,
          delta: { type: "text_delta", text: event.delta.text },
        },
      ]);
    }
    if (event.delta.type !== "input_json_delta")
      this.#lifecycle("tool block received non-input-json delta");
    const fragment = event.delta.partial_json as string;
    this.#openBlock.fragments.push(fragment);
    return this.#result([
      {
        type: "content_block_delta",
        index: event.index,
        delta: { type: "input_json_delta", partial_json: fragment },
      },
    ]);
  }

  #blockStop(index: number): ConversionResult<readonly AnthropicStreamEvent[]> {
    if (this.#openBlock === undefined || index !== this.#openBlock.index)
      this.#lifecycle("content_block_stop out of grammar order");
    const block = this.#openBlock;
    if (block.kind === "text") {
      this.#openBlock = undefined;
      return this.#result([{ type: "content_block_stop", index }]);
    }
    const events: AnthropicStreamEvent[] = [];
    if (block.fragments.length === 0) {
      block.fragments.push(block.input);
      events.push({
        type: "content_block_delta",
        index,
        delta: { type: "input_json_delta", partial_json: block.input },
      });
    }
    if (block.fragments.join("") !== block.input)
      this.#lifecycle(
        "tool_use input does not equal concatenated input_json_delta fragments",
      );
    events.push({ type: "content_block_stop", index });
    this.#openBlock = undefined;
    return this.#result(events);
  }

  #messageDelta(
    event: Extract<Event, { type: "message_delta" }>,
  ): ConversionResult<readonly AnthropicStreamEvent[]> {
    if (
      !this.#started ||
      this.#openBlock !== undefined ||
      this.#messageDeltaSeen
    )
      this.#lifecycle("message_delta out of grammar order");
    if (event.stop_reason === "other")
      this.#lifecycle("stop reason other has no Anthropic equivalent");
    if (
      event.stop_reason === "stop_sequence" &&
      event.stop_sequence !== undefined &&
      event.stop_sequence === ""
    )
      this.#lifecycle("stop_sequence must be nonempty when present");
    const inputTokens = encodeUsageInteger(
      event.usage.input_tokens,
      "usage.input_tokens",
    );
    const outputTokens = encodeUsageInteger(
      event.usage.output_tokens,
      "usage.output_tokens",
    );
    this.#messageDeltaSeen = true;
    return this.#result([
      {
        type: "message_delta",
        delta: {
          stop_reason: event.stop_reason,
          ...(event.stop_reason === "stop_sequence" &&
          event.stop_sequence !== undefined
            ? { stop_sequence: event.stop_sequence }
            : {}),
        },
        usage: {
          input_tokens: inputTokens,
          output_tokens: outputTokens,
        },
      },
    ]);
  }

  #result<T>(value: T): ConversionResult<T> {
    return { value, losses: [] };
  }

  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `anthropic: ${message}`);
  }
}
