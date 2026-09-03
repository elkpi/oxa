"""Canonical JSON codec for Intermediate Representation (IR) documents (spec/01)."""

from __future__ import annotations

import json
from typing import Any

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
    SPEC_VERSION,
    STOP_STOP_SEQUENCE,
    TOOL_CHOICE_TOOL,
)
from oxa.ir.loss import Loss
from oxa.ir.types import (
    Block,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Delta,
    Event,
    EventStream,
    ImageBlock,
    InputJsonDelta,
    Message,
    MessageDelta,
    MessageDone,
    MessageStart,
    Params,
    Request,
    Response,
    TextBlock,
    TextDelta,
    Tool,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
    Usage,
)


class CodecError(ValueError):
    """Raised when an IR document violates structural schema requirements."""


# ---- Blocks ----------------------------------------------------------------


def dump_block(block: Block) -> dict[str, Any]:
    if isinstance(block, TextBlock):
        return {"type": BLOCK_TYPE_TEXT, "text": block.text}
    if isinstance(block, ImageBlock):
        has_data = bool(block.data)
        has_url = bool(block.url)
        if has_data == has_url:
            raise CodecError("image block must carry exactly one of data or url")
        if has_data:
            if not block.media_type:
                raise CodecError("image block with data requires media_type")
            return {
                "type": BLOCK_TYPE_IMAGE,
                "media_type": block.media_type,
                "data": block.data,
            }
        return {"type": BLOCK_TYPE_IMAGE, "url": block.url}
    if isinstance(block, ToolUseBlock):
        if not block.id:
            raise CodecError("tool_use block requires non-empty id")
        if not block.name:
            raise CodecError("tool_use block requires non-empty name")
        if block.input is None:
            raise CodecError("tool_use block requires input")
        return {
            "type": BLOCK_TYPE_TOOL_USE,
            "id": block.id,
            "name": block.name,
            "input": block.input,
        }
    if isinstance(block, ToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("tool_result block requires non-empty tool_use_id")
        out: dict[str, Any] = {
            "type": BLOCK_TYPE_TOOL_RESULT,
            "tool_use_id": block.tool_use_id,
            "content": [dump_block(b) for b in block.content],
        }
        if block.is_error:
            out["is_error"] = True
        return out
    raise CodecError(f"unknown block type: {type(block)}")


def load_block(data: dict[str, Any]) -> Block:
    kind = data.get("type")
    if kind == BLOCK_TYPE_TEXT:
        return TextBlock(text=data["text"])
    if kind == BLOCK_TYPE_IMAGE:
        return ImageBlock(
            media_type=data.get("media_type"),
            data=data.get("data"),
            url=data.get("url"),
        )
    if kind == BLOCK_TYPE_TOOL_USE:
        return ToolUseBlock(
            id=data["id"],
            name=data["name"],
            input=data["input"],
        )
    if kind == BLOCK_TYPE_TOOL_RESULT:
        content_raw = data.get("content", [])
        return ToolResultBlock(
            tool_use_id=data["tool_use_id"],
            content=[load_block(b) for b in content_raw],
            is_error=bool(data.get("is_error", False)),
        )
    raise CodecError(f"unknown block discriminant: {kind}")


# ---- Deltas & Events -------------------------------------------------------


def dump_delta(delta: Delta) -> dict[str, Any]:
    if isinstance(delta, TextDelta):
        return {"type": DELTA_TYPE_TEXT_DELTA, "text": delta.text}
    if isinstance(delta, InputJsonDelta):
        return {"type": DELTA_TYPE_INPUT_JSON_DELTA, "partial_json": delta.partial_json}
    raise CodecError(f"unknown delta type: {type(delta)}")


def load_delta(data: dict[str, Any]) -> Delta:
    kind = data.get("type")
    if kind == DELTA_TYPE_TEXT_DELTA:
        return TextDelta(text=data["text"])
    if kind == DELTA_TYPE_INPUT_JSON_DELTA:
        return InputJsonDelta(partial_json=data["partial_json"])
    raise CodecError(f"unknown delta discriminant: {kind}")


def dump_event(event: Event) -> dict[str, Any]:
    if isinstance(event, MessageStart):
        return {
            "type": EVENT_TYPE_MESSAGE_START,
            "id": event.id,
            "model": event.model,
        }
    if isinstance(event, ContentBlockStart):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_START,
            "index": event.index,
            "block": dump_block(event.block),
        }
    if isinstance(event, ContentBlockDelta):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
            "index": event.index,
            "delta": dump_delta(event.delta),
        }
    if isinstance(event, ContentBlockStop):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_STOP,
            "index": event.index,
        }
    if isinstance(event, MessageDelta):
        out: dict[str, Any] = {
            "type": EVENT_TYPE_MESSAGE_DELTA,
            "stop_reason": event.stop_reason,
            "usage": {
                "input_tokens": event.usage.input_tokens,
                "output_tokens": event.usage.output_tokens,
            },
        }
        if event.stop_reason == STOP_STOP_SEQUENCE:
            if event.stop_sequence:
                out["stop_sequence"] = event.stop_sequence
        elif event.stop_sequence is not None:
            raise CodecError("stop_sequence is only permitted when stop_reason is stop_sequence")
        return out
    if isinstance(event, MessageDone):
        return {"type": EVENT_TYPE_MESSAGE_DONE}
    raise CodecError(f"unknown event type: {type(event)}")


def load_event(data: dict[str, Any]) -> Event:
    kind = data.get("type")
    if kind == EVENT_TYPE_MESSAGE_START:
        return MessageStart(id=data["id"], model=data["model"])
    if kind == EVENT_TYPE_CONTENT_BLOCK_START:
        return ContentBlockStart(index=int(data["index"]), block=load_block(data["block"]))
    if kind == EVENT_TYPE_CONTENT_BLOCK_DELTA:
        return ContentBlockDelta(index=int(data["index"]), delta=load_delta(data["delta"]))
    if kind == EVENT_TYPE_CONTENT_BLOCK_STOP:
        return ContentBlockStop(index=int(data["index"]))
    if kind == EVENT_TYPE_MESSAGE_DELTA:
        usage_data = data.get("usage", {})
        usage = Usage(
            input_tokens=int(usage_data.get("input_tokens", 0)),
            output_tokens=int(usage_data.get("output_tokens", 0)),
        )
        return MessageDelta(
            stop_reason=data["stop_reason"],
            usage=usage,
            stop_sequence=data.get("stop_sequence"),
        )
    if kind == EVENT_TYPE_MESSAGE_DONE:
        return MessageDone()
    raise CodecError(f"unknown event discriminant: {kind}")


# ---- Request ---------------------------------------------------------------


def dump_request(req: Request) -> dict[str, Any]:
    out: dict[str, Any] = {
        "specVersion": SPEC_VERSION,
        "model": req.model,
        "messages": [
            {
                "role": m.role,
                "content": [dump_block(b) for b in m.content],
            }
            for m in req.messages
        ],
    }
    if req.system:
        out["system"] = [dump_block(b) for b in req.system]
    if req.tools:
        tools_out: list[dict[str, Any]] = []
        for t in req.tools:
            t_dict: dict[str, Any] = {
                "name": t.name,
                "input_schema": t.input_schema,
            }
            if t.description:
                t_dict["description"] = t.description
            tools_out.append(t_dict)
        out["tools"] = tools_out
    if req.tool_choice:
        tc_dict: dict[str, Any] = {"mode": req.tool_choice.mode}
        if req.tool_choice.mode == TOOL_CHOICE_TOOL:
            if not req.tool_choice.name:
                raise CodecError("tool choice in mode 'tool' requires name")
            tc_dict["name"] = req.tool_choice.name
        out["tool_choice"] = tc_dict
    if req.params:
        p_dict: dict[str, Any] = {}
        if req.params.temperature is not None:
            p_dict["temperature"] = req.params.temperature
        if req.params.top_p is not None:
            p_dict["top_p"] = req.params.top_p
        if req.params.max_tokens is not None:
            p_dict["max_tokens"] = req.params.max_tokens
        if req.params.stop_sequences is not None:
            p_dict["stop_sequences"] = req.params.stop_sequences
        if p_dict:
            out["params"] = p_dict
    if req.metadata:
        out["metadata"] = req.metadata
    return out


def load_request(data: dict[str, Any] | str) -> Request:
    if isinstance(data, str):
        data = json.loads(data)
    version = data.get("specVersion")
    if version != SPEC_VERSION:
        raise CodecError(f"unsupported specVersion {version!r}, want {SPEC_VERSION!r}")

    system: list[TextBlock] = []
    if "system" in data:
        for b in data["system"]:
            blk = load_block(b)
            if not isinstance(blk, TextBlock):
                raise CodecError("system block must be text")
            system.append(blk)

    messages: list[Message] = []
    for m in data.get("messages", []):
        messages.append(
            Message(
                role=m["role"],
                content=[load_block(b) for b in m["content"]],
            )
        )

    tools: list[Tool] | None = None
    if "tools" in data:
        tools = [
            Tool(
                name=t["name"],
                input_schema=t["input_schema"],
                description=t.get("description"),
            )
            for t in data["tools"]
        ]

    tool_choice: ToolChoice | None = None
    if "tool_choice" in data:
        tc = data["tool_choice"]
        tool_choice = ToolChoice(mode=tc["mode"], name=tc.get("name"))

    params: Params | None = None
    if "params" in data:
        p = data["params"]
        params = Params(
            temperature=p.get("temperature"),
            top_p=p.get("top_p"),
            max_tokens=p.get("max_tokens"),
            stop_sequences=p.get("stop_sequences"),
        )

    metadata = data.get("metadata")

    return Request(
        model=data["model"],
        messages=messages,
        system=system,
        tools=tools,
        tool_choice=tool_choice,
        params=params,
        metadata=metadata,
    )


# ---- Response --------------------------------------------------------------


def dump_response(resp: Response) -> dict[str, Any]:
    out: dict[str, Any] = {
        "specVersion": SPEC_VERSION,
        "id": resp.id,
        "model": resp.model,
        "content": [dump_block(b) for b in resp.content],
        "stop_reason": resp.stop_reason,
        "usage": {
            "input_tokens": resp.usage.input_tokens,
            "output_tokens": resp.usage.output_tokens,
        },
    }
    if resp.stop_reason == STOP_STOP_SEQUENCE:
        if resp.stop_sequence:
            out["stop_sequence"] = resp.stop_sequence
    elif resp.stop_sequence is not None:
        raise CodecError("stop_sequence is only permitted when stop_reason is stop_sequence")
    return out


def load_response(data: dict[str, Any] | str) -> Response:
    if isinstance(data, str):
        data = json.loads(data)
    version = data.get("specVersion")
    if version != SPEC_VERSION:
        raise CodecError(f"unsupported specVersion {version!r}, want {SPEC_VERSION!r}")

    content = [load_block(b) for b in data.get("content", [])]
    usage_data = data.get("usage", {})
    usage = Usage(
        input_tokens=int(usage_data.get("input_tokens", 0)),
        output_tokens=int(usage_data.get("output_tokens", 0)),
    )
    return Response(
        id=data["id"],
        model=data["model"],
        content=content,
        stop_reason=data["stop_reason"],
        usage=usage,
        stop_sequence=data.get("stop_sequence"),
    )


# ---- EventStream -----------------------------------------------------------


def dump_event_stream(stream: EventStream) -> dict[str, Any]:
    return {
        "specVersion": SPEC_VERSION,
        "events": [dump_event(e) for e in stream.events],
    }


def load_event_stream(data: dict[str, Any] | list[dict[str, Any]] | str) -> EventStream:
    if isinstance(data, str):
        data = json.loads(data)
    if isinstance(data, list):
        # Bare array of events without document envelope
        return EventStream(events=[load_event(e) for e in data])
    version = data.get("specVersion")
    if version != SPEC_VERSION:
        raise CodecError(f"unsupported specVersion {version!r}, want {SPEC_VERSION!r}")
    return EventStream(events=[load_event(e) for e in data.get("events", [])])


# ---- Loss ------------------------------------------------------------------


def dump_loss(loss: Loss) -> dict[str, Any]:
    return loss.to_dict()


def load_loss(data: dict[str, Any]) -> Loss:
    return Loss.from_dict(data)
