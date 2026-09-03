"""Chat Completions wire protocol constants (spec/10)."""

ROLE_SYSTEM = "system"
ROLE_USER = "user"
ROLE_ASSISTANT = "assistant"
ROLE_TOOL = "tool"

TOOL_TYPE_FUNCTION = "function"

CONTENT_PART_TYPE_TEXT = "text"
CONTENT_PART_TYPE_IMAGE_URL = "image_url"

TOOL_CHOICE_AUTO = "auto"
TOOL_CHOICE_NONE = "none"
TOOL_CHOICE_REQUIRED = "required"

FINISH_REASON_STOP = "stop"
FINISH_REASON_LENGTH = "length"
FINISH_REASON_CONTENT_FILTER = "content_filter"
FINISH_REASON_TOOL_CALLS = "tool_calls"

OBJECT_CHAT_COMPLETION = "chat.completion"
OBJECT_CHAT_COMPLETION_CHUNK = "chat.completion.chunk"
