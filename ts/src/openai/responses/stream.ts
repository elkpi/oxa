import { OxaError } from "../../error.js";
import {
  encodeUsageInteger,
  parseUsageInteger,
  type Event,
  type StopReason,
  type Usage,
} from "../../ir/index.js";
import { jsonText } from "../../json/index.js";
import type { ConversionResult, Loss } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import type {
  ResponsesOutputItem,
  ResponsesOutputTextPart,
  ResponsesResponse,
  ResponsesStreamEvent,
} from "./types.js";

export interface ResponsesStreamDecoderOptions {
  readonly modelMapper?: ModelMapper;
}

interface FunctionCallState {
  readonly itemId: string;
  readonly outputIndex: number;
  readonly callId: string;
  readonly name: string;
  readonly fragments: string[];
  argumentsDone: boolean;
}

/** Incrementally converts Responses event objects to IR stream events. */
export class ResponsesStreamDecoder {
  readonly #modelMapper: ModelMapper | undefined;
  readonly #losses: Loss[] = [];
  #started = false;
  #terminated = false;
  #flushed = false;
  #nextOutputIndex = 0;
  #nextBlockIndex = 0;
  #itemOpen = false;
  #skippedItem = false;
  #itemType = "";
  #itemId = "";
  #skippedCallId = "";
  #outputIndex = 0;
  #nextContentIndex = 0;
  #functionCall: FunctionCallState | undefined;
  #toolUseSeen = false;
  #blockOpen = false;
  #skippedPart = false;
  #blockIndex = 0;
  #contentIndex = 0;
  #textDone = false;

  constructor(options: ResponsesStreamDecoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Feed(event: ResponsesStreamEvent): readonly Event[] {
    if (this.#flushed) this.#lifecycle("event fed after stream flush");
    if (this.#terminated) this.#lifecycle("event fed after terminal response");
    switch (event.type) {
      case "response.created":
        if (this.#started) this.#lifecycle("duplicate response.created");
        if (event.response === undefined)
          this.#lifecycle("response.created without response");
        this.#started = true;
        return [
          {
            type: "message_start",
            id: event.response.id,
            model: mapModel(this.#modelMapper, event.response.model),
          },
        ];
      case "response.output_item.added":
        return this.#itemAdded(event);
      case "response.function_call_arguments.delta":
        return this.#argumentDelta(event);
      case "response.function_call_arguments.done":
        return this.#argumentDone(event);
      case "response.content_part.added":
        return this.#partAdded(event);
      case "response.output_text.delta":
        return this.#textDelta(event);
      case "response.output_text.done":
        return this.#textDoneEvent(event);
      case "response.content_part.done":
        return this.#partDone(event);
      case "response.output_item.done":
        return this.#itemDone(event);
      case "response.completed":
      case "response.incomplete":
      case "response.failed":
        return this.#terminal(event);
      default:
        if (this.#isSkippedDescendant(event)) return [];
        this.#losses.push({
          path: "type",
          field: "type",
          reason: "unsupported-semantic",
          detail: `Responses stream event type ${JSON.stringify(event.type)} is not decoded in the Responses stream profile`,
        });
        return [];
    }
  }

  Flush(): readonly Event[] {
    if (this.#flushed) this.#lifecycle("stream flushed twice");
    if (!this.#terminated)
      this.#lifecycle("stream ended without a terminal response event");
    this.#flushed = true;
    return [];
  }

  Losses(): readonly Loss[] {
    return this.#losses;
  }

  #itemAdded(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    if (this.#itemOpen)
      this.#lifecycle("response.output_item.added with an item still open");
    const index = this.#outputIndexOf(event);
    if (index !== this.#nextOutputIndex)
      this.#lifecycle(
        `output_item.added output_index ${index}, want ${this.#nextOutputIndex}`,
      );
    if (event.item === undefined)
      this.#lifecycle("response.output_item.added without item");
    this.#nextOutputIndex += 1;
    this.#itemOpen = true;
    this.#itemType = event.item.type;
    this.#itemId = event.item.id ?? "";
    this.#outputIndex = index;
    this.#skippedItem = false;
    this.#skippedCallId = "";
    this.#nextContentIndex = 0;
    this.#functionCall = undefined;
    if (event.item.type === "message" && event.item.role === "assistant")
      return [];
    if (event.item.type === "function_call") {
      const callId = event.item.call_id ?? "";
      const name = event.item.name ?? "";
      if (this.#itemId === "" || callId === "" || name === "")
        this.#lifecycle("function_call item requires id, call_id, and name");
      this.#functionCall = {
        itemId: this.#itemId,
        outputIndex: index,
        callId,
        name,
        fragments: [event.item.arguments ?? ""],
        argumentsDone: false,
      };
      return [];
    }
    this.#skippedItem = true;
    if (event.item.type === "function_call_output")
      this.#skippedCallId = event.item.call_id ?? "";
    this.#losses.push(this.#unsupportedItemLoss(index, event.item.type));
    return [];
  }

  #argumentDelta(event: ResponsesStreamEvent): readonly Event[] {
    if (this.#skippedItem) {
      this.#requireActiveItem(event);
      return [];
    }
    const call = this.#requireFunctionCall(event);
    if (call.argumentsDone)
      this.#lifecycle(
        "response.function_call_arguments.delta after arguments.done",
      );
    if (event.delta === undefined)
      this.#lifecycle("response.function_call_arguments.delta without delta");
    call.fragments.push(event.delta);
    return [];
  }

  #argumentDone(event: ResponsesStreamEvent): readonly Event[] {
    if (this.#skippedItem) {
      this.#requireActiveItem(event);
      return [];
    }
    const call = this.#requireFunctionCall(event);
    if (call.argumentsDone)
      this.#lifecycle("duplicate response.function_call_arguments.done");
    if (
      event.call_id !== call.callId ||
      event.name !== call.name ||
      event.arguments !== call.fragments.join("")
    )
      this.#lifecycle(
        "response.function_call_arguments.done does not match the active function call",
      );
    call.argumentsDone = true;
    return [];
  }

  #partAdded(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireActiveItem(event);
    if (this.#functionCall !== undefined)
      this.#lifecycle("response.content_part.added on function_call item");
    if (this.#blockOpen || this.#skippedPart)
      this.#lifecycle("response.content_part.added with a part still open");
    const contentIndex = this.#contentIndexOf(event);
    if (contentIndex !== this.#nextContentIndex)
      this.#lifecycle(
        `content_part.added content_index ${contentIndex}, want ${this.#nextContentIndex}`,
      );
    if (event.part === undefined)
      this.#lifecycle("response.content_part.added without part");
    this.#nextContentIndex += 1;
    this.#contentIndex = contentIndex;
    if (this.#skippedItem) {
      this.#skippedPart = true;
      return [];
    }
    if (event.part.type !== "output_text") {
      this.#skippedPart = true;
      this.#losses.push({
        path: `output[${this.#outputIndex}].content[${contentIndex}]`,
        field: "type",
        reason: "unsupported-semantic",
        detail: `Responses streaming content type ${JSON.stringify(event.part.type)} is not decoded in the Responses stream profile`,
      });
      return [];
    }
    this.#blockOpen = true;
    this.#blockIndex = this.#nextBlockIndex;
    this.#nextBlockIndex += 1;
    this.#textDone = false;
    return [
      {
        type: "content_block_start",
        index: this.#blockIndex,
        block: { type: "text", text: event.part.text },
      },
    ];
  }

  #textDelta(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireActiveItem(event);
    if (this.#functionCall !== undefined)
      this.#lifecycle("response.output_text.delta on function_call item");
    const contentIndex = this.#contentIndexOf(event);
    if (this.#skippedItem || this.#skippedPart) {
      if (contentIndex !== this.#contentIndex)
        this.#lifecycle("output_text.delta does not match the skipped part");
      return [];
    }
    if (!this.#blockOpen || contentIndex !== this.#contentIndex)
      this.#lifecycle("output_text.delta does not match the open content part");
    if (this.#textDone)
      this.#lifecycle("output_text.delta after output_text.done");
    if (event.delta === undefined)
      this.#lifecycle("response.output_text.delta without delta");
    return [
      {
        type: "content_block_delta",
        index: this.#blockIndex,
        delta: { type: "text_delta", text: event.delta },
      },
    ];
  }

  #textDoneEvent(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireActiveItem(event);
    if (this.#functionCall !== undefined)
      this.#lifecycle("response.output_text.done on function_call item");
    const contentIndex = this.#contentIndexOf(event);
    if (this.#skippedItem || this.#skippedPart) {
      if (contentIndex !== this.#contentIndex)
        this.#lifecycle("output_text.done does not match the skipped part");
      return [];
    }
    if (!this.#blockOpen || contentIndex !== this.#contentIndex)
      this.#lifecycle("output_text.done does not match the open content part");
    if (this.#textDone) this.#lifecycle("duplicate output_text.done");
    this.#textDone = true;
    return [];
  }

  #partDone(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireActiveItem(event);
    if (this.#functionCall !== undefined)
      this.#lifecycle("response.content_part.done on function_call item");
    if (event.part === undefined)
      this.#lifecycle("response.content_part.done without part");
    const contentIndex = this.#contentIndexOf(event);
    if (this.#skippedItem || this.#skippedPart) {
      if (contentIndex !== this.#contentIndex)
        this.#lifecycle("content_part.done does not match the skipped part");
      this.#skippedPart = false;
      return [];
    }
    if (!this.#blockOpen || contentIndex !== this.#contentIndex)
      this.#lifecycle("content_part.done does not match the open content part");
    if (!this.#textDone)
      this.#lifecycle("content_part.done before output_text.done");
    this.#blockOpen = false;
    return [{ type: "content_block_stop", index: this.#blockIndex }];
  }

  #itemDone(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    const index = this.#outputIndexOf(event);
    if (!this.#itemOpen || index !== this.#outputIndex)
      this.#lifecycle("response.output_item.done does not match the open item");
    if (
      event.item === undefined ||
      event.item.id !== this.#itemId ||
      event.item.type !== this.#itemType
    )
      this.#lifecycle("response.output_item.done does not match the open item");
    if (
      this.#skippedItem &&
      this.#itemType === "function_call_output" &&
      event.item.call_id !== this.#skippedCallId
    )
      this.#lifecycle(
        "response.output_item.done does not match the active function_call_output",
      );
    if (this.#blockOpen || this.#skippedPart)
      this.#lifecycle(
        "response.output_item.done with a content part still open",
      );
    let events: readonly Event[] = [];
    if (this.#functionCall !== undefined) {
      const call = this.#functionCall;
      const arguments_ = call.fragments.join("");
      if (
        event.item.call_id !== call.callId ||
        event.item.name !== call.name ||
        event.item.arguments !== arguments_
      )
        this.#lifecycle(
          "response.output_item.done does not match the active function call",
        );
      const blockIndex = this.#nextBlockIndex;
      this.#nextBlockIndex += 1;
      events = [
        {
          type: "content_block_start",
          index: blockIndex,
          block: {
            type: "tool_use",
            id: call.callId,
            name: call.name,
            input: jsonText(arguments_),
          },
        },
        ...call.fragments.map((fragment) => ({
          type: "content_block_delta" as const,
          index: blockIndex,
          delta: {
            type: "input_json_delta" as const,
            partial_json: jsonText(fragment),
          },
        })),
        { type: "content_block_stop", index: blockIndex },
      ];
      this.#toolUseSeen = true;
    }
    this.#itemOpen = false;
    this.#skippedItem = false;
    this.#itemType = "";
    this.#itemId = "";
    this.#skippedCallId = "";
    this.#functionCall = undefined;
    return events;
  }

  #terminal(event: ResponsesStreamEvent): readonly Event[] {
    this.#requireStarted(event.type);
    if (this.#itemOpen || this.#blockOpen || this.#skippedPart)
      this.#lifecycle(`${event.type} before output lifecycle completed`);
    if (event.response === undefined)
      this.#lifecycle(`${event.type} without response`);
    const [stopReason, losses] = this.#decodeStatus(event.response);
    this.#losses.push(...losses);
    this.#terminated = true;
    const usage: Usage =
      event.response.usage === undefined
        ? { input_tokens: 0n, output_tokens: 0n }
        : {
            input_tokens: parseUsageInteger(
              event.response.usage.input_tokens,
              "response.usage.input_tokens",
            ),
            output_tokens: parseUsageInteger(
              event.response.usage.output_tokens,
              "response.usage.output_tokens",
            ),
          };
    return [
      { type: "message_delta", stop_reason: stopReason, usage },
      { type: "message_done" },
    ];
  }

  #decodeStatus(
    response: ResponsesResponse,
  ): readonly [StopReason, readonly Loss[]] {
    if (response.error !== undefined || response.status === "failed")
      return [
        "other",
        [
          {
            path: "status",
            field: "status",
            reason: "unsupported-semantic",
            detail: `Responses response failed${response.error?.code === undefined ? "" : ` with ${JSON.stringify(response.error.code)}`}`,
          },
        ],
      ];
    if (response.status === "completed")
      return [this.#toolUseSeen ? "tool_use" : "end_turn", []];
    if (response.status === "incomplete") {
      if (response.incomplete_details?.reason === "max_output_tokens")
        return ["max_tokens", []];
      return [
        "other",
        [
          {
            path: "incomplete_details.reason",
            field: "reason",
            reason: "unmapped-value",
            detail:
              "Responses incomplete reason has no IR stop reason equivalent",
          },
        ],
      ];
    }
    this.#lifecycle(
      `unknown Responses response status ${JSON.stringify(response.status)}`,
    );
  }

  #requireStarted(eventType: string): void {
    if (!this.#started) this.#lifecycle(`${eventType} before response.created`);
  }

  #requireActiveItem(event: ResponsesStreamEvent): void {
    this.#requireStarted(event.type);
    if (
      !this.#itemOpen ||
      this.#outputIndexOf(event) !== this.#outputIndex ||
      event.item_id !== this.#itemId
    )
      this.#lifecycle(`${event.type} does not match the open output item`);
  }

  #requireFunctionCall(event: ResponsesStreamEvent): FunctionCallState {
    this.#requireActiveItem(event);
    if (this.#functionCall === undefined)
      this.#lifecycle(`${event.type} without an active function_call item`);
    return this.#functionCall;
  }

  #outputIndexOf(event: ResponsesStreamEvent): number {
    if (
      !Number.isInteger(event.output_index) ||
      event.output_index === undefined
    )
      this.#lifecycle(`${event.type} without an output_index`);
    return event.output_index;
  }

  #contentIndexOf(event: ResponsesStreamEvent): number {
    if (
      !Number.isInteger(event.content_index) ||
      event.content_index === undefined
    )
      this.#lifecycle(`${event.type} without a content_index`);
    return event.content_index;
  }

  #isSkippedDescendant(event: ResponsesStreamEvent): boolean {
    return (
      this.#itemOpen &&
      this.#skippedItem &&
      event.output_index === this.#outputIndex &&
      event.item_id === this.#itemId
    );
  }

  #unsupportedItemLoss(outputIndex: number, itemType: string): Loss {
    const detail =
      itemType === "function_call_output"
        ? "N-S-10: Responses function_call_output has no supported IR block mapping; response.output_item.done completes and is absorbed for this item-only lifecycle vector"
        : `Responses streaming output item type ${JSON.stringify(itemType)} is not decoded`;
    return {
      path: `output[${outputIndex}]`,
      field: "type",
      reason: "unsupported-semantic",
      detail,
    };
  }

  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `responses: ${message}`);
  }
}

export interface ResponsesStreamEncoderOptions {
  readonly modelMapper?: ModelMapper;
}

type EncoderItem =
  | {
      readonly kind: "message";
      readonly id: string;
      readonly outputIndex: number;
      readonly content: ResponsesOutputTextPart[];
      nextContentIndex: number;
    }
  | {
      readonly kind: "function_call";
      readonly id: string;
      readonly outputIndex: number;
      readonly callId: string;
      readonly name: string;
    };

type EncoderBlock =
  | {
      readonly kind: "text";
      readonly index: number;
      readonly contentIndex: number;
      text: string;
    }
  | {
      readonly kind: "tool";
      readonly index: number;
      readonly input: string;
      readonly fragments: string[];
    };

/** Incrementally converts IR events to typed Responses streaming events. */
export class ResponsesStreamEncoder {
  readonly #modelMapper: ModelMapper | undefined;
  #id = "";
  #model = "";
  #started = false;
  #terminal = false;
  #done = false;
  #nextBlockIndex = 0;
  #nextOutputIndex = 0;
  #nextMessageItem = 0;
  #nextFunctionItem = 0;
  #activeItem: EncoderItem | undefined;
  #activeBlock: EncoderBlock | undefined;
  readonly #completed: ResponsesOutputItem[] = [];

  constructor(options: ResponsesStreamEncoderOptions = {}) {
    this.#modelMapper = options.modelMapper;
  }

  Apply(event: Event): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (this.#done || (this.#terminal && event.type !== "message_done"))
      this.#lifecycle("event applied after stream termination");
    switch (event.type) {
      case "message_start":
        if (this.#started) this.#lifecycle("duplicate message_start");
        this.#started = true;
        this.#id = event.id;
        this.#model = mapModel(this.#modelMapper, event.model);
        return this.#result([
          { type: "response.created", response: this.#response("in_progress") },
        ]);
      case "content_block_start":
        return this.#startBlock(event);
      case "content_block_delta":
        return this.#delta(event);
      case "content_block_stop":
        return this.#stopBlock(event.index);
      case "message_delta":
        return this.#messageDelta(event.stop_reason, event.usage);
      case "message_done":
        if (!this.#terminal)
          this.#lifecycle("message_done out of grammar order");
        this.#done = true;
        return this.#result([]);
    }
  }

  #startBlock(
    event: Extract<Event, { type: "content_block_start" }>,
  ): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (!this.#started || this.#terminal || this.#activeBlock !== undefined)
      this.#lifecycle("content_block_start out of grammar order");
    if (event.index !== this.#nextBlockIndex)
      this.#lifecycle(
        `content_block_start index ${event.index}, want ${this.#nextBlockIndex}`,
      );
    this.#nextBlockIndex += 1;
    if (event.block.type === "text")
      return this.#startText(event.index, event.block.text);
    if (event.block.type === "tool_use")
      return this.#startTool(
        event.index,
        event.block.id,
        event.block.name,
        event.block.input,
      );
    this.#lifecycle(`unsupported content block ${event.block.type}`);
  }

  #startText(
    index: number,
    initial: string,
  ): ConversionResult<readonly ResponsesStreamEvent[]> {
    const events: ResponsesStreamEvent[] = [];
    if (this.#activeItem === undefined) {
      const item = this.#openMessageItem();
      this.#activeItem = item;
      events.push({
        type: "response.output_item.added",
        output_index: item.outputIndex,
        item: {
          type: "message",
          id: item.id,
          status: "in_progress",
          role: "assistant",
        },
      });
    }
    if (this.#activeItem.kind !== "message")
      this.#lifecycle(
        "text block cannot open before the active function_call item completes",
      );
    const contentIndex = this.#activeItem.nextContentIndex;
    this.#activeItem.nextContentIndex += 1;
    const part: ResponsesOutputTextPart = {
      type: "output_text",
      text: initial,
      annotations: [],
    };
    this.#activeItem.content.push(part);
    this.#activeBlock = { kind: "text", index, contentIndex, text: initial };
    events.push({
      type: "response.content_part.added",
      item_id: this.#activeItem.id,
      output_index: this.#activeItem.outputIndex,
      content_index: contentIndex,
      part,
    });
    return this.#result(events);
  }

  #startTool(
    index: number,
    callId: string,
    name: string,
    input: string,
  ): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (callId === "" || name === "")
      this.#lifecycle("tool_use requires nonempty id and name");
    const events: ResponsesStreamEvent[] = [];
    if (this.#activeItem !== undefined) {
      if (this.#activeItem.kind !== "message")
        this.#lifecycle(
          "tool block cannot open before the active function_call item completes",
        );
      events.push(this.#closeMessageItem());
    }
    const item = this.#openFunctionCallItem(callId, name);
    this.#activeItem = item;
    this.#activeBlock = { kind: "tool", index, input, fragments: [] };
    events.push({
      type: "response.output_item.added",
      output_index: item.outputIndex,
      item: {
        type: "function_call",
        id: item.id,
        call_id: callId,
        name,
        status: "in_progress",
      },
    });
    return this.#result(events);
  }

  #delta(
    event: Extract<Event, { type: "content_block_delta" }>,
  ): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (
      this.#activeBlock === undefined ||
      event.index !== this.#activeBlock.index
    )
      this.#lifecycle("content_block_delta out of grammar order");
    if (this.#activeItem === undefined)
      this.#lifecycle("content block has no active item");
    if (this.#activeBlock.kind === "text") {
      if (event.delta.type !== "text_delta")
        this.#lifecycle("text block received non-text delta");
      if (this.#activeItem.kind !== "message")
        this.#lifecycle("text block has non-message item");
      this.#activeBlock.text += event.delta.text;
      return this.#result([
        {
          type: "response.output_text.delta",
          item_id: this.#activeItem.id,
          output_index: this.#activeItem.outputIndex,
          content_index: this.#activeBlock.contentIndex,
          delta: event.delta.text,
        },
      ]);
    }
    if (event.delta.type !== "input_json_delta")
      this.#lifecycle("tool block received non-input-json delta");
    if (this.#activeItem.kind !== "function_call")
      this.#lifecycle("tool block has non-function item");
    const fragment = event.delta.partial_json as string;
    this.#activeBlock.fragments.push(fragment);
    return this.#result([
      {
        type: "response.function_call_arguments.delta",
        item_id: this.#activeItem.id,
        output_index: this.#activeItem.outputIndex,
        delta: fragment,
      },
    ]);
  }

  #stopBlock(index: number): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (this.#activeBlock === undefined || index !== this.#activeBlock.index)
      this.#lifecycle("content_block_stop out of grammar order");
    if (this.#activeItem === undefined)
      this.#lifecycle("content block has no active item");
    if (this.#activeBlock.kind === "text") return this.#stopText();
    return this.#stopTool();
  }

  #stopText(): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (
      this.#activeBlock?.kind !== "text" ||
      this.#activeItem?.kind !== "message"
    )
      this.#lifecycle("text block without active message item");
    const block = this.#activeBlock;
    const item = this.#activeItem;
    const part: ResponsesOutputTextPart = {
      type: "output_text",
      text: block.text,
      annotations: [],
    };
    item.content[block.contentIndex] = part;
    this.#activeBlock = undefined;
    return this.#result([
      {
        type: "response.output_text.done",
        item_id: item.id,
        output_index: item.outputIndex,
        content_index: block.contentIndex,
        text: block.text,
      },
      {
        type: "response.content_part.done",
        item_id: item.id,
        output_index: item.outputIndex,
        content_index: block.contentIndex,
        part,
      },
    ]);
  }

  #stopTool(): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (
      this.#activeBlock?.kind !== "tool" ||
      this.#activeItem?.kind !== "function_call"
    )
      this.#lifecycle("tool block without active function_call item");
    const block = this.#activeBlock;
    const item = this.#activeItem;
    const events: ResponsesStreamEvent[] = [];
    if (block.fragments.length === 0) {
      block.fragments.push(block.input);
      events.push({
        type: "response.function_call_arguments.delta",
        item_id: item.id,
        output_index: item.outputIndex,
        delta: block.input,
      });
    }
    const arguments_ = block.fragments.join("");
    if (arguments_ !== block.input)
      this.#lifecycle(
        "tool_use input does not equal concatenated input_json_delta fragments",
      );
    const completed: ResponsesOutputItem = {
      type: "function_call",
      id: item.id,
      call_id: item.callId,
      name: item.name,
      status: "completed",
      arguments: arguments_,
    };
    events.push(
      {
        type: "response.function_call_arguments.done",
        item_id: item.id,
        output_index: item.outputIndex,
        call_id: item.callId,
        name: item.name,
        arguments: arguments_,
      },
      {
        type: "response.output_item.done",
        output_index: item.outputIndex,
        item: completed,
      },
    );
    this.#completed.push(completed);
    this.#activeBlock = undefined;
    this.#activeItem = undefined;
    return this.#result(events);
  }

  #messageDelta(
    stopReason: StopReason,
    usage: Usage,
  ): ConversionResult<readonly ResponsesStreamEvent[]> {
    if (!this.#started || this.#terminal || this.#activeBlock !== undefined)
      this.#lifecycle("message_delta out of grammar order");
    const events: ResponsesStreamEvent[] = [];
    if (this.#activeItem !== undefined) {
      if (this.#activeItem.kind !== "message")
        this.#lifecycle("message_delta with an uncompleted function_call item");
      events.push(this.#closeMessageItem());
    }
    const result = this.#terminalEvent(stopReason, usage);
    events.push(result.value);
    this.#terminal = true;
    return { value: events, losses: result.losses };
  }

  #openMessageItem(): Extract<EncoderItem, { kind: "message" }> {
    const ordinal = this.#nextMessageItem++;
    return {
      kind: "message",
      id: this.#generatedId("msg", ordinal),
      outputIndex: this.#nextOutputIndex++,
      content: [],
      nextContentIndex: 0,
    };
  }

  #openFunctionCallItem(
    callId: string,
    name: string,
  ): Extract<EncoderItem, { kind: "function_call" }> {
    const ordinal = this.#nextFunctionItem++;
    return {
      kind: "function_call",
      id: this.#generatedId("fc", ordinal),
      outputIndex: this.#nextOutputIndex++,
      callId,
      name,
    };
  }

  #closeMessageItem(): ResponsesStreamEvent {
    if (this.#activeItem?.kind !== "message")
      this.#lifecycle("no active message item");
    const item = this.#activeItem;
    const completed: ResponsesOutputItem = {
      type: "message",
      id: item.id,
      status: "completed",
      role: "assistant",
      content: item.content,
    };
    this.#completed.push(completed);
    this.#activeItem = undefined;
    return {
      type: "response.output_item.done",
      output_index: item.outputIndex,
      item: completed,
    };
  }

  #terminalEvent(
    stopReason: StopReason,
    usage: Usage,
  ): ConversionResult<ResponsesStreamEvent> {
    const response = this.#response("completed", usage, this.#completed);
    switch (stopReason) {
      case "end_turn":
      case "tool_use":
        return this.#result({ type: "response.completed", response });
      case "max_tokens":
        return this.#result({
          type: "response.incomplete",
          response: {
            ...response,
            status: "incomplete",
            incomplete_details: { reason: "max_output_tokens" },
          },
        });
      case "refusal":
        return this.#result({
          type: "response.failed",
          response: {
            ...response,
            status: "failed",
            error: { code: "refusal", message: "" },
          },
        });
      case "stop_sequence":
        return {
          value: { type: "response.completed", response },
          losses: [
            {
              path: "",
              field: "stop_sequence",
              reason: "unmapped-value",
              detail:
                "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
            },
          ],
        };
      default:
        this.#lifecycle(
          `stop reason ${JSON.stringify(stopReason)} has no Responses equivalent`,
        );
    }
  }

  #response(
    status: string,
    usage?: Usage,
    output: readonly ResponsesOutputItem[] = [],
  ): ResponsesResponse {
    const encodedUsage =
      usage === undefined
        ? undefined
        : {
            input_tokens: encodeUsageInteger(
              usage.input_tokens,
              "usage.input_tokens",
            ),
            output_tokens: encodeUsageInteger(
              usage.output_tokens,
              "usage.output_tokens",
            ),
          };
    return {
      id: this.#id,
      object: "response",
      status,
      model: this.#model,
      output,
      ...(encodedUsage === undefined
        ? {}
        : {
            usage: {
              input_tokens: encodedUsage.input_tokens,
              output_tokens: encodedUsage.output_tokens,
              total_tokens:
                encodedUsage.input_tokens + encodedUsage.output_tokens,
            },
          }),
    };
  }

  #generatedId(prefix: string, ordinal: number): string {
    return `${prefix}_abc${String(123 + 333 * ordinal).padStart(3, "0")}`;
  }

  #result<T>(value: T): ConversionResult<T> {
    return { value, losses: [] };
  }

  #lifecycle(message: string): never {
    throw new OxaError("stream-lifecycle", `responses: ${message}`);
  }
}
