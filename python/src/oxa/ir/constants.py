"""Intermediate Representation (IR) constants (spec/01)."""

SPEC_VERSION = "0.1.0"

# Roles (spec/01 §3.3)
ROLE_USER = "user"
ROLE_ASSISTANT = "assistant"

# Block types (spec/01 §3.4)
BLOCK_TYPE_TEXT = "text"
BLOCK_TYPE_IMAGE = "image"
BLOCK_TYPE_TOOL_USE = "tool_use"
BLOCK_TYPE_TOOL_RESULT = "tool_result"

# Tool choice modes (spec/01 §3.6)
TOOL_CHOICE_AUTO = "auto"
TOOL_CHOICE_ANY = "any"
TOOL_CHOICE_TOOL = "tool"
TOOL_CHOICE_NONE = "none"

# Stop reasons (spec/01 §4.1)
STOP_END_TURN = "end_turn"
STOP_MAX_TOKENS = "max_tokens"
STOP_STOP_SEQUENCE = "stop_sequence"
STOP_TOOL_USE = "tool_use"
STOP_REFUSAL = "refusal"
STOP_OTHER = "other"

# Streaming event types (spec/01 §5.1)
EVENT_TYPE_MESSAGE_START = "message_start"
EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start"
EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta"
EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop"
EVENT_TYPE_MESSAGE_DELTA = "message_delta"
EVENT_TYPE_MESSAGE_DONE = "message_done"

# Streaming delta types (spec/01 §5.2)
DELTA_TYPE_TEXT_DELTA = "text_delta"
DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta"

# Loss reasons (spec/02 §3)
LOSS_UNMAPPED_FIELD = "unmapped-field"
LOSS_UNMAPPED_VALUE = "unmapped-value"
LOSS_UNSUPPORTED_SEMANTIC = "unsupported-semantic"
LOSS_DEGRADED = "degraded"
