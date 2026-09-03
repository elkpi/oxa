"""Chat Completions streaming decoder: converts chunks into IR events (face → IR)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

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
    TextBlock,
    TextDelta,
    ToolUseBlock,
    Usage,
)
from oxa.modelmap import Table
from oxa.openai.chatcompletions.constants import TOOL_TYPE_FUNCTION
from oxa.openai.chatcompletions.decode import decode_finish_reason
from oxa.openai.chatcompletions.normalize import loss


@dataclass(slots=True)
class _StreamToolCall:
    index: int
    id: str = ""
    name: str = ""
    fragments: list[str] = field(default_factory=list)
    skipped: bool = False


class StreamDecoder:
    """Incrementally converts a Chat Completions chunk stream into IR events."""

    __slots__ = (
        "_table",
        "_losses",
        "_started",
        "_text_open",
        "_text_index",
        "_next_ir_index",
        "_id",
        "_model",
        "_finish_seen",
        "_stop",
        "_usage",
        "_flushed",
        "_tool_calls",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._losses: list[Loss] = []
        self._started = False
        self._text_open = False
        self._text_index = 0
        self._next_ir_index = 0
        self._id = ""
        self._model = ""
        self._finish_seen = False
        self._stop: str | None = None
        self._usage: Usage | None = None
        self._flushed = False
        self._tool_calls: list[_StreamToolCall] = []

    def losses(self) -> list[Loss]:
        return list(self._losses)

    def feed(self, chunk: dict[str, Any], raw_text: str = "") -> list[Event]:
        """Pushes one wire chunk and returns any IR events it completes."""
        if self._flushed:
            raise ValueError("chatcompletions: chunk fed after stream flush")

        usage_raw = chunk.get("usage")
        if isinstance(usage_raw, dict):
            self._usage = Usage(
                input_tokens=int(usage_raw.get("prompt_tokens", 0)),
                output_tokens=int(usage_raw.get("completion_tokens", 0)),
            )

        choices = chunk.get("choices")
        if not choices or not isinstance(choices, list):
            return []

        choice = choices[0]
        delta = choice.get("delta", {})

        if self._started and delta.get("role"):
            if self._finish_seen:
                raise ValueError("chatcompletions: chunk stream restarted after finish_reason")
            raise ValueError("chatcompletions: chunk stream already started")

        events: list[Event] = []
        if not self._started:
            self._started = True
            self._id = str(chunk.get("id", ""))
            raw_model = str(chunk.get("model", ""))
            self._model = self._table.map(raw_model) if self._table else raw_model
            events.append(MessageStart(id=self._id, model=self._model))

        if "tool_calls" in delta and delta["tool_calls"] is not None:
            self._record_tool_calls(delta["tool_calls"])

        content = delta.get("content")
        if content is not None:
            if not self._text_open:
                self._text_open = True
                self._text_index = self._next_ir_index
                self._next_ir_index += 1
                events.append(ContentBlockStart(index=self._text_index, block=TextBlock(text="")))
            events.append(ContentBlockDelta(index=self._text_index, delta=TextDelta(text=content)))

        finish_reason = choice.get("finish_reason")
        if finish_reason is not None:
            if self._finish_seen:
                raise ValueError("chatcompletions: duplicate finish_reason")
            stop, finish_loss = decode_finish_reason(finish_reason)
            if finish_loss is not None:
                self._losses.append(finish_loss)
            self._stop = stop
            self._finish_seen = True

        return events

    def _record_tool_calls(self, calls: list[dict[str, Any]]) -> None:
        for call in calls:
            idx = int(call.get("index", 0))
            if idx > len(self._tool_calls):
                raise ValueError(
                    f"chatcompletions: tool_calls index {idx} is not the next consecutive native index"
                )
            if idx == len(self._tool_calls):
                self._tool_calls.append(_StreamToolCall(index=idx))

            record = self._tool_calls[idx]
            call_id = call.get("id")
            if call_id:
                if record.id and record.id != call_id:
                    raise ValueError(
                        f"chatcompletions: tool_calls[{idx}] has conflicting IDs {record.id!r} and {call_id!r}"
                    )
                record.id = call_id

            kind = call.get("type")
            if kind is not None and kind != TOOL_TYPE_FUNCTION and not record.skipped:
                self._losses.append(
                    loss(
                        f"choices[0].delta.tool_calls[{idx}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Chat Completions streamed tool type {kind!r} has no IR equivalent",
                    )
                )
                record.skipped = True

            if record.skipped:
                continue

            func = call.get("function")
            if isinstance(func, dict):
                if func.get("name"):
                    record.name += func["name"]
                if func.get("arguments") is not None:
                    record.fragments.append(func["arguments"])

    def flush(self) -> list[Event]:
        """Closes the stream and returns terminal stop and message events."""
        if self._flushed:
            raise ValueError("chatcompletions: stream flushed twice")
        if self._stop is None:
            raise ValueError("chatcompletions: stream ended without finish_reason")
        self._flushed = True

        events: list[Event] = []
        if self._text_open:
            events.append(ContentBlockStop(index=self._text_index))
            self._text_open = False

        for call in self._tool_calls:
            if call.skipped:
                continue
            if not call.id:
                raise ValueError(
                    f"chatcompletions: tool_calls[{call.index}] is missing final ID"
                )
            if not call.name:
                raise ValueError(
                    f"chatcompletions: tool_calls[{call.index}] is missing final function name"
                )
            idx = self._next_ir_index
            self._next_ir_index += 1
            full_input = "".join(call.fragments)
            events.append(
                ContentBlockStart(
                    index=idx,
                    block=ToolUseBlock(
                        id=call.id,
                        name=call.name,
                        input=full_input,
                    ),
                )
            )
            for fragment in call.fragments:
                events.append(
                    ContentBlockDelta(
                        index=idx,
                        delta=InputJsonDelta(partial_json=fragment),
                    )
                )
            events.append(ContentBlockStop(index=idx))

        usage = self._usage or Usage(input_tokens=0, output_tokens=0)
        events.append(
            MessageDelta(
                stop_reason=self._stop,
                usage=usage,
            )
        )
        events.append(MessageDone())
        return events
