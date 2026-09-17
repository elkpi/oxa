"""Anthropic Messages wire protocol constants (spec/12)."""

from typing import Final

ROLE_USER: Final = "user"
ROLE_ASSISTANT: Final = "assistant"

BLOCK_TYPE_TEXT: Final = "text"
BLOCK_TYPE_IMAGE: Final = "image"
BLOCK_TYPE_TOOL_USE: Final = "tool_use"
BLOCK_TYPE_TOOL_RESULT: Final = "tool_result"

SOURCE_TYPE_BASE64: Final = "base64"
SOURCE_TYPE_URL: Final = "url"

TOOL_CHOICE_TYPE_AUTO: Final = "auto"
TOOL_CHOICE_TYPE_ANY: Final = "any"
TOOL_CHOICE_TYPE_NONE: Final = "none"
TOOL_CHOICE_TYPE_TOOL: Final = "tool"

STOP_REASON_END_TURN: Final = "end_turn"
STOP_REASON_MAX_TOKENS: Final = "max_tokens"
STOP_REASON_STOP_SEQUENCE: Final = "stop_sequence"
STOP_REASON_TOOL_USE: Final = "tool_use"
STOP_REASON_REFUSAL: Final = "refusal"

EVENT_TYPE_MESSAGE_START: Final = "message_start"
EVENT_TYPE_CONTENT_BLOCK_START: Final = "content_block_start"
EVENT_TYPE_CONTENT_BLOCK_DELTA: Final = "content_block_delta"
EVENT_TYPE_CONTENT_BLOCK_STOP: Final = "content_block_stop"
EVENT_TYPE_MESSAGE_DELTA: Final = "message_delta"
EVENT_TYPE_MESSAGE_STOP: Final = "message_stop"

DELTA_TYPE_TEXT_DELTA: Final = "text_delta"
DELTA_TYPE_INPUT_JSON_DELTA: Final = "input_json_delta"

TYPE_MESSAGE: Final = "message"

__all__ = [
    "ROLE_USER",
    "ROLE_ASSISTANT",
    "BLOCK_TYPE_TEXT",
    "BLOCK_TYPE_IMAGE",
    "BLOCK_TYPE_TOOL_USE",
    "BLOCK_TYPE_TOOL_RESULT",
    "SOURCE_TYPE_BASE64",
    "SOURCE_TYPE_URL",
    "TOOL_CHOICE_TYPE_AUTO",
    "TOOL_CHOICE_TYPE_ANY",
    "TOOL_CHOICE_TYPE_NONE",
    "TOOL_CHOICE_TYPE_TOOL",
    "STOP_REASON_END_TURN",
    "STOP_REASON_MAX_TOKENS",
    "STOP_REASON_STOP_SEQUENCE",
    "STOP_REASON_TOOL_USE",
    "STOP_REASON_REFUSAL",
    "EVENT_TYPE_MESSAGE_START",
    "EVENT_TYPE_CONTENT_BLOCK_START",
    "EVENT_TYPE_CONTENT_BLOCK_DELTA",
    "EVENT_TYPE_CONTENT_BLOCK_STOP",
    "EVENT_TYPE_MESSAGE_DELTA",
    "EVENT_TYPE_MESSAGE_STOP",
    "DELTA_TYPE_TEXT_DELTA",
    "DELTA_TYPE_INPUT_JSON_DELTA",
    "TYPE_MESSAGE",
]
