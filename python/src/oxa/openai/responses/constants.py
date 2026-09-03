"""OpenAI Responses wire protocol constants (spec/11)."""

ROLE_SYSTEM = "system"
ROLE_USER = "user"
ROLE_ASSISTANT = "assistant"
ROLE_DEVELOPER = "developer"

TOOL_TYPE_FUNCTION = "function"

TOOL_CHOICE_AUTO = "auto"
TOOL_CHOICE_NONE = "none"
TOOL_CHOICE_REQUIRED = "required"

ITEM_TYPE_MESSAGE = "message"
ITEM_TYPE_FUNCTION_CALL = "function_call"
ITEM_TYPE_FUNCTION_CALL_OUTPUT = "function_call_output"

PART_TYPE_INPUT_TEXT = "input_text"
PART_TYPE_OUTPUT_TEXT = "output_text"
PART_TYPE_INPUT_IMAGE = "input_image"

STATUS_IN_PROGRESS = "in_progress"
STATUS_COMPLETED = "completed"
STATUS_INCOMPLETE = "incomplete"
STATUS_FAILED = "failed"

INCOMPLETE_REASON_MAX_OUTPUT_TOKENS = "max_output_tokens"

ERROR_CODE_REFUSAL = "refusal"

OBJECT_RESPONSE = "response"

EVENT_TYPE_RESPONSE_CREATED = "response.created"
EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED = "response.output_item.added"
EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE = "response.output_item.done"
EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED = "response.content_part.added"
EVENT_TYPE_RESPONSE_CONTENT_PART_DONE = "response.content_part.done"
EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA = "response.output_text.delta"
EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE = "response.output_text.done"
EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA = "response.function_call_arguments.delta"
EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE = "response.function_call_arguments.done"
EVENT_TYPE_RESPONSE_COMPLETED = "response.completed"
EVENT_TYPE_RESPONSE_INCOMPLETE = "response.incomplete"
EVENT_TYPE_RESPONSE_FAILED = "response.failed"
