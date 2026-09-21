/** Intermediate Representation (IR) constants (spec/01, spec/02). */

export const SPEC_VERSION = "0.2.0" as const;

// Roles (spec/01 §3.3)
export const ROLE_USER = "user" as const;
export const ROLE_ASSISTANT = "assistant" as const;

// Block types (spec/01 §3.4)
export const BLOCK_TYPE_TEXT = "text" as const;
export const BLOCK_TYPE_IMAGE = "image" as const;
export const BLOCK_TYPE_TOOL_USE = "tool_use" as const;
export const BLOCK_TYPE_TOOL_RESULT = "tool_result" as const;
export const BLOCK_TYPE_THINKING = "thinking" as const;

// Tool choice modes (spec/01 §3.6)
export const TOOL_CHOICE_AUTO = "auto" as const;
export const TOOL_CHOICE_ANY = "any" as const;
export const TOOL_CHOICE_TOOL = "tool" as const;
export const TOOL_CHOICE_NONE = "none" as const;

// Stop reasons (spec/01 §4.1)
export const STOP_END_TURN = "end_turn" as const;
export const STOP_MAX_TOKENS = "max_tokens" as const;
export const STOP_STOP_SEQUENCE = "stop_sequence" as const;
export const STOP_TOOL_USE = "tool_use" as const;
export const STOP_REFUSAL = "refusal" as const;
export const STOP_OTHER = "other" as const;

// Streaming event types (spec/01 §5.1)
export const EVENT_TYPE_MESSAGE_START = "message_start" as const;
export const EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start" as const;
export const EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta" as const;
export const EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop" as const;
export const EVENT_TYPE_MESSAGE_DELTA = "message_delta" as const;
export const EVENT_TYPE_MESSAGE_DONE = "message_done" as const;

// Streaming delta types (spec/01 §5.2)
export const DELTA_TYPE_TEXT_DELTA = "text_delta" as const;
export const DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta" as const;
export const DELTA_TYPE_THINKING_DELTA = "thinking_delta" as const;
export const DELTA_TYPE_SIGNATURE_DELTA = "signature_delta" as const;

// Loss reasons (spec/02 §3)
export const LOSS_UNMAPPED_FIELD = "unmapped-field" as const;
export const LOSS_UNMAPPED_VALUE = "unmapped-value" as const;
export const LOSS_UNSUPPORTED_SEMANTIC = "unsupported-semantic" as const;
export const LOSS_DEGRADED = "degraded" as const;
