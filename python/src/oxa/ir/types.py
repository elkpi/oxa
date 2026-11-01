"""Intermediate Representation (IR) types (spec/01)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from oxa.ir.constants import (
    BLOCK_TYPE_DOCUMENT,
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING,
    BLOCK_TYPE_REDACTED_THINKING,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    BLOCK_TYPE_CUSTOM_TOOL_USE,
    BLOCK_TYPE_CUSTOM_TOOL_RESULT,
    BLOCK_TYPE_APPLY_PATCH_TOOL_USE,
    BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT,
    BLOCK_TYPE_WEB_SEARCH_CALL,
    BLOCK_TYPE_REFUSAL,
    DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA,
    DELTA_TYPE_INPUT_JSON_DELTA,
    DELTA_TYPE_SIGNATURE_DELTA,
    DELTA_TYPE_TEXT_DELTA,
    DELTA_TYPE_THINKING_DELTA,
    DELTA_TYPE_CITATION,
    EVENT_TYPE_CONTENT_BLOCK_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA,
    EVENT_TYPE_MESSAGE_DONE,
    EVENT_TYPE_MESSAGE_START,
    ReasoningEffort,
)
from oxa.ir.native import NativeAnnotation


def _normalize_native(val: Any) -> tuple[NativeAnnotation, ...]:
    if isinstance(val, tuple):
        return val
    if isinstance(val, list):
        return tuple(val)
    return ()


# ---- Blocks ----------------------------------------------------------------


@dataclass(frozen=True, slots=True)
class TextCitation:
    """A citation reference attached to a TextBlock (since 2.4.11)."""

    source: str
    value: str


@dataclass(frozen=True, slots=True)
class TextBlock:
    """A run of text (spec/01 §3.4)."""

    text: str
    native: tuple[NativeAnnotation, ...] = ()
    citations: tuple[TextCitation, ...] | None = None
    role: str = ""
    type: str = field(default=BLOCK_TYPE_TEXT, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))
        if self.citations is not None and not isinstance(self.citations, tuple):
            object.__setattr__(self, "citations", tuple(self.citations))


@dataclass(frozen=True, slots=True)
class RedactedThinkingBlock:
    """Anthropic encrypted thinking replay state (spec/01 §3.4, since 2.4.11)."""

    data: str
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_REDACTED_THINKING, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))

    def __repr__(self) -> str:
        return "[RedactedThinkingBlock]"


@dataclass(frozen=True, slots=True)
class ThinkingBlock:
    """Reasoning content with an optional opaque provider signature (spec/01 §3.4)."""

    thinking: str
    signature: str | None = None
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_THINKING, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ImageBlock:
    """An image input (spec/01 §3.4).

    Exactly one of data or url must be present; media_type is required with data.
    """

    media_type: str | None = None
    data: str | None = None
    url: str | None = None
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_IMAGE, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class DocumentBlock:
    """A document input (spec/01 §3.4, since 2.4.8, request-side only).

    Exactly one of data / url / file_id must be present; media_type is
    required with data. Document-level provider metadata (title, context,
    citations) travels as native annotations (spec/15 §3.5).
    """

    media_type: str | None = None
    data: str | None = None
    url: str | None = None
    file_id: str | None = None
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_DOCUMENT, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ToolUseBlock:
    """A tool invocation produced by the model (spec/01 §3.4).

    input is the raw JSON string token carried as opaque JSON text:
    implementations MUST NOT parse or re-serialize it (INV-1).
    """

    id: str
    name: str
    input: str
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_TOOL_USE, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ToolResultBlock:
    """The outcome of a tool invocation, supplied by the caller (spec/01 §3.4)."""

    tool_use_id: str
    content: list[Block] = field(default_factory=list)
    is_error: bool = False
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_TOOL_RESULT, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class CustomToolUseBlock:
    """An invocation of an opaque-text custom tool (spec/13)."""
    id: str
    name: str
    input: str
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_CUSTOM_TOOL_USE, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class CustomToolResultBlock:
    """A custom tool result with string or text/image-list output."""
    tool_use_id: str
    output: str | list[TextBlock | ImageBlock]
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_CUSTOM_TOOL_RESULT, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ApplyPatchCreateFileOperation:
    path: str
    diff: str
    type: str = field(default="create_file", init=False)


@dataclass(frozen=True, slots=True)
class ApplyPatchDeleteFileOperation:
    path: str
    type: str = field(default="delete_file", init=False)


@dataclass(frozen=True, slots=True)
class ApplyPatchUpdateFileOperation:
    path: str
    diff: str
    type: str = field(default="update_file", init=False)


ApplyPatchOperation = (
    ApplyPatchCreateFileOperation
    | ApplyPatchDeleteFileOperation
    | ApplyPatchUpdateFileOperation
)


@dataclass(frozen=True, slots=True)
class ApplyPatchToolUseBlock:
    id: str
    status: str
    operation: ApplyPatchOperation
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_APPLY_PATCH_TOOL_USE, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ApplyPatchToolResultBlock:
    tool_use_id: str
    status: str
    output: str | None = None
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class WebSearchCallBlock:
    id: str
    status: str
    action: dict[str, Any]
    extra: dict[str, Any] = field(default_factory=dict)
    type: str = field(default=BLOCK_TYPE_WEB_SEARCH_CALL, init=False)


@dataclass(frozen=True, slots=True)
class RefusalBlock:
    """A model refusal content block (spec/01 §3.4, since 2.5.0)."""

    refusal: str
    category: str | None = None
    native: tuple[NativeAnnotation, ...] = ()
    type: str = field(default=BLOCK_TYPE_REFUSAL, init=False)

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


Block = (
    TextBlock
    | ThinkingBlock
    | RedactedThinkingBlock
    | ImageBlock
    | DocumentBlock
    | ToolUseBlock
    | ToolResultBlock
    | CustomToolUseBlock
    | CustomToolResultBlock
    | ApplyPatchToolUseBlock
    | ApplyPatchToolResultBlock
    | WebSearchCallBlock
    | RefusalBlock
)
SystemBlock = TextBlock

# ---- Request Components ---------------------------------------------------


@dataclass(frozen=True, slots=True)
class Message:
    """A single conversational turn (spec/01 §3.3)."""

    role: str
    content: list[Block]


@dataclass(frozen=True, slots=True)
class CustomToolFormatText:
    type: str = field(default="text", init=False)


@dataclass(frozen=True, slots=True)
class CustomToolFormatGrammar:
    syntax: str
    definition: str
    type: str = field(default="grammar", init=False)


CustomToolFormat = CustomToolFormatText | CustomToolFormatGrammar


@dataclass(frozen=True, slots=True)
class Tool:
    """A tool definition (spec/01 §3.5).

    input_schema is a JSON-Schema-shaped object carried verbatim.
    """

    name: str = ""
    input_schema: dict[str, Any] | None = None
    description: str | None = None
    type: str = "function"
    format: CustomToolFormat | None = None
    native: tuple[NativeAnnotation, ...] = ()
    source: str | None = None
    definition: str | None = None
    strict: bool | None = None

    def __post_init__(self) -> None:
        if self.strict is not None and (
            not isinstance(self.strict, bool) or self.type != "function"
        ):
            raise ValueError("strict is only allowed as a boolean on function tools")
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


@dataclass(frozen=True, slots=True)
class ToolChoice:
    """Selects tool-usage behavior (spec/01 §3.6).

    name is required iff mode is 'tool'.
    """

    mode: str
    name: str | None = None
    tool_type: str = "function"


@dataclass(frozen=True, slots=True)
class ResponseFormatText:
    """Explicit unstructured text generation."""

    type: str = field(default="text", init=False)


@dataclass(frozen=True, slots=True)
class ResponseFormatJsonObject:
    """Unconstrained JSON object output (JSON mode)."""

    type: str = field(default="json_object", init=False)


@dataclass(frozen=True, slots=True, init=False)
class ResponseFormatJsonSchema:
    """JSON Schema 输出约束；非 OpenAI 协议可省略名称。"""

    name: str
    schema: dict[str, Any]
    description: str | None = None
    strict: bool | None = None
    schema_raw: str | None = None
    type: str = field(default="json_schema", init=False)

    def __init__(self, name: str = "", schema: dict[str, Any] | None = None,
                 description: str | None = None, strict: bool | None = None,
                 schema_raw: str | None = None) -> None:
        if not isinstance(schema, dict):
            raise ValueError("response_format.json_schema requires schema object")
        object.__setattr__(self, "name", name)
        object.__setattr__(self, "schema", schema)
        object.__setattr__(self, "description", description)
        object.__setattr__(self, "strict", strict)
        object.__setattr__(self, "schema_raw", schema_raw)
        object.__setattr__(self, "type", "json_schema")


ResponseFormat = (
    ResponseFormatText | ResponseFormatJsonObject | ResponseFormatJsonSchema
)


@dataclass(frozen=True, slots=True)
class Params:
    """Sampling parameters (spec/01 §3.7).

    None represents absence. Absent and zero/empty are distinct states.
    """

    temperature: float | None = None
    top_p: float | None = None
    max_tokens: int | None = None
    stop_sequences: list[str] | None = None
    reasoning_effort: ReasoningEffort | None = None
    verbosity: str | None = None
    response_format: ResponseFormat | None = None


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
    native: tuple[NativeAnnotation, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.native, tuple):
            object.__setattr__(self, "native", _normalize_native(self.native))


# ---- Response Components --------------------------------------------------


@dataclass(frozen=True, slots=True)
class InputTokensDetails:
    """Optional cached-input token breakdown (spec/01 §4.2)."""

    cached_tokens: int


@dataclass(frozen=True, slots=True)
class OutputTokensDetails:
    """Optional reasoning-output token breakdown (spec/01 §4.2)."""

    reasoning_tokens: int


@dataclass(frozen=True, slots=True)
class Usage:
    """Token totals and optional provider-specific details (spec/01 §4.2)."""

    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_input_tokens: int | None = None
    cache_creation_input_tokens: int | None = None
    input_tokens_details: InputTokensDetails | None = None
    output_tokens_details: OutputTokensDetails | None = None


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


@dataclass(frozen=True, slots=True)
class CustomToolInputDelta:
    """An opaque text fragment for a custom-tool input (spec/13)."""
    text: str
    type: str = field(default=DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class ThinkingDelta:
    """A reasoning-text fragment (spec/01 §5.2)."""

    text: str
    type: str = field(default=DELTA_TYPE_THINKING_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class SignatureDelta:
    """An opaque provider signature fragment (spec/01 §5.2)."""

    signature: str
    type: str = field(default=DELTA_TYPE_SIGNATURE_DELTA, init=False)


@dataclass(frozen=True, slots=True)
class CitationDelta:
    """A text citation delta (spec/01 §5.2, since 2.4.11)."""

    citation: TextCitation
    type: str = field(default=DELTA_TYPE_CITATION, init=False)


Delta = (
    TextDelta
    | InputJsonDelta
    | ThinkingDelta
    | SignatureDelta
    | CustomToolInputDelta
    | CitationDelta
)


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


Event = (
    MessageStart
    | ContentBlockStart
    | ContentBlockDelta
    | ContentBlockStop
    | MessageDelta
    | MessageDone
)


@dataclass(frozen=True, slots=True)
class EventStream:
    """A streamed response document (spec/01 §5.3)."""

    events: list[Event]
