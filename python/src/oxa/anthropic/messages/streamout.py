"""Anthropic Messages streaming encoder: converts IR events into wire stream events (IR → face)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_TEXT,
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
    TYPE_MESSAGE,
)
from oxa.anthropic.messages.encode import encode_stop_reason
from oxa.ir import (
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Event,
    InputJsonDelta,
    Loss,
    MessageDelta,
    MessageDone,
    MessageStart,
    TextBlock,
    TextDelta,
    ToolUseBlock,
)
from oxa.modelmap import Table


class StreamEncoder:
    """Incrementally converts an IR event stream into Anthropic Messages wire events."""

    __slots__ = (
        "_table",
        "_id",
        "_model",
        "_started",
        "_open_index",
        "_open_tool",
        "_tool_input",
        "_tool_parts",
        "_delta_seen",
        "_done",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._id = ""
        self._model = ""
        self._started = False
        self._open_index = -1
        self._open_tool = False
        self._tool_input = ""
        self._tool_parts: list[str] = []
        self._delta_seen = False
        self._done = False

    def apply(self, ev: Event) -> tuple[list[dict[str, Any]], list[Loss]]:
        """Pushes one IR event and returns the emitted wire events and losses."""
        if self._done:
            raise ValueError("anthropic: event applied after message_done")

        if isinstance(ev, MessageStart):
            if self._started:
                raise ValueError("anthropic: duplicate MessageStart")
            self._started = True
            self._id = ev.id
            self._model = self._table.map(ev.model) if self._table else ev.model
            return [
                {
                    "type": EVENT_TYPE_MESSAGE_START,
                    "message": {
                        "id": self._id,
                        "type": TYPE_MESSAGE,
                        "role": ROLE_ASSISTANT,
                        "model": self._model,
                        "content": [],
                        "stop_reason": None,
                        "usage": {"input_tokens": 0, "output_tokens": 0},
                    },
                }
            ], []

        if isinstance(ev, ContentBlockStart):
            if not self._started or self._open_index >= 0:
                raise ValueError("anthropic: ContentBlockStart out of grammar order")

            if isinstance(ev.block, TextBlock):
                self._open_index = ev.index
                self._open_tool = False
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": ev.index,
                        "content_block": {
                            "type": BLOCK_TYPE_TEXT,
                            "text": ev.block.text,
                        },
                    }
                ], []

            if isinstance(ev.block, ToolUseBlock):
                if not ev.block.id or not ev.block.name:
                    raise ValueError("anthropic: ToolUseBlock requires nonempty ID and name")
                self._open_index = ev.index
                self._open_tool = True
                self._tool_input = ev.block.input
                self._tool_parts.clear()
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": ev.index,
                        "content_block": {
                            "type": BLOCK_TYPE_TOOL_USE,
                            "id": ev.block.id,
                            "name": ev.block.name,
                            "input": {},
                        },
                    }
                ], []

            raise ValueError("anthropic: ContentBlockStart carries unsupported block")

        if isinstance(ev, ContentBlockDelta):
            if self._open_index != ev.index:
                raise ValueError("anthropic: ContentBlockDelta out of grammar order")

            if self._open_tool:
                if not isinstance(ev.delta, InputJsonDelta):
                    raise ValueError("anthropic: ToolUseBlock received non-input-json delta")
                self._tool_parts.append(ev.delta.partial_json)
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                        "index": ev.index,
                        "delta": {
                            "type": DELTA_TYPE_INPUT_JSON_DELTA,
                            "partial_json": ev.delta.partial_json,
                        },
                    }
                ], []

            if not isinstance(ev.delta, TextDelta):
                raise ValueError("anthropic: TextBlock received non-text delta")
            return [
                {
                    "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                    "index": ev.index,
                    "delta": {
                        "type": DELTA_TYPE_TEXT_DELTA,
                        "text": ev.delta.text,
                    },
                }
            ], []

        if isinstance(ev, ContentBlockStop):
            if self._open_index != ev.index:
                raise ValueError("anthropic: ContentBlockStop out of grammar order")

            events: list[dict[str, Any]] = []
            if self._open_tool:
                if not self._tool_parts:
                    events.append(
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": ev.index,
                            "delta": {
                                "type": DELTA_TYPE_INPUT_JSON_DELTA,
                                "partial_json": self._tool_input,
                            },
                        }
                    )
                else:
                    if "".join(self._tool_parts) != self._tool_input:
                        raise ValueError(
                            "anthropic: ToolUseBlock input does not equal concatenated InputJSONDelta fragments"
                        )
                self._open_tool = False
                self._tool_input = ""
                self._tool_parts.clear()

            self._open_index = -1
            events.append(
                {
                    "type": EVENT_TYPE_CONTENT_BLOCK_STOP,
                    "index": ev.index,
                }
            )
            return events, []

        if isinstance(ev, MessageDelta):
            if not self._started or self._open_index >= 0:
                raise ValueError("anthropic: MessageDelta out of grammar order")
            reason, seq = encode_stop_reason(ev.stop_reason, ev.stop_sequence)
            self._delta_seen = True
            delta_dict: dict[str, Any] = {"stop_reason": reason}
            if seq is not None:
                delta_dict["stop_sequence"] = seq
            return [
                {
                    "type": EVENT_TYPE_MESSAGE_DELTA,
                    "delta": delta_dict,
                    "usage": {
                        "input_tokens": ev.usage.input_tokens,
                        "output_tokens": ev.usage.output_tokens,
                    },
                }
            ], []

        if isinstance(ev, MessageDone):
            if not self._delta_seen:
                raise ValueError("anthropic: MessageDone out of grammar order")
            self._done = True
            return [{"type": EVENT_TYPE_MESSAGE_STOP}], []

        raise ValueError(f"anthropic: unknown event type {type(ev)}")
