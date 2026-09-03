"""Anthropic Messages face implementation (spec/12)."""

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    DELTA_TYPE_INPUT_JSON_DELTA,
    DELTA_TYPE_TEXT_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA,
    EVENT_TYPE_MESSAGE_START,
    EVENT_TYPE_MESSAGE_STOP,
    ROLE_ASSISTANT,
    ROLE_USER,
    SOURCE_TYPE_BASE64,
    SOURCE_TYPE_URL,
    STOP_REASON_END_TURN,
    STOP_REASON_MAX_TOKENS,
    STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE,
    STOP_REASON_TOOL_USE,
    TOOL_CHOICE_TYPE_ANY,
    TOOL_CHOICE_TYPE_AUTO,
    TOOL_CHOICE_TYPE_NONE,
    TOOL_CHOICE_TYPE_TOOL,
    TYPE_MESSAGE,
)
from oxa.anthropic.messages.decode import decode_request, decode_response
from oxa.anthropic.messages.encode import encode_request, encode_response
from oxa.anthropic.messages.streamin import StreamDecoder
from oxa.anthropic.messages.streamout import StreamEncoder

__all__ = [
    # Constants
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
    # Functions
    "decode_request",
    "decode_response",
    "encode_request",
    "encode_response",
    # Streaming Classes
    "StreamDecoder",
    "StreamEncoder",
]
