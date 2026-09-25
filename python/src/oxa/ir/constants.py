"""Intermediate Representation (IR) constants (spec/01)."""

from typing import Final, Literal

SPEC_VERSION: Final = "0.2.0"
SUPPORTED_SPEC_VERSIONS: Final = ("0.1.0", "0.2.0")

# Roles (spec/01 §3.3)
ROLE_USER: Final = "user"
ROLE_ASSISTANT: Final = "assistant"
Role = Literal["user", "assistant"]

# Block types (spec/01 §3.4)
BLOCK_TYPE_TEXT: Final = "text"
BLOCK_TYPE_THINKING: Final = "thinking"
BLOCK_TYPE_IMAGE: Final = "image"
BLOCK_TYPE_TOOL_USE: Final = "tool_use"
BLOCK_TYPE_TOOL_RESULT: Final = "tool_result"
BlockType = Literal["text", "thinking", "image", "tool_use", "tool_result"]

# Tool choice modes (spec/01 §3.6)
TOOL_CHOICE_AUTO: Final = "auto"
TOOL_CHOICE_ANY: Final = "any"
TOOL_CHOICE_TOOL: Final = "tool"
TOOL_CHOICE_NONE: Final = "none"
ToolChoiceMode = Literal["auto", "any", "tool", "none"]

# Request-side reasoning effort (spec/01 §3.7)
ReasoningEffort = Literal["minimal", "low", "medium", "high"]

# Stop reasons (spec/01 §4.1)
STOP_END_TURN: Final = "end_turn"
STOP_MAX_TOKENS: Final = "max_tokens"
STOP_STOP_SEQUENCE: Final = "stop_sequence"
STOP_TOOL_USE: Final = "tool_use"
STOP_REFUSAL: Final = "refusal"
STOP_OTHER: Final = "other"
StopReason = Literal[
    "end_turn",
    "max_tokens",
    "stop_sequence",
    "tool_use",
    "refusal",
    "other",
]

# Streaming event types (spec/01 §5.1)
EVENT_TYPE_MESSAGE_START: Final = "message_start"
EVENT_TYPE_CONTENT_BLOCK_START: Final = "content_block_start"
EVENT_TYPE_CONTENT_BLOCK_DELTA: Final = "content_block_delta"
EVENT_TYPE_CONTENT_BLOCK_STOP: Final = "content_block_stop"
EVENT_TYPE_MESSAGE_DELTA: Final = "message_delta"
EVENT_TYPE_MESSAGE_DONE: Final = "message_done"
EventType = Literal[
    "message_start",
    "content_block_start",
    "content_block_delta",
    "content_block_stop",
    "message_delta",
    "message_done",
]

# Streaming delta types (spec/01 §5.2)
DELTA_TYPE_TEXT_DELTA: Final = "text_delta"
DELTA_TYPE_INPUT_JSON_DELTA: Final = "input_json_delta"
DELTA_TYPE_THINKING_DELTA: Final = "thinking_delta"
DELTA_TYPE_SIGNATURE_DELTA: Final = "signature_delta"
DeltaType = Literal["text_delta", "input_json_delta", "thinking_delta", "signature_delta"]

# Loss reasons (spec/02 §3)
LOSS_UNMAPPED_FIELD: Final = "unmapped-field"
LOSS_UNMAPPED_VALUE: Final = "unmapped-value"
LOSS_UNSUPPORTED_SEMANTIC: Final = "unsupported-semantic"
LOSS_DEGRADED: Final = "degraded"
LossReason = Literal[
    "unmapped-field",
    "unmapped-value",
    "unsupported-semantic",
    "degraded",
]

__all__ = [
    "SPEC_VERSION",
    "SUPPORTED_SPEC_VERSIONS",
    "ROLE_USER",
    "ROLE_ASSISTANT",
    "Role",
    "BLOCK_TYPE_TEXT",
    "BLOCK_TYPE_THINKING",
    "BLOCK_TYPE_IMAGE",
    "BLOCK_TYPE_TOOL_USE",
    "BLOCK_TYPE_TOOL_RESULT",
    "BlockType",
    "TOOL_CHOICE_AUTO",
    "TOOL_CHOICE_ANY",
    "TOOL_CHOICE_TOOL",
    "TOOL_CHOICE_NONE",
    "ToolChoiceMode",
    "ReasoningEffort",
    "STOP_END_TURN",
    "STOP_MAX_TOKENS",
    "STOP_STOP_SEQUENCE",
    "STOP_TOOL_USE",
    "STOP_REFUSAL",
    "STOP_OTHER",
    "StopReason",
    "EVENT_TYPE_MESSAGE_START",
    "EVENT_TYPE_CONTENT_BLOCK_START",
    "EVENT_TYPE_CONTENT_BLOCK_DELTA",
    "EVENT_TYPE_CONTENT_BLOCK_STOP",
    "EVENT_TYPE_MESSAGE_DELTA",
    "EVENT_TYPE_MESSAGE_DONE",
    "EventType",
    "DELTA_TYPE_TEXT_DELTA",
    "DELTA_TYPE_INPUT_JSON_DELTA",
    "DELTA_TYPE_THINKING_DELTA",
    "DELTA_TYPE_SIGNATURE_DELTA",
    "DeltaType",
    "LOSS_UNMAPPED_FIELD",
    "LOSS_UNMAPPED_VALUE",
    "LOSS_UNSUPPORTED_SEMANTIC",
    "LOSS_DEGRADED",
    "LossReason",
]
