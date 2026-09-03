"""Anthropic Messages streaming decoder: converts wire stream events into IR events (face → IR)."""

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
)
from oxa.anthropic.messages.decode import decode_stop_reason
from oxa.anthropic.messages.normalize import extract_tool_input_raw, loss
from oxa.ir import (
    LOSS_UNSUPPORTED_SEMANTIC,
    Block,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Event,
    InputJsonDelta,
    Loss,
    MessageDelta,
    MessageDone,
    MessageStart,
    STOP_END_TURN,
    TextBlock,
    TextDelta,
    ToolUseBlock,
    Usage,
)
from oxa.modelmap import Table


class StreamDecoder:
    """Incrementally converts an Anthropic Messages event stream into IR events."""

    __slots__ = (
        "_table",
        "_losses",
        "_started",
        "_next_index",
        "_next_ir_index",
        "_open_index",
        "_open_ir_index",
        "_block_open",
        "_open_tool",
        "_tool_id",
        "_tool_name",
        "_tool_input",
        "_tool_parts",
        "_skipped_open",
        "_skipped",
        "_delta_seen",
        "_stop",
        "_stop_seq",
        "_usage",
        "_stopped",
        "_flushed",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._losses: list[Loss] = []
        self._started = False
        self._next_index = 0
        self._next_ir_index = 0
        self._open_index = 0
        self._open_ir_index = 0
        self._block_open = False
        self._open_tool = False
        self._tool_id = ""
        self._tool_name = ""
        self._tool_input = ""
        self._tool_parts: list[str] = []
        self._skipped_open = False
        self._skipped: set[int] = set()
        self._delta_seen = False
        self._stop: str | None = None
        self._stop_seq: str | None = None
        self._usage = Usage(input_tokens=0, output_tokens=0)
        self._stopped = False
        self._flushed = False

    def losses(self) -> list[Loss]:
        return list(self._losses)

    def feed(self, ev: dict[str, Any], raw_text: str = "") -> list[Event]:
        """Pushes one wire stream event and returns any completed IR events."""
        if self._flushed:
            raise ValueError("anthropic: event fed after stream flush")
        if self._stopped:
            raise ValueError("anthropic: event fed after message_stop")

        kind = ev.get("type", "")

        if kind == EVENT_TYPE_MESSAGE_START:
            if self._started:
                raise ValueError("anthropic: duplicate message_start")
            msg = ev.get("message")
            if not isinstance(msg, dict):
                raise ValueError("anthropic: message_start without message")
            self._started = True
            raw_model = str(msg.get("model", ""))
            model = self._table.map(raw_model) if self._table else raw_model
            return [MessageStart(id=str(msg.get("id", "")), model=model)]

        if kind == EVENT_TYPE_CONTENT_BLOCK_START:
            if not self._started:
                raise ValueError("anthropic: content_block_start before message_start")
            if self._block_open or self._skipped_open:
                raise ValueError("anthropic: content_block_start with a block still open")
            index = int(ev.get("index", -1))
            if index != self._next_index:
                raise ValueError(
                    f"anthropic: content_block_start index {index}, want {self._next_index}"
                )

            block = ev.get("content_block")
            if not isinstance(block, dict):
                self._next_index += 1
                self._skipped.add(index)
                self._skipped_open = True
                self._losses.append(
                    loss(
                        f"content_block_start[{index}].content_block.type",
                        "content_block.type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "Anthropic streaming block has no content_block payload; the index is skipped",
                    )
                )
                return []

            b_kind = block.get("type", "")
            if b_kind == BLOCK_TYPE_TOOL_USE:
                tool_id = block.get("id", "")
                name = block.get("name", "")
                if not tool_id:
                    raise ValueError(
                        f"anthropic: content_block_start[{index}].content_block.id is required"
                    )
                if not name:
                    raise ValueError(
                        f"anthropic: content_block_start[{index}].content_block.name is required"
                    )
                self._next_index += 1
                self._block_open = True
                self._open_tool = True
                self._open_index = index
                self._open_ir_index = self._next_ir_index
                self._next_ir_index += 1
                self._tool_id = tool_id
                self._tool_name = name

                raw_input = block.get("input")
                raw_slice = extract_tool_input_raw(raw_text, tool_id)
                if raw_slice is not None:
                    self._tool_input = raw_slice
                elif isinstance(raw_input, dict):
                    self._tool_input = json.dumps(raw_input, separators=(",", ":"), ensure_ascii=False)
                elif raw_input is not None:
                    self._tool_input = str(raw_input)
                else:
                    self._tool_input = ""

                self._tool_parts.clear()
                return []

            if b_kind != BLOCK_TYPE_TEXT:
                self._next_index += 1
                self._skipped.add(index)
                self._skipped_open = True
                self._losses.append(
                    loss(
                        f"content_block_start[{index}].content_block.type",
                        "content_block.type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Anthropic streaming block type {b_kind!r} is not decodable in M7; the index is skipped",
                    )
                )
                return []

            self._next_index += 1
            self._block_open = True
            self._open_tool = False
            self._open_index = index
            self._open_ir_index = self._next_ir_index
            self._next_ir_index += 1
            return [ContentBlockStart(index=self._open_ir_index, block=TextBlock(text=block.get("text", "")))]

        if kind == EVENT_TYPE_CONTENT_BLOCK_DELTA:
            if not self._started:
                raise ValueError("anthropic: content_block_delta before message_start")
            delta = ev.get("delta")
            if not isinstance(delta, dict):
                raise ValueError("anthropic: content_block_delta without delta")
            index = int(ev.get("index", -1))
            if index in self._skipped:
                return []
            if not self._block_open or index != self._open_index:
                raise ValueError(
                    f"anthropic: content_block_delta index {index} does not match the open block"
                )

            d_kind = delta.get("type", "")
            if self._open_tool:
                if d_kind == DELTA_TYPE_TEXT_DELTA:
                    raise ValueError("anthropic: text_delta on tool_use block")
                if d_kind == DELTA_TYPE_INPUT_JSON_DELTA:
                    self._tool_parts.append(delta.get("partial_json", ""))
                    return []
            else:
                if d_kind == DELTA_TYPE_TEXT_DELTA:
                    return [ContentBlockDelta(index=self._open_ir_index, delta=TextDelta(text=delta.get("text", "")))]
                if d_kind == DELTA_TYPE_INPUT_JSON_DELTA:
                    raise ValueError("anthropic: input_json_delta on non-tool block")

            self._losses.append(
                loss(
                    f"content_block_delta[{index}].delta.type",
                    "delta.type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Anthropic delta type {d_kind!r} has no IR equivalent",
                )
            )
            return []

        if kind == EVENT_TYPE_CONTENT_BLOCK_STOP:
            if not self._started:
                raise ValueError("anthropic: content_block_stop before message_start")
            index = int(ev.get("index", -1))
            if index in self._skipped:
                self._skipped.remove(index)
                self._skipped_open = False
                return []
            if not self._block_open or index != self._open_index:
                raise ValueError(
                    f"anthropic: content_block_stop index {index} does not match the open block"
                )

            if self._open_tool:
                if self._tool_parts:
                    full_input = "".join(self._tool_parts)
                    deltas = [
                        ContentBlockDelta(index=self._open_ir_index, delta=InputJsonDelta(partial_json=part))
                        for part in self._tool_parts
                    ]
                else:
                    if not self._tool_input:
                        raise ValueError("anthropic: tool_use input is required")
                    full_input = self._tool_input
                    deltas = [
                        ContentBlockDelta(index=self._open_ir_index, delta=InputJsonDelta(partial_json=full_input))
                    ]

                events = [
                    ContentBlockStart(
                        index=self._open_ir_index,
                        block=ToolUseBlock(
                            id=self._tool_id,
                            name=self._tool_name,
                            input=full_input,
                        ),
                    )
                ]
                events.extend(deltas)
                events.append(ContentBlockStop(index=self._open_ir_index))
                self._block_open = False
                self._open_tool = False
                self._tool_id = ""
                self._tool_name = ""
                self._tool_input = ""
                self._tool_parts.clear()
                return events

            self._block_open = False
            return [ContentBlockStop(index=self._open_ir_index)]

        if kind == EVENT_TYPE_MESSAGE_DELTA:
            if not self._started:
                raise ValueError("anthropic: message_delta before message_start")
            if self._block_open or self._skipped_open:
                raise ValueError("anthropic: message_delta with a block still open")
            delta = ev.get("delta")
            if not isinstance(delta, dict):
                raise ValueError("anthropic: message_delta without delta")

            stop, stop_loss = decode_stop_reason(delta.get("stop_reason", ""))
            if stop_loss is not None:
                self._losses.append(stop_loss)
            self._stop = stop
            self._stop_seq = delta.get("stop_sequence")

            usage_raw = ev.get("usage")
            if isinstance(usage_raw, dict):
                self._usage = Usage(
                    input_tokens=int(usage_raw.get("input_tokens", 0)),
                    output_tokens=int(usage_raw.get("output_tokens", 0)),
                )
            self._delta_seen = True
            return []

        if kind == EVENT_TYPE_MESSAGE_STOP:
            if not self._started:
                raise ValueError("anthropic: message_stop before message_start")
            if self._block_open or self._skipped_open:
                raise ValueError("anthropic: message_stop with a block still open")
            if not self._delta_seen:
                raise ValueError("anthropic: message_stop without preceding message_delta")

            self._stopped = True
            return [
                MessageDelta(
                    stop_reason=self._stop or STOP_END_TURN,
                    usage=self._usage,
                    stop_sequence=self._stop_seq,
                ),
                MessageDone(),
            ]

        return []

    def flush(self) -> list[Event]:
        if self._flushed:
            raise ValueError("anthropic: stream flushed twice")
        if not self._stopped:
            raise ValueError("anthropic: stream ended without message_stop")
        self._flushed = True
        return []
