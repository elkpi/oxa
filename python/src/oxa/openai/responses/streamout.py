"""OpenAI Responses streaming encoder: converts IR events into Responses streaming events (IR → face)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from oxa.ir import (
    LOSS_UNMAPPED_VALUE,
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
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    TextBlock,
    TextDelta,
    ToolUseBlock,
)
from oxa.modelmap import Table
from oxa.openai.responses.constants import (
    ERROR_CODE_REFUSAL,
    EVENT_TYPE_RESPONSE_COMPLETED,
    EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED,
    EVENT_TYPE_RESPONSE_CONTENT_PART_DONE,
    EVENT_TYPE_RESPONSE_CREATED,
    EVENT_TYPE_RESPONSE_FAILED,
    EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA,
    EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE,
    EVENT_TYPE_RESPONSE_INCOMPLETE,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE,
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_MESSAGE,
    OBJECT_RESPONSE,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_IN_PROGRESS,
    STATUS_INCOMPLETE,
)
from oxa.openai.responses.normalize import loss


@dataclass(slots=True)
class _StreamOutputItem:
    kind: str  # "message" or "function_call"
    id: str
    output_index: int
    content: list[dict[str, Any]] = field(default_factory=list)
    next_content_index: int = 0
    call_id: str = ""
    name: str = ""


@dataclass(slots=True)
class _StreamEncodeBlock:
    index: int
    kind: str
    content_index: int
    text: str = ""
    tool_input: str = ""
    fragments: list[str] = field(default_factory=list)


def _stream_generated_item_id(prefix: str, ordinal: int) -> str:
    return f"{prefix}_abc{123 + 333 * ordinal:03}"


class StreamEncoder:
    """Incrementally converts an IR event stream into OpenAI Responses streaming events."""

    __slots__ = (
        "_table",
        "_id",
        "_model",
        "_started",
        "_delta",
        "_done",
        "_next_block_index",
        "_next_output_index",
        "_next_message_item",
        "_next_function_item",
        "_active_item",
        "_active_block",
        "_completed",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._id = ""
        self._model = ""
        self._started = False
        self._delta = False
        self._done = False
        self._next_block_index = 0
        self._next_output_index = 0
        self._next_message_item = 0
        self._next_function_item = 0
        self._active_item: _StreamOutputItem | None = None
        self._active_block: _StreamEncodeBlock | None = None
        self._completed: list[dict[str, Any]] = []

    def apply(self, ev: Event) -> tuple[list[dict[str, Any]], list[Loss]]:
        """Pushes one IR event and returns the emitted wire events and losses."""
        if self._done or (self._delta and not isinstance(ev, MessageDone)):
            raise ValueError("responses: event applied after stream termination")

        if isinstance(ev, MessageStart):
            if self._started:
                raise ValueError("responses: duplicate MessageStart")
            self._started = True
            self._id = ev.id
            self._model = self._table.map(ev.model) if self._table else ev.model
            return [
                {
                    "type": EVENT_TYPE_RESPONSE_CREATED,
                    "response": {
                        "id": self._id,
                        "object": OBJECT_RESPONSE,
                        "status": STATUS_IN_PROGRESS,
                        "model": self._model,
                        "output": [],
                    },
                }
            ], []

        if isinstance(ev, ContentBlockStart):
            if not self._started or self._active_block is not None or self._delta:
                raise ValueError("responses: ContentBlockStart out of grammar order")
            if ev.index != self._next_block_index:
                raise ValueError(
                    f"responses: ContentBlockStart index {ev.index}, want {self._next_block_index}"
                )
            self._next_block_index += 1
            if isinstance(ev.block, TextBlock):
                return self._start_text_block(ev.index, ev.block.text)
            if isinstance(ev.block, ToolUseBlock):
                return self._start_function_call_block(
                    ev.index, ev.block.id, ev.block.name, ev.block.input
                )
            raise ValueError("responses: ContentBlockStart carries unsupported block")

        if isinstance(ev, ContentBlockDelta):
            if self._active_block is None or ev.index != self._active_block.index:
                raise ValueError("responses: ContentBlockDelta out of grammar order")
            active_item = self._active_item
            assert active_item is not None

            if self._active_block.kind == "message":
                if not isinstance(ev.delta, TextDelta):
                    raise ValueError("responses: TextBlock received non-text delta")
                self._active_block.text += ev.delta.text
                return [
                    {
                        "type": EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA,
                        "item_id": active_item.id,
                        "output_index": active_item.output_index,
                        "content_index": self._active_block.content_index,
                        "delta": ev.delta.text,
                    }
                ], []

            if self._active_block.kind == "function_call":
                if not isinstance(ev.delta, InputJsonDelta):
                    raise ValueError("responses: ToolUseBlock received non-input-json delta")
                self._active_block.fragments.append(ev.delta.partial_json)
                return [
                    {
                        "type": EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA,
                        "item_id": active_item.id,
                        "output_index": active_item.output_index,
                        "delta": ev.delta.partial_json,
                    }
                ], []

        if isinstance(ev, ContentBlockStop):
            if self._active_block is None or ev.index != self._active_block.index:
                raise ValueError("responses: ContentBlockStop out of grammar order")
            if self._active_block.kind == "message":
                return self._stop_text_block()
            return self._stop_function_call_block()

        if isinstance(ev, MessageDelta):
            if not self._started or self._active_block is not None or self._delta:
                raise ValueError("responses: MessageDelta out of grammar order")
            out: list[dict[str, Any]] = []
            if self._active_item is not None:
                if self._active_item.kind != "message":
                    raise ValueError(
                        "responses: MessageDelta with an uncompleted function_call item"
                    )
                out.append(self._close_message_item())

            terminal, losses = self._terminal(ev)
            resp = terminal.get("response")
            if isinstance(resp, dict):
                resp["output"] = list(self._completed)
            self._delta = True
            out.append(terminal)
            return out, losses

        if isinstance(ev, MessageDone):
            if not self._delta:
                raise ValueError("responses: MessageDone out of grammar order")
            self._done = True
            return [], []

        raise ValueError(f"responses: unknown event type {type(ev)}")

    def _start_text_block(
        self,
        index: int,
        text: str,
    ) -> tuple[list[dict[str, Any]], list[Loss]]:
        out: list[dict[str, Any]] = []
        if self._active_item is None:
            item, added = self._open_message_item()
            self._active_item = item
            out.append(added)

        active = self._active_item
        assert active is not None
        if active.kind != "message":
            raise ValueError(
                "responses: TextBlock cannot open before the active function_call item completes"
            )

        content_index = active.next_content_index
        active.next_content_index += 1
        part: dict[str, Any] = {
            "type": PART_TYPE_OUTPUT_TEXT,
            "text": text,
            "annotations": [],
        }
        active.content.append(dict(part))
        self._active_block = _StreamEncodeBlock(
            index=index,
            kind="message",
            content_index=content_index,
            text=text,
        )
        out.append(
            {
                "type": EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED,
                "item_id": active.id,
                "output_index": active.output_index,
                "content_index": content_index,
                "part": dict(part),
            }
        )
        return out, []

    def _start_function_call_block(
        self,
        index: int,
        tool_id: str,
        name: str,
        input_text: str,
    ) -> tuple[list[dict[str, Any]], list[Loss]]:
        if not tool_id or not name:
            raise ValueError("responses: ToolUseBlock requires nonempty ID and name")
        out: list[dict[str, Any]] = []
        if self._active_item is not None:
            if self._active_item.kind != "message":
                raise ValueError(
                    "responses: ToolUseBlock cannot open before the active function_call item completes"
                )
            out.append(self._close_message_item())

        item, added = self._open_function_call_item(tool_id, name)
        self._active_item = item
        self._active_block = _StreamEncodeBlock(
            index=index,
            kind="function_call",
            content_index=0,
            tool_input=input_text,
        )
        out.append(added)
        return out, []

    def _stop_text_block(self) -> tuple[list[dict[str, Any]], list[Loss]]:
        if self._active_item is None or self._active_item.kind != "message":
            raise ValueError("responses: text block without an active message item")
        assert self._active_block is not None
        block = self._active_block
        self._active_block = None

        content_idx = block.content_index
        part = {
            "type": PART_TYPE_OUTPUT_TEXT,
            "text": block.text,
            "annotations": [],
        }
        self._active_item.content[content_idx] = part
        item_id = self._active_item.id
        output_index = self._active_item.output_index

        return [
            {
                "type": EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE,
                "item_id": item_id,
                "output_index": output_index,
                "content_index": block.content_index,
                "text": block.text,
            },
            {
                "type": EVENT_TYPE_RESPONSE_CONTENT_PART_DONE,
                "item_id": item_id,
                "output_index": output_index,
                "content_index": block.content_index,
                "part": part,
            },
        ], []

    def _stop_function_call_block(self) -> tuple[list[dict[str, Any]], list[Loss]]:
        if self._active_item is None or self._active_item.kind != "function_call":
            raise ValueError("responses: tool block without an active function_call item")
        assert self._active_block is not None
        block = self._active_block
        self._active_block = None

        out: list[dict[str, Any]] = []
        if not block.fragments:
            block.fragments.append(block.tool_input)
            out.append(
                {
                    "type": EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA,
                    "item_id": self._active_item.id,
                    "output_index": self._active_item.output_index,
                    "delta": block.tool_input,
                }
            )

        arguments = "".join(block.fragments)
        if arguments != block.tool_input:
            raise ValueError(
                "responses: ToolUseBlock input does not equal concatenated InputJSONDelta fragments"
            )

        completed: dict[str, Any] = {
            "id": self._active_item.id,
            "type": ITEM_TYPE_FUNCTION_CALL,
            "status": STATUS_COMPLETED,
            "call_id": self._active_item.call_id,
            "name": self._active_item.name,
            "arguments": arguments,
        }
        out.append(
            {
                "type": EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE,
                "item_id": self._active_item.id,
                "output_index": self._active_item.output_index,
                "call_id": self._active_item.call_id,
                "name": self._active_item.name,
                "arguments": arguments,
            }
        )
        out.append(
            {
                "type": EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE,
                "output_index": self._active_item.output_index,
                "item": completed,
            }
        )
        self._completed.append(completed)
        self._active_item = None
        return out, []

    def _open_message_item(self) -> tuple[_StreamOutputItem, dict[str, Any]]:
        item_id = _stream_generated_item_id("msg", self._next_message_item)
        self._next_message_item += 1
        output_index = self._next_output_index
        self._next_output_index += 1
        item = _StreamOutputItem(
            kind="message",
            id=item_id,
            output_index=output_index,
        )
        event = {
            "type": EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
            "output_index": output_index,
            "item": {
                "id": item_id,
                "type": ITEM_TYPE_MESSAGE,
                "status": STATUS_IN_PROGRESS,
                "role": ROLE_ASSISTANT,
            },
        }
        return item, event

    def _open_function_call_item(self, call_id: str, name: str) -> tuple[_StreamOutputItem, dict[str, Any]]:
        item_id = _stream_generated_item_id("fc", self._next_function_item)
        self._next_function_item += 1
        output_index = self._next_output_index
        self._next_output_index += 1
        item = _StreamOutputItem(
            kind="function_call",
            id=item_id,
            output_index=output_index,
            call_id=call_id,
            name=name,
        )
        event = {
            "type": EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
            "output_index": output_index,
            "item": {
                "id": item_id,
                "type": ITEM_TYPE_FUNCTION_CALL,
                "status": STATUS_IN_PROGRESS,
                "call_id": call_id,
                "name": name,
            },
        }
        return item, event

    def _close_message_item(self) -> dict[str, Any]:
        item = self._active_item
        assert item is not None
        self._active_item = None
        completed: dict[str, Any] = {
            "id": item.id,
            "type": ITEM_TYPE_MESSAGE,
            "status": STATUS_COMPLETED,
            "role": ROLE_ASSISTANT,
            "content": item.content,
        }
        event = {
            "type": EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE,
            "output_index": item.output_index,
            "item": completed,
        }
        self._completed.append(completed)
        return event

    def _terminal(self, delta: MessageDelta) -> tuple[dict[str, Any], list[Loss]]:
        resp: dict[str, Any] = {
            "id": self._id,
            "object": OBJECT_RESPONSE,
            "model": self._model,
            "output": [],
            "usage": {
                "input_tokens": delta.usage.input_tokens,
                "output_tokens": delta.usage.output_tokens,
                "total_tokens": delta.usage.input_tokens + delta.usage.output_tokens,
            },
        }

        if delta.stop_reason in (STOP_END_TURN, STOP_TOOL_USE):
            resp["status"] = STATUS_COMPLETED
            return {"type": EVENT_TYPE_RESPONSE_COMPLETED, "response": resp}, []

        if delta.stop_reason == STOP_MAX_TOKENS:
            resp["status"] = STATUS_INCOMPLETE
            resp["incomplete_details"] = {"reason": INCOMPLETE_REASON_MAX_OUTPUT_TOKENS}
            return {"type": EVENT_TYPE_RESPONSE_INCOMPLETE, "response": resp}, []

        if delta.stop_reason == STOP_REFUSAL:
            resp["status"] = STATUS_FAILED
            resp["error"] = {"code": ERROR_CODE_REFUSAL, "message": ""}
            return {"type": EVENT_TYPE_RESPONSE_FAILED, "response": resp}, []

        if delta.stop_reason == STOP_STOP_SEQUENCE:
            resp["status"] = STATUS_COMPLETED
            return {
                "type": EVENT_TYPE_RESPONSE_COMPLETED,
                "response": resp,
            }, [
                loss(
                    "status",
                    "stop_sequence",
                    LOSS_UNMAPPED_VALUE,
                    "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
                )
            ]

        raise ValueError("responses: stop reason 'other' has no Responses equivalent")
