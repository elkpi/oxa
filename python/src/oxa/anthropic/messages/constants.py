"""Anthropic Messages wire protocol constants (spec/12)."""

ROLE_USER = "user"
ROLE_ASSISTANT = "assistant"

BLOCK_TYPE_TEXT = "text"
BLOCK_TYPE_IMAGE = "image"
BLOCK_TYPE_TOOL_USE = "tool_use"
BLOCK_TYPE_TOOL_RESULT = "tool_result"

SOURCE_TYPE_BASE64 = "base64"
SOURCE_TYPE_URL = "url"

TOOL_CHOICE_TYPE_AUTO = "auto"
TOOL_CHOICE_TYPE_ANY = "any"
TOOL_CHOICE_TYPE_NONE = "none"
TOOL_CHOICE_TYPE_TOOL = "tool"

STOP_REASON_END_TURN = "end_turn"
STOP_REASON_MAX_TOKENS = "max_tokens"
STOP_REASON_STOP_SEQUENCE = "stop_sequence"
STOP_REASON_TOOL_USE = "tool_use"
STOP_REASON_REFUSAL = "refusal"

EVENT_TYPE_MESSAGE_START = "message_start"
EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start"
EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta"
EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop"
EVENT_TYPE_MESSAGE_DELTA = "message_delta"
EVENT_TYPE_MESSAGE_STOP = "message_stop"

DELTA_TYPE_TEXT_DELTA = "text_delta"
DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta"

TYPE_MESSAGE = "message"
