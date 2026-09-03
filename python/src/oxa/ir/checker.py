"""Event-stream invariant checking (spec/01 §7).

Enforces INV-5 grammar, INV-6 index discipline, and the relational rule that a
tool block's input equals the exact concatenation of its
input_json_delta.partial_json fragments (INV-1: compared as opaque strings).
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import Enum

from oxa.ir.constants import STOP_STOP_SEQUENCE
from oxa.ir.types import (
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    EventStream,
    InputJsonDelta,
    MessageDelta,
    MessageDone,
    MessageStart,
    TextBlock,
    TextDelta,
    ToolUseBlock,
)


@dataclass(frozen=True, slots=True)
class Violation(Exception):
    """A rule violation found by the event-stream validator."""

    event: int
    message: str

    def __str__(self) -> str:
        return f"event {self.event}: {self.message}"


class _Phase(Enum):
    NEED_START = 1
    BLOCKS = 2
    NEED_MESSAGE_DONE = 3
    DONE = 4


@dataclass(slots=True)
class _OpenTool:
    input: str
    fragments: list[str]
    fragment_count: int


def validate_event_stream(stream: EventStream) -> None:
    """Validates a decoder-produced stream (strict).

    A tool block with non-empty input must carry explicit input fragments.
    """
    _validate_with(stream, allow_synthesized=False)


def validate_event_stream_for_encoder(stream: EventStream) -> None:
    """Validates an encoder-input stream (lenient).

    Accepts the documented encoder shorthand where a tool block carries its
    full input and no fragments (the encoder synthesizes one full delta).
    """
    _validate_with(stream, allow_synthesized=True)


def _validate_with(stream: EventStream, allow_synthesized: bool) -> None:
    phase = _Phase.NEED_START
    next_index = 0
    # Currently open block: (index, "text" | _OpenTool)
    open_block: tuple[int, str | _OpenTool] | None = None

    for i, event in enumerate(stream.events):
        if isinstance(event, MessageStart):
            if phase != _Phase.NEED_START:
                raise Violation(i, "duplicate message_start")
            phase = _Phase.BLOCKS

        elif isinstance(event, ContentBlockStart):
            if phase != _Phase.BLOCKS:
                raise Violation(
                    i,
                    "content_block_start outside the block region (no message_start, or a message_delta already seen)",
                )
            if open_block is not None:
                raise Violation(i, "content_block_start with an open block")
            if event.index != next_index:
                raise Violation(
                    i,
                    f"content_block_start index {event.index}, want {next_index}",
                )
            next_index += 1

            if isinstance(event.block, TextBlock):
                open_block = (event.index, "text")
            elif isinstance(event.block, ToolUseBlock):
                open_block = (
                    event.index,
                    _OpenTool(
                        input=event.block.input,
                        fragments=[],
                        fragment_count=0,
                    ),
                )
            else:
                block_kind = getattr(event.block, "type", type(event.block).__name__)
                raise Violation(
                    i,
                    f"content_block_start carries {block_kind} block; streams carry text and tool_use only",
                )

        elif isinstance(event, ContentBlockDelta):
            if open_block is None:
                raise Violation(i, "content_block_delta without an open block")
            open_idx, kind = open_block
            if event.index != open_idx:
                raise Violation(
                    i,
                    f"content_block_delta index {event.index} does not match the open block {open_idx}",
                )
            if kind == "text":
                if not isinstance(event.delta, TextDelta):
                    raise Violation(
                        i,
                        f"delta type {getattr(event.delta, 'type', type(event.delta).__name__)} does not match the open block kind",
                    )
            elif isinstance(kind, _OpenTool):
                if not isinstance(event.delta, InputJsonDelta):
                    raise Violation(
                        i,
                        f"delta type {getattr(event.delta, 'type', type(event.delta).__name__)} does not match the open block kind",
                    )
                kind.fragments.append(event.delta.partial_json)
                kind.fragment_count += 1

        elif isinstance(event, ContentBlockStop):
            if open_block is None:
                raise Violation(i, "content_block_stop without an open block")
            open_idx, kind = open_block
            open_block = None
            if event.index != open_idx:
                raise Violation(
                    i,
                    f"content_block_stop index {event.index} does not match the open block {open_idx}",
                )
            if isinstance(kind, _OpenTool):
                _validate_tool_input(i, kind, allow_synthesized)

        elif isinstance(event, MessageDelta):
            if phase != _Phase.BLOCKS:
                raise Violation(
                    i,
                    "message_delta outside the block region (missing message_start or out of grammar order)",
                )
            if open_block is not None:
                raise Violation(i, "message_delta with an open block")
            if event.stop_sequence is not None and event.stop_reason != STOP_STOP_SEQUENCE:
                raise Violation(
                    i,
                    "stop_sequence is only permitted when stop_reason is stop_sequence",
                )
            phase = _Phase.NEED_MESSAGE_DONE

        elif isinstance(event, MessageDone):
            if phase == _Phase.DONE:
                raise Violation(i, "event after message_done")
            if phase != _Phase.NEED_MESSAGE_DONE:
                raise Violation(
                    i,
                    "message_done without an immediately preceding message_delta",
                )
            phase = _Phase.DONE

    if phase == _Phase.DONE:
        return

    total = len(stream.events)
    if open_block is not None:
        raise Violation(total, f"block index {open_block[0]} is not stopped")
    if phase == _Phase.NEED_MESSAGE_DONE:
        raise Violation(total, f"events: missing message_done (stream ends after {total} events)")
    raise Violation(total, "events: missing message_delta")


def _validate_tool_input(i: int, tool: _OpenTool, allow_synthesized: bool) -> None:
    if tool.fragment_count == 0:
        if not tool.input or allow_synthesized:
            return
        raise Violation(
            i,
            "tool block input without input_json_delta fragments; only encoder shorthand may synthesize them",
        )
    concatenated = "".join(tool.fragments)
    if tool.input != concatenated:
        raise Violation(
            i,
            f"tool block input does not equal the concatenation of its {tool.fragment_count} fragments (INV-1 exact text)",
        )
