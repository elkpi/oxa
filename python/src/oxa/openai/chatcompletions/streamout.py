"""Chat Completions streaming encoder: converts IR events into chunks (IR → face)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from oxa.ir import (
    LOSS_DEGRADED,
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
from oxa.openai.chatcompletions.constants import (
    OBJECT_CHAT_COMPLETION_CHUNK,
    ROLE_ASSISTANT,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.chatcompletions.encode import encode_finish_reason
from oxa.openai.chatcompletions.normalize import loss


@dataclass(slots=True)
class _StreamEncodeBlock:
    kind: str  # "text" or "tool"
    index: int
    tool_id: str = ""
    tool_name: str = ""
    tool_input: str = ""
    fragments: list[str] = field(default_factory=list)
    native_index: int = 0
    tool_started: bool = False


class StreamEncoder:
    """Incrementally converts an IR event stream into Chat Completions chunks."""

    __slots__ = (
        "_table",
        "_id",
        "_model",
        "_started",
        "_active",
        "_next_ir_index",
        "_next_native_tool",
        "_tool_seen",
        "_ordering_degrade",
        "_pending_tools",
        "_finished",
        "_done",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._id = ""
        self._model = ""
        self._started = False
        self._active: _StreamEncodeBlock | None = None
        self._next_ir_index = 0
        self._next_native_tool = 0
        self._tool_seen = False
        self._ordering_degrade = False
        self._pending_tools: list[dict[str, Any]] = []
        self._finished = False
        self._done = False

    def apply(self, ev: Event) -> tuple[list[dict[str, Any]], list[Loss]]:
        """Pushes one IR event and returns the chunks and losses it produces."""
        if self._done or (self._finished and not isinstance(ev, MessageDone)):
            raise ValueError("chatcompletions: event applied after stream termination")

        if isinstance(ev, MessageStart):
            if self._started:
                raise ValueError("chatcompletions: duplicate MessageStart")
            self._started = True
            self._id = ev.id
            self._model = self._table.map(ev.model) if self._table else ev.model
            chunk = self._chunk({"role": ROLE_ASSISTANT})
            return [chunk], []

        if isinstance(ev, ContentBlockStart):
            if not self._started or self._active is not None:
                raise ValueError("chatcompletions: ContentBlockStart out of grammar order")
            if ev.index != self._next_ir_index:
                raise ValueError(
                    f"chatcompletions: ContentBlockStart index {ev.index}, want {self._next_ir_index}"
                )
            self._next_ir_index += 1

            if isinstance(ev.block, TextBlock):
                if self._tool_seen:
                    self._ordering_degrade = True
                self._active = _StreamEncodeBlock(
                    kind="text",
                    index=ev.index,
                )
                return [], []
            if isinstance(ev.block, ToolUseBlock):
                if not ev.block.id or not ev.block.name:
                    raise ValueError("chatcompletions: ToolUseBlock requires nonempty ID and name")
                native_idx = self._next_native_tool
                self._next_native_tool += 1
                self._tool_seen = True
                self._active = _StreamEncodeBlock(
                    kind="tool",
                    index=ev.index,
                    tool_id=ev.block.id,
                    tool_name=ev.block.name,
                    tool_input=ev.block.input,
                    native_index=native_idx,
                )
                return [], []
            raise ValueError("chatcompletions: ContentBlockStart carries unsupported block")

        if isinstance(ev, ContentBlockDelta):
            if self._active is None or ev.index != self._active.index:
                raise ValueError("chatcompletions: ContentBlockDelta out of grammar order")

            if self._active.kind == "text":
                if not isinstance(ev.delta, TextDelta):
                    raise ValueError("chatcompletions: TextBlock received non-text delta")
                chunk = self._chunk({"content": ev.delta.text})
                return [chunk], []
            if self._active.kind == "tool":
                if not isinstance(ev.delta, InputJsonDelta):
                    raise ValueError("chatcompletions: ToolUseBlock received non-input-json delta")
                self._active.fragments.append(ev.delta.partial_json)
                chunk = self._make_tool_argument_chunk(self._active, ev.delta.partial_json)
                self._pending_tools.append(chunk)
                return [], []

        if isinstance(ev, ContentBlockStop):
            if self._active is None or ev.index != self._active.index:
                raise ValueError("chatcompletions: ContentBlockStop out of grammar order")

            active = self._active
            self._active = None

            if active.kind == "tool":
                if not active.fragments:
                    # Synthesize full argument delta
                    full = active.tool_input
                    active.fragments.append(full)
                    chunk = self._make_tool_argument_chunk(active, full)
                    self._pending_tools.append(chunk)

                if "".join(active.fragments) != active.tool_input:
                    raise ValueError(
                        "chatcompletions: ToolUseBlock input does not equal concatenated InputJSONDelta fragments"
                    )
            return [], []

        if isinstance(ev, MessageDelta):
            if not self._started or self._active is not None:
                raise ValueError("chatcompletions: MessageDelta out of grammar order")

            finish, finish_loss = encode_finish_reason(ev.stop_reason)
            losses: list[Loss] = []
            if finish_loss is not None:
                losses.append(finish_loss)

            if self._ordering_degrade:
                losses.append(
                    loss(
                        "events",
                        "ordering",
                        LOSS_DEGRADED,
                        "N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved",
                    )
                )

            self._finished = True
            chunks = list(self._pending_tools)
            self._pending_tools.clear()

            chunks.append(
                {
                    "id": self._id,
                    "object": OBJECT_CHAT_COMPLETION_CHUNK,
                    "created": 0,
                    "model": self._model,
                    "choices": [
                        {
                            "index": 0,
                            "delta": {},
                            "finish_reason": finish,
                        }
                    ],
                    "usage": {
                        "prompt_tokens": ev.usage.input_tokens,
                        "completion_tokens": ev.usage.output_tokens,
                        "total_tokens": ev.usage.input_tokens + ev.usage.output_tokens,
                    },
                }
            )
            return chunks, losses

        if isinstance(ev, MessageDone):
            if not self._finished:
                raise ValueError("chatcompletions: MessageDone out of grammar order")
            self._done = True
            return [], []

        raise ValueError(f"chatcompletions: unknown event type {type(ev)}")

    def _chunk(self, delta: dict[str, Any]) -> dict[str, Any]:
        return {
            "id": self._id,
            "object": OBJECT_CHAT_COMPLETION_CHUNK,
            "created": 0,
            "model": self._model,
            "choices": [
                {
                    "index": 0,
                    "delta": delta,
                    "finish_reason": None,
                }
            ],
        }

    def _make_tool_argument_chunk(self, block: _StreamEncodeBlock, fragment: str) -> dict[str, Any]:
        call_delta: dict[str, Any] = {
            "index": block.native_index,
        }
        func: dict[str, Any] = {"arguments": fragment}

        if not block.tool_started:
            call_delta["id"] = block.tool_id
            call_delta["type"] = TOOL_TYPE_FUNCTION
            func["name"] = block.tool_name

        call_delta["function"] = func
        block.tool_started = True

        return {
            "id": self._id,
            "object": OBJECT_CHAT_COMPLETION_CHUNK,
            "created": 0,
            "model": self._model,
            "choices": [
                {
                    "index": 0,
                    "delta": {
                        "tool_calls": [call_delta],
                    },
                    "finish_reason": None,
                }
            ],
        }
