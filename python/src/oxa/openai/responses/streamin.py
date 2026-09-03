"""OpenAI Responses streaming decoder: converts Responses streaming events into IR events (face → IR)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from oxa.ir import (
    LOSS_UNSUPPORTED_SEMANTIC,
    Block,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Delta,
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
from oxa.openai.responses.constants import (
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
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
)
from oxa.openai.responses.decode import decode_status
from oxa.openai.responses.normalize import loss


@dataclass(slots=True)
class _StreamFunctionCall:
    call_id: str
    name: str
    fragments: list[str] = field(default_factory=list)
    arguments_done: bool = False


class StreamDecoder:
    """Incrementally converts an OpenAI Responses event stream into IR events."""

    __slots__ = (
        "_table",
        "_losses",
        "_started",
        "_terminated",
        "_flushed",
        "_next_output_index",
        "_next_block_index",
        "_item_open",
        "_skipped_item",
        "_item_type",
        "_item_id",
        "_skipped_call_id",
        "_output_index",
        "_next_content_index",
        "_function_call",
        "_tool_use_seen",
        "_block_open",
        "_skipped_part",
        "_block_index",
        "_content_index",
        "_text_done",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._losses: list[Loss] = []
        self._started = False
        self._terminated = False
        self._flushed = False
        self._next_output_index = 0
        self._next_block_index = 0
        self._item_open = False
        self._skipped_item = False
        self._item_type = ""
        self._item_id = ""
        self._skipped_call_id = ""
        self._output_index = 0
        self._next_content_index = 0
        self._function_call: _StreamFunctionCall | None = None
        self._tool_use_seen = False
        self._block_open = False
        self._skipped_part = False
        self._block_index = 0
        self._content_index = 0
        self._text_done = False

    def losses(self) -> list[Loss]:
        return list(self._losses)

    def feed(self, ev: dict[str, Any], raw_text: str = "") -> list[Event]:
        """Pushes one Responses streaming event and returns any completed IR events."""
        if self._flushed:
            raise ValueError("responses: event fed after stream flush")
        if self._terminated:
            raise ValueError("responses: event fed after terminal response")

        kind = ev.get("type", "")

        if kind == EVENT_TYPE_RESPONSE_CREATED:
            if self._started:
                raise ValueError("responses: duplicate response.created")
            resp = ev.get("response")
            if not isinstance(resp, dict):
                raise ValueError("responses: response.created without response")
            self._started = True
            raw_model = str(resp.get("model", ""))
            model = self._table.map(raw_model) if self._table else raw_model
            return [MessageStart(id=str(resp.get("id", "")), model=model)]

        if kind == EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED:
            self._require_started(EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED)
            if self._item_open:
                raise ValueError("responses: response.output_item.added with an item still open")
            out_idx = int(ev.get("output_index", -1))
            if out_idx != self._next_output_index:
                raise ValueError(
                    f"responses: output_item.added output_index {out_idx}, want {self._next_output_index}"
                )
            item = ev.get("item")
            if not isinstance(item, dict):
                raise ValueError("responses: response.output_item.added without item")

            self._next_output_index += 1
            self._item_open = True
            item_type = item.get("type", "")
            self._item_type = item_type
            self._output_index = out_idx
            self._item_id = item.get("id", "")
            self._skipped_call_id = ""
            self._next_content_index = 0
            self._function_call = None

            if item_type == ITEM_TYPE_MESSAGE and item.get("role") == ROLE_ASSISTANT:
                return []
            if item_type == ITEM_TYPE_FUNCTION_CALL:
                call_id = item.get("call_id", "")
                name = item.get("name", "")
                item_id = item.get("id", "")
                if not item_id or not call_id or not name:
                    raise ValueError("responses: function_call item requires id, call_id, and name")
                self._function_call = _StreamFunctionCall(
                    call_id=call_id,
                    name=name,
                    fragments=[item.get("arguments", "")],
                )
                return []

            self._skipped_item = True
            if item_type == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
                self._skipped_call_id = item.get("call_id", "")
            self._losses.append(self._unsupported_item_loss(out_idx, item_type))
            return []

        if kind == EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED)
            if self._function_call is not None:
                raise ValueError("responses: response.content_part.added on function_call item")
            if self._block_open or self._skipped_part:
                raise ValueError("responses: response.content_part.added with a part still open")
            content_index = int(ev.get("content_index", -1))
            if content_index != self._next_content_index:
                raise ValueError(
                    f"responses: content_part.added content_index {content_index}, want {self._next_content_index}"
                )
            self._next_content_index += 1
            self._content_index = content_index

            part = ev.get("part")
            if not isinstance(part, dict):
                raise ValueError("responses: response.content_part.added without part")
            if self._skipped_item:
                self._skipped_part = True
                return []
            if part.get("type") != PART_TYPE_OUTPUT_TEXT:
                self._skipped_part = True
                self._losses.append(
                    loss(
                        f"output[{ev.get('output_index', -1)}].content[{content_index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Responses streaming content type {part.get('type')!r} is not decoded in the Responses stream profile",
                    )
                )
                return []

            self._block_open = True
            self._block_index = self._next_block_index
            self._next_block_index += 1
            self._text_done = False
            return [ContentBlockStart(index=self._block_index, block=TextBlock(text=part.get("text", "")))]

        if kind == EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA:
            if self._skipped_item:
                self._require_active_item(ev, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA)
                return []
            self._require_function_call(ev, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA)
            fc = self._function_call
            assert fc is not None
            if fc.arguments_done:
                raise ValueError(
                    "responses: response.function_call_arguments.delta after arguments.done"
                )
            fc.fragments.append(str(ev.get("delta", "")))
            return []

        if kind == EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE:
            if self._skipped_item:
                self._require_active_item(ev, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE)
                return []
            self._require_function_call(ev, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE)
            fc = self._function_call
            assert fc is not None
            if fc.arguments_done:
                raise ValueError("responses: duplicate response.function_call_arguments.done")
            call_id = ev.get("call_id", "")
            name = ev.get("name", "")
            arguments = ev.get("arguments", "")
            if call_id != fc.call_id or name != fc.name or arguments != "".join(fc.fragments):
                raise ValueError(
                    "responses: response.function_call_arguments.done does not match the active function call"
                )
            fc.arguments_done = True
            return []

        if kind == EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA)
            if self._function_call is not None:
                raise ValueError("responses: response.output_text.delta on function_call item")
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: output_text.delta content_index {content_index} does not match the skipped part"
                    )
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: output_text.delta does not match the open content part"
                )
            if self._text_done:
                raise ValueError("responses: output_text.delta after output_text.done")
            return [ContentBlockDelta(index=self._block_index, delta=TextDelta(text=str(ev.get("delta", ""))))]

        if kind == EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE)
            if self._function_call is not None:
                raise ValueError("responses: response.output_text.done on function_call item")
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: output_text.done content_index {content_index} does not match the skipped part"
                    )
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: output_text.done does not match the open content part"
                )
            if self._text_done:
                raise ValueError("responses: duplicate output_text.done")
            self._text_done = True
            return []

        if kind == EVENT_TYPE_RESPONSE_CONTENT_PART_DONE:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_CONTENT_PART_DONE)
            if self._function_call is not None:
                raise ValueError("responses: response.content_part.done on function_call item")
            if "part" not in ev:
                raise ValueError("responses: response.content_part.done without part")
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: content_part.done content_index {content_index} does not match the skipped part"
                    )
                self._skipped_part = False
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: content_part.done does not match the open content part"
                )
            if not self._text_done:
                raise ValueError("responses: content_part.done before output_text.done")
            self._block_open = False
            return [ContentBlockStop(index=self._block_index)]

        if kind == EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE:
            self._require_started(EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE)
            output_index = int(ev.get("output_index", -1))
            if not self._item_open or output_index != self._output_index:
                raise ValueError(
                    "responses: response.output_item.done does not match the open item"
                )
            item = ev.get("item")
            if not isinstance(item, dict):
                raise ValueError(
                    "responses: response.output_item.done does not match the open item"
                )
            if item.get("id") != self._item_id or item.get("type") != self._item_type:
                raise ValueError(
                    "responses: response.output_item.done does not match the open item"
                )
            if (
                self._skipped_item
                and self._item_type == ITEM_TYPE_FUNCTION_CALL_OUTPUT
                and item.get("call_id") != self._skipped_call_id
            ):
                raise ValueError(
                    "responses: response.output_item.done does not match the active function_call_output"
                )
            if self._block_open or self._skipped_part:
                raise ValueError(
                    "responses: response.output_item.done with a content part still open"
                )

            events: list[Event] = []
            if self._function_call is not None:
                fc = self._function_call
                self._function_call = None
                joined = "".join(fc.fragments)
                if (
                    item.get("call_id") != fc.call_id
                    or item.get("name") != fc.name
                    or item.get("arguments") != joined
                ):
                    raise ValueError(
                        "responses: response.output_item.done does not match the active function call"
                    )
                idx = self._next_block_index
                self._next_block_index += 1
                events.append(
                    ContentBlockStart(
                        index=idx,
                        block=ToolUseBlock(
                            id=fc.call_id,
                            name=fc.name,
                            input=joined,
                        ),
                    )
                )
                for frag in fc.fragments:
                    events.append(
                        ContentBlockDelta(
                            index=idx,
                            delta=InputJsonDelta(partial_json=frag),
                        )
                    )
                events.append(ContentBlockStop(index=idx))
                self._tool_use_seen = True

            self._item_open = False
            self._item_type = ""
            self._item_id = ""
            self._skipped_call_id = ""
            self._skipped_item = False
            self._function_call = None
            return events

        if kind in (
            EVENT_TYPE_RESPONSE_COMPLETED,
            EVENT_TYPE_RESPONSE_INCOMPLETE,
            EVENT_TYPE_RESPONSE_FAILED,
        ):
            self._require_started(kind)
            if self._item_open or self._block_open or self._skipped_part:
                raise ValueError(f"responses: {kind} before output lifecycle completed")
            response = ev.get("response")
            if not isinstance(response, dict):
                raise ValueError(f"responses: {kind} without response")
            stop, status_losses = decode_status(response, self._tool_use_seen)
            self._losses.extend(status_losses)
            self._terminated = True

            usage_raw = response.get("usage", {})
            usage = Usage(
                input_tokens=int(usage_raw.get("input_tokens", 0)),
                output_tokens=int(usage_raw.get("output_tokens", 0)),
            )
            return [
                MessageDelta(stop_reason=stop, usage=usage),
                MessageDone(),
            ]

        self._losses.append(
            loss(
                "type",
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Responses stream event type {kind!r} is not decoded in the Responses stream profile",
            )
        )
        return []

    def flush(self) -> list[Event]:
        if self._flushed:
            raise ValueError("responses: stream flushed twice")
        if not self._terminated:
            raise ValueError("responses: stream ended without a terminal response event")
        self._flushed = True
        return []

    def _require_started(self, event_type: str) -> None:
        if not self._started:
            raise ValueError(f"responses: {event_type} before response.created")

    def _require_active_item(self, ev: dict[str, Any], event_type: str) -> None:
        self._require_started(event_type)
        out_idx = int(ev.get("output_index", -1))
        item_id = ev.get("item_id", "")
        if not self._item_open or out_idx != self._output_index or item_id != self._item_id:
            raise ValueError(f"responses: {event_type} does not match the open output item")

    def _require_function_call(self, ev: dict[str, Any], event_type: str) -> None:
        self._require_active_item(ev, event_type)
        if self._function_call is None:
            raise ValueError(f"responses: {event_type} without an active function_call item")

    def _unsupported_item_loss(self, output_index: int, item_type: str) -> Loss:
        if item_type == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
            detail = (
                "N-S-10: Responses function_call_output has no supported IR block mapping; "
                "response.output_item.done completes and is absorbed for this item-only lifecycle vector"
            )
        else:
            detail = f"Responses streaming output item type {item_type!r} is not decoded"
        return loss(f"output[{output_index}]", "type", LOSS_UNSUPPORTED_SEMANTIC, detail)
