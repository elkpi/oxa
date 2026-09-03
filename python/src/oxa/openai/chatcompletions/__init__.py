"""OpenAI Chat Completions face implementation (spec/10)."""

from oxa.openai.chatcompletions.constants import (
    CONTENT_PART_TYPE_IMAGE_URL,
    CONTENT_PART_TYPE_TEXT,
    FINISH_REASON_CONTENT_FILTER,
    FINISH_REASON_LENGTH,
    FINISH_REASON_STOP,
    FINISH_REASON_TOOL_CALLS,
    OBJECT_CHAT_COMPLETION,
    OBJECT_CHAT_COMPLETION_CHUNK,
    ROLE_ASSISTANT,
    ROLE_SYSTEM,
    ROLE_TOOL,
    ROLE_USER,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_REQUIRED,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.chatcompletions.decode import decode_request, decode_response
from oxa.openai.chatcompletions.encode import encode_request, encode_response
from oxa.openai.chatcompletions.streamin import StreamDecoder
from oxa.openai.chatcompletions.streamout import StreamEncoder

__all__ = [
    # Constants
    "ROLE_SYSTEM",
    "ROLE_USER",
    "ROLE_ASSISTANT",
    "ROLE_TOOL",
    "TOOL_TYPE_FUNCTION",
    "CONTENT_PART_TYPE_TEXT",
    "CONTENT_PART_TYPE_IMAGE_URL",
    "TOOL_CHOICE_AUTO",
    "TOOL_CHOICE_NONE",
    "TOOL_CHOICE_REQUIRED",
    "FINISH_REASON_STOP",
    "FINISH_REASON_LENGTH",
    "FINISH_REASON_CONTENT_FILTER",
    "FINISH_REASON_TOOL_CALLS",
    "OBJECT_CHAT_COMPLETION",
    "OBJECT_CHAT_COMPLETION_CHUNK",
    # Nonstream Functions
    "decode_request",
    "decode_response",
    "encode_request",
    "encode_response",
    # Streaming Classes
    "StreamDecoder",
    "StreamEncoder",
]
