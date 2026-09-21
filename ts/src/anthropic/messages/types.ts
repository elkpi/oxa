import type { JsonNumber, JsonObject, JsonText } from "../../json/index.js";

export interface AnthropicUsage {
  readonly input_tokens: bigint | JsonNumber;
  readonly output_tokens: bigint | JsonNumber;
  readonly cache_read_input_tokens?: bigint | JsonNumber;
  readonly cache_creation_input_tokens?: bigint | JsonNumber;
}

/** A native block; opaque text retains tool input source bytes when known. */
export interface AnthropicContentBlock {
  readonly type: string;
  readonly text?: string;
  /** Model reasoning text for thinking blocks (N-AN-11). */
  readonly thinking?: string;
  /** Opaque provider integrity token; carried verbatim. */
  readonly signature?: string;
  readonly id?: string;
  readonly name?: string;
  readonly input?: JsonText | JsonObject;
  readonly inputText?: JsonText;
}

export interface AnthropicMessageEnvelope {
  readonly id: string;
  readonly type: "message";
  readonly role: "assistant";
  readonly model: string;
  readonly content: readonly AnthropicContentBlock[];
  readonly stop_reason: string | null;
  readonly stop_sequence?: string | null;
  readonly usage: AnthropicUsage;
}

export interface AnthropicStreamDelta {
  readonly type?: string;
  readonly text?: string;
  /** Reasoning fragment for thinking_delta (native field name). */
  readonly thinking?: string;
  /** Opaque provider integrity token for signature_delta. */
  readonly signature?: string;
  readonly partial_json?: string;
  readonly stop_reason?: string;
  readonly stop_sequence?: string | null;
}

/** A typed Anthropic Messages streaming event envelope. */
export interface AnthropicStreamEvent {
  readonly type: string;
  readonly message?: AnthropicMessageEnvelope;
  readonly index?: number;
  readonly content_block?: AnthropicContentBlock;
  readonly delta?: AnthropicStreamDelta;
  readonly usage?: AnthropicUsage;
}
