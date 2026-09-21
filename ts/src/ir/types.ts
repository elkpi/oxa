import type { JsonObject, JsonText } from "../json/index.js";

export const specVersion = "0.2.0";

export type ReasoningEffort = "minimal" | "low" | "medium" | "high";
export type StopReason =
  | "end_turn"
  | "max_tokens"
  | "stop_sequence"
  | "tool_use"
  | "refusal"
  | "other";
export interface InputTokensDetails {
  readonly cached_tokens: bigint;
}
export interface OutputTokensDetails {
  readonly reasoning_tokens: bigint;
}
export interface Usage {
  readonly input_tokens: bigint;
  readonly output_tokens: bigint;
  readonly cache_read_input_tokens?: bigint;
  readonly cache_creation_input_tokens?: bigint;
  readonly input_tokens_details?: InputTokensDetails;
  readonly output_tokens_details?: OutputTokensDetails;
}
export interface TextBlock {
  readonly type: "text";
  readonly text: string;
}
export interface ThinkingBlock {
  readonly type: "thinking";
  readonly thinking: string;
  readonly signature?: string;
}
export interface ImageBlock {
  readonly type: "image";
  readonly media_type?: string;
  readonly data?: string;
  readonly url?: string;
}
export interface ToolUseBlock {
  readonly type: "tool_use";
  readonly id: string;
  readonly name: string;
  readonly input: JsonText;
}
export interface ToolResultBlock {
  readonly type: "tool_result";
  readonly tool_use_id: string;
  readonly content: readonly Block[];
  readonly is_error?: boolean;
}
export type Block =
  | TextBlock
  | ThinkingBlock
  | ImageBlock
  | ToolUseBlock
  | ToolResultBlock;
export interface TextDelta {
  readonly type: "text_delta";
  readonly text: string;
}
export interface ThinkingDelta {
  readonly type: "thinking_delta";
  readonly text: string;
}
export interface SignatureDelta {
  readonly type: "signature_delta";
  readonly signature: string;
}
export interface InputJsonDelta {
  readonly type: "input_json_delta";
  readonly partial_json: JsonText;
}
export type Delta =
  | TextDelta
  | ThinkingDelta
  | SignatureDelta
  | InputJsonDelta;
export interface MessageStart {
  readonly type: "message_start";
  readonly id: string;
  readonly model: string;
}
export interface ContentBlockStart {
  readonly type: "content_block_start";
  readonly index: number;
  readonly block: Block;
}
export interface ContentBlockDelta {
  readonly type: "content_block_delta";
  readonly index: number;
  readonly delta: Delta;
}
export interface ContentBlockStop {
  readonly type: "content_block_stop";
  readonly index: number;
}
export interface MessageDelta {
  readonly type: "message_delta";
  readonly stop_reason: StopReason;
  readonly stop_sequence?: string;
  readonly usage: Usage;
}
export interface MessageDone {
  readonly type: "message_done";
}
export type Event =
  | MessageStart
  | ContentBlockStart
  | ContentBlockDelta
  | ContentBlockStop
  | MessageDelta
  | MessageDone;
export interface EventStream {
  readonly events: readonly Event[];
}
export interface Message {
  readonly role: "user" | "assistant";
  readonly content: readonly Block[];
}
export interface Tool {
  readonly name: string;
  readonly description?: string;
  readonly input_schema: JsonObject;
}
export type ToolChoice =
  | { readonly mode: "auto" | "any" | "none" }
  | { readonly mode: "tool"; readonly name: string };
export interface Params {
  readonly temperature?: number;
  readonly top_p?: number;
  readonly max_tokens?: bigint;
  readonly stop_sequences?: readonly string[];
  readonly reasoning_effort?: ReasoningEffort;
}
export interface Request {
  readonly model: string;
  readonly system?: readonly TextBlock[];
  readonly messages: readonly Message[];
  readonly tools?: readonly Tool[];
  readonly tool_choice?: ToolChoice;
  readonly params?: Params;
  readonly metadata?: Readonly<Record<string, string>>;
}
export interface Response {
  readonly id: string;
  readonly model: string;
  readonly content: readonly Block[];
  readonly stop_reason: StopReason;
  readonly stop_sequence?: string;
  readonly usage: Usage;
}
