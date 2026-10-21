import type { JsonObject, JsonText } from "../json/index.js";

export const specVersion = "0.2.0";

export type ReasoningEffort =
  | "minimal"
  | "low"
  | "medium"
  | "high"
  | "xhigh"
  | "max";
export type StopReason =
  | "end_turn"
  | "max_tokens"
  | "stop_sequence"
  | "tool_use"
  | "refusal"
  | "other";

export interface NativeOmission {
  readonly field: string;
  readonly reason:
    | "unmapped-field"
    | "unmapped-value"
    | "unsupported-semantic"
    | "degraded";
  readonly detail?: string;
}

export interface NativeProjection {
  readonly origin: "budget" | "explicit";
  readonly effort: ReasoningEffort;
}

export interface NativeAnnotation {
  readonly source: string;
  readonly kind: string;
  readonly value: string;
  readonly omission?: NativeOmission;
  readonly projection?: NativeProjection;
}

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
  readonly native?: readonly NativeAnnotation[];
}
export interface ThinkingBlock {
  readonly type: "thinking";
  readonly thinking: string;
  readonly signature?: string;
  readonly native?: readonly NativeAnnotation[];
}
export interface ImageBlock {
  readonly type: "image";
  readonly media_type?: string;
  readonly data?: string;
  readonly url?: string;
  readonly native?: readonly NativeAnnotation[];
}
export interface ToolUseBlock {
  readonly type: "tool_use";
  readonly id: string;
  readonly name: string;
  readonly input: JsonText;
  readonly native?: readonly NativeAnnotation[];
}
export interface CustomToolUseBlock {
  readonly type: "custom_tool_use";
  readonly id: string;
  readonly name: string;
  readonly input: string;
  readonly native?: readonly NativeAnnotation[];
}
export type CustomToolResultOutput =
  | string
  | readonly (TextBlock | ImageBlock)[];
export interface CustomToolResultBlock {
  readonly type: "custom_tool_result";
  readonly tool_use_id: string;
  readonly output: CustomToolResultOutput;
  readonly native?: readonly NativeAnnotation[];
}
export interface ToolResultBlock {
  readonly type: "tool_result";
  readonly tool_use_id: string;
  readonly content: readonly Block[];
  readonly is_error?: boolean;
  readonly native?: readonly NativeAnnotation[];
}

export interface ApplyPatchCreateFileOperation {
  readonly type: "create_file";
  readonly path: string;
  readonly diff: string;
}
export interface ApplyPatchDeleteFileOperation {
  readonly type: "delete_file";
  readonly path: string;
}
export interface ApplyPatchUpdateFileOperation {
  readonly type: "update_file";
  readonly path: string;
  readonly diff: string;
}
export type ApplyPatchOperation =
  | ApplyPatchCreateFileOperation
  | ApplyPatchDeleteFileOperation
  | ApplyPatchUpdateFileOperation;

export interface ApplyPatchToolUseBlock {
  readonly type: "apply_patch_tool_use";
  readonly id: string;
  readonly status: "in_progress" | "completed";
  readonly operation: ApplyPatchOperation;
  readonly native?: readonly NativeAnnotation[];
}
export interface ApplyPatchToolResultBlock {
  readonly type: "apply_patch_tool_result";
  readonly tool_use_id: string;
  readonly status: "completed" | "failed";
  readonly output?: string;
  readonly native?: readonly NativeAnnotation[];
}

export type Block =
  | TextBlock
  | ThinkingBlock
  | ImageBlock
  | ToolUseBlock
  | ToolResultBlock
  | CustomToolUseBlock
  | CustomToolResultBlock
  | ApplyPatchToolUseBlock
  | ApplyPatchToolResultBlock;
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
export interface CustomToolInputDelta {
  readonly type: "custom_tool_input_delta";
  readonly text: string;
}
export type Delta =
  | TextDelta
  | ThinkingDelta
  | SignatureDelta
  | InputJsonDelta
  | CustomToolInputDelta;
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
export type CustomToolFormat =
  | { readonly type: "text" }
  | {
      readonly type: "grammar";
      readonly syntax: "lark" | "regex";
      readonly definition: string;
    };
export type Tool =
  | {
      readonly type?: "function";
      readonly name: string;
      readonly description?: string;
      readonly input_schema: JsonObject;
      readonly format?: never;
      readonly native?: readonly NativeAnnotation[];
      readonly source?: never;
      readonly definition?: never;
    }
  | {
      readonly type: "custom";
      readonly name: string;
      readonly description?: string;
      readonly input_schema?: never;
      readonly format?: CustomToolFormat;
      readonly native?: readonly NativeAnnotation[];
      readonly source?: never;
      readonly definition?: never;
    }
  | {
      readonly type: "apply_patch";
      readonly name?: never;
      readonly description?: never;
      readonly input_schema?: never;
      readonly format?: never;
      readonly native?: readonly NativeAnnotation[];
      readonly source?: never;
      readonly definition?: never;
    }
  | {
      readonly type: "native";
      readonly name: string;
      readonly source: string;
      readonly definition: string;
      readonly description?: never;
      readonly input_schema?: never;
      readonly format?: never;
      readonly native?: never;
    };
export type ToolChoice =
  | { readonly mode: "auto" | "any" | "none" }
  | {
      readonly mode: "tool";
      readonly name: string;
      readonly tool_type?: "function" | "custom" | "native";
    }
  | {
      readonly mode: "tool";
      readonly tool_type: "apply_patch";
      readonly name?: never;
    };
export interface ResponseFormatText {
  readonly type: "text";
}
export interface ResponseFormatJsonObject {
  readonly type: "json_object";
}
export interface ResponseFormatJsonSchema {
  readonly type: "json_schema";
  readonly name: string;
  readonly description?: string;
  readonly schema: JsonObject;
  readonly strict?: boolean;
}
export type ResponseFormat =
  | ResponseFormatText
  | ResponseFormatJsonObject
  | ResponseFormatJsonSchema;
export interface Params {
  readonly temperature?: number;
  readonly top_p?: number;
  readonly max_tokens?: bigint;
  readonly stop_sequences?: readonly string[];
  readonly reasoning_effort?: ReasoningEffort;
  readonly response_format?: ResponseFormat;
}
export interface Request {
  readonly model: string;
  readonly system?: readonly TextBlock[];
  readonly messages: readonly Message[];
  readonly tools?: readonly Tool[];
  readonly tool_choice?: ToolChoice;
  readonly params?: Params;
  readonly metadata?: Readonly<Record<string, string>>;
  readonly native?: readonly NativeAnnotation[];
}
export interface Response {
  readonly id: string;
  readonly model: string;
  readonly content: readonly Block[];
  readonly stop_reason: StopReason;
  readonly stop_sequence?: string;
  readonly usage: Usage;
}
