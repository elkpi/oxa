"""Intermediate Representation (IR) types (spec/01)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Union

from oxa.ir.constants import (
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
    EVENT_TYPE_MESSAGE_DONE,
    EVENT_TYPE_MESSAGE_START,
)

# ---- Blocks ----------------------------------------------------------------


@dataclass(frozen=True, slots=True)
class TextBlock:
    """A run of text (spec/01 §3.4)."""

    text: str
    type: str = field(default=BLOCK_TYPE_TEXT, init=False)


@dataclass(frozen=True, slots=True)
class ImageBlock:
    """An image input (spec/01 §3.4).

    Exactly one of data or url must be present; media_type is required with data.
    """

    media_type: str | None = None
    data: str | None = None
    url: str | None = None
    type: str = field(default=BLOCK_TYPE_IMAGE, init=False)


@dataclass(frozen=True, slots=True)
class ToolUseBlock:
    """A tool invocation produced by the model (spec/01 §3.4).

    input is the raw JSON string token carried as opaque JSON text:
    implementations MUST NOT parse or re-serialize it (INV-1).
    """

    id: str
    name: str
    input: str
    type: str = field(default=BLOCK_TYPE_TOOL_USE, init=False)


@dataclass(frozen=True, slots=True)
class ToolResultBlock:
    """The outcome of a tool invocation, supplied by the caller (spec/01 §3.4)."""

    tool_use_id: str
    content: list[Block] = field(default_factory=list)
    is_error: bool = False
    type: str = field(default=BLOCK_TYPE_TOOL_RESULT, init=False)


Block = Union[TextBlock, ImageBlock, ToolUseBlock, ToolResultBlock]
SystemBlock = TextBlock

# ---- Request Components ---------------------------------------------------


@dataclass(frozen=True, slots=True)
class Message:
    """A single conversational turn (spec/01 §3.3)."""

    role: str
    content: list[Block]


@dataclass(frozen=True, slots=True)
class Tool:
    """A tool definition (spec/01 §3.5).

    input_schema is a JSON-Schema-shaped object carried verbatim.
    """

    name: str
    input_schema: dict[str, Any]
    description: str | None = None


@dataclass(frozen=True, slots=True)
class ToolChoice:
    """Selects tool-usage behavior (spec/01 §3.6).

    name is required iff mode is 'tool'.
    """

    mode: str
    name: str | None = None


@dataclass(frozen=True, slots=True)
class Params:
    """Sampling parameters (spec/01 §3.7).

    None represents absence. Absent and zero/empty are distinct states.
    """

    temperature: float | None = None
    top_p: float | None = None
    max_tokens: int | None = None
    stop_sequences: list[str] | None = None


@dataclass(frozen=True, slots=True)
class Request:
    """A face-neutral conversation request to a model (spec/01 §3.1)."""

    model: str
    messages: list[Message]
    system: list[SystemBlock] = field(default_factory=list)
    tools: list[Tool] | None = None
    tool_choice: ToolChoice | None = None
    params: Params | None = None
    metadata: dict[str, str] | None = None


# ---- Response Components --------------------------------------------------


@dataclass(frozen=True, slots=True)
class Usage:
    """Token counts (spec/01 §4.2)."""

    input_tokens: int = 0
    output_tokens: int = 0


@dataclass(frozen=True, slots=True)
class Response:
    """A completed (aggregated) model response (spec/01 §4.1)."""

    id: str
    model: str
    content: list[Block]
    stop_reason: str
    usage: Usage
    stop_sequence: str | None = None


# ---- Streaming Events & Deltas --------------------------------------------


@dataclass(frozen=True, slots=True)
class TextDelta:
    """A text fragment (spec/01 §5.2)."""

    text: str
    type: str = field(default=DELTA_TYPE_TEXT_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class InputJsonDelta:
    """A fragment of the tool-argument string (spec/01 §5.2).

    partial_json is raw JSON text (INV-1).
    """

    partial_json: str
    type: str = field(default=DELTA_TYPE_INPUT_JSON_DELTA, init=False)


Delta = Union[TextDelta, InputJsonDelta]


@dataclass(frozen=True, slots=True)
class MessageStart:
    """Opens the stream (spec/01 §5.1)."""

    id: str
    model: str
    type: str = field(default=EVENT_TYPE_MESSAGE_START, init=False)


@dataclass(frozen=True, slots=True)
class ContentBlockStart:
    """Opens a content block at index (spec/01 §5.1)."""

    index: int
    block: Block
    type: str = field(default=EVENT_TYPE_CONTENT_BLOCK_START, init=False)


@dataclass(frozen=True, slots=True)
class ContentBlockDelta:
    """Carries a delta for the currently open block (spec/01 §5.1)."""

    index: int
    delta: Delta
    type: str = field(default=EVENT_TYPE_CONTENT_BLOCK_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class ContentBlockStop:
    """Closes the currently open block (spec/01 §5.1)."""

    index: int
    type: str = field(default=EVENT_TYPE_CONTENT_BLOCK_STOP, init=False)


@dataclass(frozen=True, slots=True)
class MessageDelta:
    """Carries the final stop reason and usage immediately before MessageDone (spec/01 §5.1)."""

    stop_reason: str
    usage: Usage
    stop_sequence: str | None = None
    type: str = field(default=EVENT_TYPE_MESSAGE_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class MessageDone:
    """Terminates the stream (spec/01 §5.1)."""

    type: str = field(default=EVENT_TYPE_MESSAGE_DONE, init=False)


Event = Union[
    MessageStart,
    ContentBlockStart,
    ContentBlockDelta,
    ContentBlockStop,
    MessageDelta,
    MessageDone,
]


@dataclass(frozen=True, slots=True)
class EventStream:
    """A streamed response document (spec/01 §5.3)."""

    events: list[Event]
