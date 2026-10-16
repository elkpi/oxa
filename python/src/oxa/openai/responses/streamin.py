"""OpenAI Responses streaming decoder: converts Responses streaming events into IR events (face → IR)."""

from __future__ import annotations

from oxa._wire import RawDocument, audit_feed

from dataclasses import dataclass, field
from typing import Any

from oxa.ir import (
    LOSS_UNSUPPORTED_SEMANTIC,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    CustomToolInputDelta,
    CustomToolUseBlock,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Event,
    InputJsonDelta,
    InputTokensDetails,
    Loss,
    MessageDelta,
    MessageDone,
    MessageStart,
    OutputTokensDetails,
    TextBlock,
    TextDelta,
    ThinkingBlock,
    ThinkingDelta,
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
    EVENT_TYPE_RESPONSE_CUSTOM_TOOL_CALL_INPUT_DELTA,
    EVENT_TYPE_RESPONSE_CUSTOM_TOOL_CALL_INPUT_DONE,
    EVENT_TYPE_RESPONSE_INCOMPLETE,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DONE,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
)
from oxa.openai.responses.decode import decode_status
from oxa.openai.responses.normalize import (
    decode_apply_patch_call_status,
    decode_apply_patch_operation,
    decode_apply_patch_result_status,
    loss,
)


@dataclass(slots=True)
class _StreamFunctionCall:
    call_id: str
    name: str
    fragments: list[str] = field(default_factory=list)
    arguments_done: bool = False


@dataclass(slots=True)
class _StreamCustomToolCall:
    call_id: str
    name: str
    fragments: list[str] = field(default_factory=list)
    snapshots: list[str] = field(default_factory=list)


@dataclass(slots=True)
class _StreamApplyPatchCall:
    item_id: str
    call_id: str
    status: str
    operation: dict[str, Any]


@dataclass(slots=True)
class _StreamApplyPatchResult:
    item_id: str
    call_id: str
    status: str
    output: str | None = None


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
        "_custom_tool_call",
        "_apply_patch_call",
        "_apply_patch_result",
        "_completed_items",
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
        self._custom_tool_call: _StreamCustomToolCall | None = None
        self._apply_patch_call: _StreamApplyPatchCall | None = None
        self._apply_patch_result: _StreamApplyPatchResult | None = None
        self._completed_items: dict[str, dict[str, Any]] = {}
        self._tool_use_seen = False
        self._block_open = False
        self._skipped_part = False
        self._block_index = 0
        self._content_index = 0
        self._text_done = False

    def losses(self) -> list[Loss]:
        return list(self._losses)

    def feed_json(self, raw_text: str) -> list[Event]:
        document = RawDocument(raw_text)
        return self.feed(document.data, document)

    @audit_feed("responses")
    def feed(self, ev: dict[str, Any], raw_text: str = "") -> list[Event]:
        """Pushes one Responses streaming event and returns any completed IR events."""
        if self._flushed:
            raise ValueError("responses: event fed after stream flush")
        if self._terminated:
            raise ValueError("responses: event fed after terminal response")

        kind = ev.get("type", "")

        if (self._apply_patch_call is not None or self._apply_patch_result is not None) and (
            kind.startswith("response.content_part.")
            or kind.startswith("response.output_text.")
            or kind.startswith("response.reasoning_summary_")
            or kind.startswith("response.function_call_arguments.")
            or kind.startswith("response.custom_tool_call_input.")
        ):
            raise ValueError("responses: content or delta event on apply patch item")

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
            self._custom_tool_call = None

            if item_type == ITEM_TYPE_MESSAGE and item.get("role") == ROLE_ASSISTANT:
                return []
            if item_type == ITEM_TYPE_REASONING:
                return []
            if item_type == "apply_patch_call":
                call_id = item.get("call_id", "")
                if not self._item_id or not call_id or not item.get("status") or item.get("operation") is None:
                    raise ValueError("responses: apply_patch_call requires id, call_id, status, and operation")
                self._apply_patch_call = _StreamApplyPatchCall(
                    item_id=self._item_id,
                    call_id=call_id,
                    status=item["status"],
                    operation=item["operation"],
                )
                return []
            if item_type == "apply_patch_call_output":
                call_id = item.get("call_id", "")
                if not self._item_id or not call_id or not item.get("status"):
                    raise ValueError("responses: apply_patch_call_output requires id, call_id, and status")
                self._apply_patch_result = _StreamApplyPatchResult(
                    item_id=self._item_id,
                    call_id=call_id,
                    status=item["status"],
                    output=item.get("output"),
                )
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
            if item_type == ITEM_TYPE_CUSTOM_TOOL_CALL:
                call_id, name = item.get("call_id", ""), item.get("name", "")
                if not self._item_id or not call_id or not name:
                    raise ValueError("responses: custom_tool_call item requires id, call_id, and name")
                if not isinstance(item.get("input"), str) or item["input"] != "":
                    raise ValueError("responses: custom_tool_call added item requires input as an empty string")
                self._custom_tool_call = _StreamCustomToolCall(call_id=call_id, name=name)
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
            return [
                ContentBlockStart(
                    index=self._block_index, block=TextBlock(text=part.get("text", ""))
                )
            ]

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

        if kind == EVENT_TYPE_RESPONSE_CUSTOM_TOOL_CALL_INPUT_DELTA:
            self._require_custom_tool_call(ev, kind)
            call = self._custom_tool_call
            assert call is not None
            if "delta" not in ev or not isinstance(ev["delta"], str):
                raise ValueError("responses: custom_tool_call_input.delta requires string delta")
            if call.snapshots:
                raise ValueError("responses: custom_tool_call_input.delta after input.done")
            call.fragments.append(ev["delta"])
            return []

        if kind == EVENT_TYPE_RESPONSE_CUSTOM_TOOL_CALL_INPUT_DONE:
            self._require_custom_tool_call(ev, kind)
            call = self._custom_tool_call
            assert call is not None
            snapshot = ev.get("input")
            if not isinstance(snapshot, str):
                raise ValueError("responses: custom_tool_call_input.done requires string input")
            if call.snapshots and call.snapshots[0] != snapshot:
                raise ValueError("responses: conflicting custom_tool_call_input.done snapshots")
            if call.fragments and "".join(call.fragments) != snapshot:
                raise ValueError("responses: custom_tool_call_input.done does not match input deltas")
            call.snapshots.append(snapshot)
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
            return [
                ContentBlockDelta(
                    index=self._block_index, delta=TextDelta(text=str(ev.get("delta", "")))
                )
            ]

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
                raise ValueError("responses: output_text.done does not match the open content part")
            if self._text_done:
                raise ValueError("responses: duplicate output_text.done")
            self._text_done = True
            return []

        if kind == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED)
            if self._block_open or self._skipped_part:
                raise ValueError("responses: reasoning_summary_part.added with a part still open")
            content_index = int(ev.get("content_index", -1))
            if content_index != self._next_content_index:
                raise ValueError(
                    f"responses: reasoning_summary_part.added content_index {content_index}, want {self._next_content_index}"
                )
            self._next_content_index += 1
            self._content_index = content_index
            part = ev.get("part")
            if not isinstance(part, dict):
                raise ValueError("responses: reasoning_summary_part.added without part")
            if self._skipped_item:
                self._skipped_part = True
                return []
            if part.get("type") != PART_TYPE_OUTPUT_TEXT:
                self._skipped_part = True
                self._losses.append(
                    loss(
                        f"output[{self._output_index}].summary[{content_index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Responses reasoning summary type {part.get('type')!r} is not decoded in the stream profile",
                    )
                )
                return []
            self._block_open = True
            self._block_index = self._next_block_index
            self._next_block_index += 1
            self._text_done = False
            return [
                ContentBlockStart(
                    index=self._block_index,
                    block=ThinkingBlock(thinking=""),
                )
            ]

        if kind == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA)
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: reasoning_summary_text.delta content_index {content_index} does not match the skipped part"
                    )
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: reasoning_summary_text.delta does not match the open content part"
                )
            if self._text_done:
                raise ValueError("responses: reasoning_summary_text.delta after text.done")
            return [
                ContentBlockDelta(
                    index=self._block_index,
                    delta=ThinkingDelta(text=str(ev.get("delta", ""))),
                )
            ]

        if kind == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DONE:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DONE)
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: reasoning_summary_text.done content_index {content_index} does not match the skipped part"
                    )
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: reasoning_summary_text.done does not match the open content part"
                )
            if self._text_done:
                raise ValueError("responses: duplicate reasoning_summary_text.done")
            self._text_done = True
            return []

        if kind == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE:
            self._require_active_item(ev, EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE)
            if "part" not in ev:
                raise ValueError("responses: reasoning_summary_part.done without part")
            content_index = int(ev.get("content_index", -1))
            if self._skipped_item or self._skipped_part:
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: reasoning_summary_part.done content_index {content_index} does not match the skipped part"
                    )
                self._skipped_part = False
                return []
            if not self._block_open or content_index != self._content_index:
                raise ValueError(
                    "responses: reasoning_summary_part.done does not match the open content part"
                )
            self._block_open = False
            return [ContentBlockStop(index=self._block_index)]

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
            if self._apply_patch_call is not None:
                ap = self._apply_patch_call
                self._apply_patch_call = None
                if item.get("call_id") != ap.call_id:
                    raise ValueError("responses: output_item.done call_id does not match active apply_patch_call")
                status, s_loss = decode_apply_patch_call_status(item.get("status"), f"output[{output_index}]")
                if s_loss is not None:
                    raise ValueError(f"responses: {s_loss.detail}")
                op, op_loss = decode_apply_patch_operation(item.get("operation"), f"output[{output_index}].operation")
                if op_loss is not None:
                    raise ValueError(f"responses: {op_loss.detail}")
                idx = self._next_block_index
                self._next_block_index += 1
                events.append(ContentBlockStart(index=idx, block=ApplyPatchToolUseBlock(id=ap.call_id, status=status, operation=op)))
                events.append(ContentBlockStop(index=idx))
                self._tool_use_seen = True
                self._completed_items[item.get("id", "")] = item

            if self._apply_patch_result is not None:
                apr = self._apply_patch_result
                self._apply_patch_result = None
                if item.get("call_id") != apr.call_id:
                    raise ValueError("responses: output_item.done call_id does not match active apply_patch_call_output")
                status, s_loss = decode_apply_patch_result_status(item.get("status"), f"output[{output_index}]")
                if s_loss is not None:
                    raise ValueError(f"responses: {s_loss.detail}")
                idx = self._next_block_index
                self._next_block_index += 1
                out = item.get("output")
                if out is not None and not isinstance(out, str):
                    raise ValueError("responses: apply_patch_call_output output must be a string")
                events.append(ContentBlockStart(index=idx, block=ApplyPatchToolResultBlock(tool_use_id=apr.call_id, status=status, output=out)))
                events.append(ContentBlockStop(index=idx))
                self._completed_items[item.get("id", "")] = item

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

            if self._custom_tool_call is not None:
                cc = self._custom_tool_call
                if item.get("call_id") != cc.call_id or item.get("name") != cc.name:
                    raise ValueError("responses: output_item.done does not match the active custom_tool_call")
                final_input = item.get("input")
                if not isinstance(final_input, str):
                    raise ValueError("responses: custom_tool_call output_item.done requires string input")
                if any(snapshot != final_input for snapshot in cc.snapshots):
                    raise ValueError("responses: conflicting custom_tool_call input snapshots")
                if cc.fragments:
                    if "".join(cc.fragments) != final_input:
                        raise ValueError("responses: custom_tool_call deltas do not match the final input snapshot")
                    fragments = cc.fragments
                else:
                    fragments = [final_input]
                idx = self._next_block_index
                self._next_block_index += 1
                events.append(ContentBlockStart(index=idx, block=CustomToolUseBlock(id=cc.call_id, name=cc.name, input=final_input)))
                events.extend(ContentBlockDelta(index=idx, delta=CustomToolInputDelta(text=part)) for part in fragments)
                events.append(ContentBlockStop(index=idx))
                self._custom_tool_call = None
                self._tool_use_seen = True

            self._item_open = False
            self._item_type = ""
            self._item_id = ""
            self._skipped_call_id = ""
            self._skipped_item = False
            self._function_call = None
            self._apply_patch_call = None
            self._apply_patch_result = None
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

            for item in response.get("output", []):
                if not isinstance(item, dict):
                    continue
                itype = item.get("type")
                if itype in ("apply_patch_call", "apply_patch_call_output"):
                    prior = self._completed_items.get(item.get("id", ""))
                    if prior is None or item.get("call_id") != prior.get("call_id") or item.get("status") != prior.get("status"):
                        raise ValueError("responses: terminal apply patch snapshot mismatch")
                    if itype == "apply_patch_call":
                        if item.get("operation") != prior.get("operation"):
                            raise ValueError("responses: terminal apply patch operation mismatch")
                    else:
                        if item.get("output") != prior.get("output"):
                            raise ValueError("responses: terminal apply patch output mismatch")

            stop, status_losses = decode_status(response, self._tool_use_seen)
            self._losses.extend(status_losses)
            self._terminated = True

            usage_raw = response.get("usage", {})
            input_details = usage_raw.get("input_token_details")
            output_details = usage_raw.get("output_token_details")
            usage = Usage(
                input_tokens=int(usage_raw.get("input_tokens", 0)),
                output_tokens=int(usage_raw.get("output_tokens", 0)),
                input_tokens_details=(
                    InputTokensDetails(cached_tokens=int(input_details["cached_tokens"]))
                    if isinstance(input_details, dict)
                    and input_details.get("cached_tokens") is not None
                    else None
                ),
                output_tokens_details=(
                    OutputTokensDetails(reasoning_tokens=int(output_details["reasoning_tokens"]))
                    if isinstance(output_details, dict)
                    and output_details.get("reasoning_tokens") is not None
                    else None
                ),
            )
            return [
                MessageDelta(stop_reason=stop, usage=usage),
                MessageDone(),
            ]

        # Unknown event types: absorb identity-matching descendants of an
        # active skipped unit (N-S-3); validate identity against the open
        # supported unit otherwise; always keep at most one loss per event.
        has_identity = "output_index" in ev or "item_id" in ev or "content_index" in ev
        if self._skipped_item or self._skipped_part:
            if has_identity:
                self._require_active_item(ev, kind)
                if self._skipped_part and not self._skipped_item:
                    content_index = int(ev.get("content_index", -1))
                    if content_index != self._content_index:
                        raise ValueError(
                            f"responses: {kind} content_index {content_index} does not match the skipped part"
                        )
                return []
        elif has_identity:
            self._require_active_item(ev, kind)
            if self._block_open:
                content_index = int(ev.get("content_index", -1))
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: {kind} content_index {content_index} does not match the open content part"
                    )
            elif "content_index" in ev:
                raise ValueError(f"responses: {kind} has no open content part")
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

    def _require_custom_tool_call(self, ev: dict[str, Any], event_type: str) -> None:
        self._require_active_item(ev, event_type)
        if self._custom_tool_call is None:
            raise ValueError(f"responses: {event_type} without an active custom_tool_call item")

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
