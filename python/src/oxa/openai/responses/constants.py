"""OpenAI Responses wire protocol constants (spec/11)."""

from typing import Final

ROLE_SYSTEM: Final = "system"
ROLE_USER: Final = "user"
ROLE_ASSISTANT: Final = "assistant"
ROLE_DEVELOPER: Final = "developer"

TOOL_TYPE_FUNCTION: Final = "function"

TOOL_CHOICE_AUTO: Final = "auto"
TOOL_CHOICE_NONE: Final = "none"
TOOL_CHOICE_REQUIRED: Final = "required"

ITEM_TYPE_MESSAGE: Final = "message"
ITEM_TYPE_FUNCTION_CALL: Final = "function_call"
ITEM_TYPE_FUNCTION_CALL_OUTPUT: Final = "function_call_output"

PART_TYPE_INPUT_TEXT: Final = "input_text"
PART_TYPE_OUTPUT_TEXT: Final = "output_text"
PART_TYPE_INPUT_IMAGE: Final = "input_image"

STATUS_IN_PROGRESS: Final = "in_progress"
STATUS_COMPLETED: Final = "completed"
STATUS_INCOMPLETE: Final = "incomplete"
STATUS_FAILED: Final = "failed"

INCOMPLETE_REASON_MAX_OUTPUT_TOKENS: Final = "max_output_tokens"

ERROR_CODE_REFUSAL: Final = "refusal"

OBJECT_RESPONSE: Final = "response"

EVENT_TYPE_RESPONSE_CREATED: Final = "response.created"
EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED: Final = "response.output_item.added"
EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE: Final = "response.output_item.done"
EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED: Final = "response.content_part.added"
EVENT_TYPE_RESPONSE_CONTENT_PART_DONE: Final = "response.content_part.done"
EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA: Final = "response.output_text.delta"
EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE: Final = "response.output_text.done"
EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA: Final = (
    "response.function_call_arguments.delta"
)
EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE: Final = (
    "response.function_call_arguments.done"
)
EVENT_TYPE_RESPONSE_COMPLETED: Final = "response.completed"
EVENT_TYPE_RESPONSE_INCOMPLETE: Final = "response.incomplete"
EVENT_TYPE_RESPONSE_FAILED: Final = "response.failed"

__all__ = [
    "ROLE_SYSTEM",
    "ROLE_USER",
    "ROLE_ASSISTANT",
    "ROLE_DEVELOPER",
    "TOOL_TYPE_FUNCTION",
    "TOOL_CHOICE_AUTO",
    "TOOL_CHOICE_NONE",
    "TOOL_CHOICE_REQUIRED",
    "ITEM_TYPE_MESSAGE",
    "ITEM_TYPE_FUNCTION_CALL",
    "ITEM_TYPE_FUNCTION_CALL_OUTPUT",
    "PART_TYPE_INPUT_TEXT",
    "PART_TYPE_OUTPUT_TEXT",
    "PART_TYPE_INPUT_IMAGE",
    "STATUS_IN_PROGRESS",
    "STATUS_COMPLETED",
    "STATUS_INCOMPLETE",
    "STATUS_FAILED",
    "INCOMPLETE_REASON_MAX_OUTPUT_TOKENS",
    "ERROR_CODE_REFUSAL",
    "OBJECT_RESPONSE",
    "EVENT_TYPE_RESPONSE_CREATED",
    "EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED",
    "EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE",
    "EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED",
    "EVENT_TYPE_RESPONSE_CONTENT_PART_DONE",
    "EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA",
    "EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE",
    "EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA",
    "EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE",
    "EVENT_TYPE_RESPONSE_COMPLETED",
    "EVENT_TYPE_RESPONSE_INCOMPLETE",
    "EVENT_TYPE_RESPONSE_FAILED",
]
