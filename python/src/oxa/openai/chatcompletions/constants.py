"""Chat Completions wire protocol constants (spec/10)."""

from typing import Final

ROLE_SYSTEM: Final = "system"
ROLE_USER: Final = "user"
ROLE_ASSISTANT: Final = "assistant"
ROLE_TOOL: Final = "tool"

TOOL_TYPE_FUNCTION: Final = "function"

CONTENT_PART_TYPE_TEXT: Final = "text"
CONTENT_PART_TYPE_IMAGE_URL: Final = "image_url"

TOOL_CHOICE_AUTO: Final = "auto"
TOOL_CHOICE_NONE: Final = "none"
TOOL_CHOICE_REQUIRED: Final = "required"

FINISH_REASON_STOP: Final = "stop"
FINISH_REASON_LENGTH: Final = "length"
FINISH_REASON_CONTENT_FILTER: Final = "content_filter"
FINISH_REASON_TOOL_CALLS: Final = "tool_calls"

OBJECT_CHAT_COMPLETION: Final = "chat.completion"
OBJECT_CHAT_COMPLETION_CHUNK: Final = "chat.completion.chunk"

__all__ = [
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
]
