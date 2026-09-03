"""Anthropic Messages IR → face conversions (spec/12 §4)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    ROLE_ASSISTANT,
    ROLE_USER,
    SOURCE_TYPE_BASE64,
    SOURCE_TYPE_URL,
    STOP_REASON_END_TURN,
    STOP_REASON_MAX_TOKENS,
    STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE,
    STOP_REASON_TOOL_USE,
    TYPE_MESSAGE,
)
from oxa.anthropic.messages.normalize import (
    encode_system,
    encode_tool_choice,
    loss,
    require_json_object,
)
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
    ROLE_USER as IR_ROLE_USER,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    Block,
    ImageBlock,
    Loss,
    Request,
    Response,
    TextBlock,
    ToolResultBlock,
    ToolUseBlock,
)
from oxa.modelmap import Table

DEFAULT_MAX_TOKENS = 4096


def encode_request(
    req: Request,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR request to an Anthropic Messages wire request (IR → face)."""
    losses: list[Loss] = []

    if req.metadata:
        losses.append(
            loss(
                "metadata",
                "metadata",
                LOSS_UNMAPPED_FIELD,
                "Anthropic request metadata is the user_id semantic, not an arbitrary string map; the IR metadata map is dropped.",
            )
        )

    model = req.model
    if table is not None:
        model = table.map(model)

    out: dict[str, Any] = {
        "model": model,
        "messages": [],
    }

    if req.system:
        out["system"] = encode_system(req.system)

    if req.tools:
        tools_wire: list[dict[str, Any]] = []
        for index, tool in enumerate(req.tools):
            require_json_object(tool.input_schema, f"tools[{index}].input_schema")
            t_dict: dict[str, Any] = {
                "name": tool.name,
                "input_schema": tool.input_schema,
            }
            if tool.description:
                t_dict["description"] = tool.description
            tools_wire.append(t_dict)
        out["tools"] = tools_wire

    choice_wire, tc_losses = encode_tool_choice(req.tool_choice)
    if choice_wire is not None:
        out["tool_choice"] = choice_wire
    losses.extend(tc_losses)

    shorthand = (
        len(req.system) == 0
        and len(req.messages) == 1
        and len(req.messages[0].content) == 1
        and isinstance(req.messages[0].content[0], TextBlock)
    )

    for index, message in enumerate(req.messages):
        role = ROLE_USER if message.role == IR_ROLE_USER else ROLE_ASSISTANT
        blocks_wire, b_losses = encode_request_blocks(
            message.content, f"messages[{index}].content"
        )
        losses.extend(b_losses)

        if shorthand:
            first_block = req.messages[0].content[0]
            assert isinstance(first_block, TextBlock)
            content_val: Any = first_block.text
        else:
            content_val = blocks_wire

        out["messages"].append(
            {
                "role": role,
                "content": content_val,
            }
        )

    max_tokens = req.params.max_tokens if req.params and req.params.max_tokens is not None else None
    if max_tokens is None:
        losses.append(
            loss(
                "params",
                "max_tokens",
                LOSS_DEGRADED,
                f"Anthropic Messages requires max_tokens; defaulting to {DEFAULT_MAX_TOKENS}",
            )
        )
        max_tokens = DEFAULT_MAX_TOKENS

    out["max_tokens"] = max_tokens

    if req.params:
        if req.params.temperature is not None:
            out["temperature"] = req.params.temperature
        if req.params.top_p is not None:
            out["top_p"] = req.params.top_p
        if req.params.stop_sequences:
            out["stop_sequences"] = req.params.stop_sequences

    return out, losses


def encode_request_blocks(
    blocks: list[Block],
    path: str,
) -> tuple[list[dict[str, Any]], list[Loss]]:
    out: list[dict[str, Any]] = []
    losses: list[Loss] = []
    for index, block in enumerate(blocks):
        encoded, b_losses, mapped = encode_request_block(block, f"{path}[{index}]")
        if mapped and encoded is not None:
            out.append(encoded)
        losses.extend(b_losses)
    return out, losses


def encode_request_block(
    block: Block,
    path: str,
) -> tuple[dict[str, Any] | None, list[Loss], bool]:
    if isinstance(block, TextBlock):
        return {"type": BLOCK_TYPE_TEXT, "text": block.text}, [], True

    if isinstance(block, ImageBlock):
        return encode_image_block(block.media_type, block.data, block.url, path)

    if isinstance(block, ToolUseBlock):
        if not block.id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not block.name:
            raise ValueError(f"anthropic: {path}.name is required")
        raw = input_from_ir_string(block.input, path)
        return (
            {
                "type": BLOCK_TYPE_TOOL_USE,
                "id": block.id,
                "name": block.name,
                "input": raw,
            },
            [],
            True,
        )

    if isinstance(block, ToolResultBlock):
        return encode_tool_result_block(block, path)

    return None, [], False


def input_from_ir_string(input_str: str, path: str) -> dict[str, Any]:
    if not input_str:
        raise ValueError("anthropic: tool_use input is required")
    trimmed = input_str.strip()
    if not (trimmed.startswith("{") and trimmed.endswith("}")):
        raise ValueError(f"anthropic: {path}.input must be a JSON object")
    try:
        data = json.loads(trimmed)
        if not isinstance(data, dict):
            raise ValueError(f"anthropic: {path}.input must be a JSON object")
        return data
    except json.JSONDecodeError as exc:
        raise ValueError(f"anthropic: {path}.input is not valid JSON") from exc


def encode_image_block(
    media_type: str | None,
    data: str | None,
    url: str | None,
    path: str,
) -> tuple[dict[str, Any] | None, list[Loss], bool]:
    has_data = bool(data)
    has_url = bool(url)
    if has_data == has_url:
        return (
            None,
            [loss(path, "image", LOSS_UNSUPPORTED_SEMANTIC, "image must contain exactly one of data or url")],
            False,
        )
    if has_data:
        if not media_type:
            return (
                None,
                [loss(path, "image", LOSS_UNSUPPORTED_SEMANTIC, "base64 image data requires media_type")],
                False,
            )
        return (
            {
                "type": BLOCK_TYPE_IMAGE,
                "source": {
                    "type": SOURCE_TYPE_BASE64,
                    "media_type": media_type,
                    "data": data,
                },
            },
            [],
            True,
        )
    if media_type:
        return (
            None,
            [loss(path, "image", LOSS_UNSUPPORTED_SEMANTIC, "URL image must not carry media_type")],
            False,
        )
    return (
        {
            "type": BLOCK_TYPE_IMAGE,
            "source": {
                "type": SOURCE_TYPE_URL,
                "url": url,
            },
        },
        [],
        True,
    )


def encode_tool_result_block(
    block: ToolResultBlock,
    path: str,
) -> tuple[dict[str, Any] | None, list[Loss], bool]:
    if not block.tool_use_id:
        raise ValueError(f"anthropic: {path}.tool_use_id is required")

    wire_content: list[dict[str, Any]] = []
    losses: list[Loss] = []

    for index, inner in enumerate(block.content):
        content_path = f"{path}.content[{index}]"
        if isinstance(inner, TextBlock):
            wire_content.append({"type": BLOCK_TYPE_TEXT, "text": inner.text})
        elif isinstance(inner, ImageBlock):
            img_wire, img_losses, mapped = encode_image_block(
                inner.media_type, inner.data, inner.url, content_path
            )
            if mapped and img_wire is not None:
                wire_content.append(img_wire)
            losses.extend(img_losses)
        else:
            losses.append(
                loss(
                    content_path,
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block type has no Anthropic equivalent in this position",
                )
            )

    out: dict[str, Any] = {
        "type": BLOCK_TYPE_TOOL_RESULT,
        "tool_use_id": block.tool_use_id,
        "content": wire_content,
    }
    if block.is_error:
        out["is_error"] = True

    return out, losses, True


def encode_response(
    resp: Response,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR response to an Anthropic Messages wire response (IR → face)."""
    model = resp.model
    if table is not None:
        model = table.map(model)

    out: dict[str, Any] = {
        "id": resp.id,
        "type": TYPE_MESSAGE,
        "role": ROLE_ASSISTANT,
        "model": model,
        "content": [],
        "usage": {
            "input_tokens": resp.usage.input_tokens,
            "output_tokens": resp.usage.output_tokens,
        },
    }

    losses: list[Loss] = []
    for index, block in enumerate(resp.content):
        encoded, b_losses, mapped = encode_response_block(block, f"content[{index}]")
        if mapped and encoded is not None:
            out["content"].append(encoded)
        losses.extend(b_losses)

    reason, seq = encode_stop_reason(resp.stop_reason, resp.stop_sequence)
    out["stop_reason"] = reason
    if seq is not None:
        out["stop_sequence"] = seq

    return out, losses


def encode_response_block(
    block: Block,
    path: str,
) -> tuple[dict[str, Any] | None, list[Loss], bool]:
    if isinstance(block, TextBlock):
        return {"type": BLOCK_TYPE_TEXT, "text": block.text}, [], True
    if isinstance(block, ImageBlock):
        return encode_image_block(block.media_type, block.data, block.url, path)
    if isinstance(block, ToolUseBlock):
        if not block.id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not block.name:
            raise ValueError(f"anthropic: {path}.name is required")
        raw = input_from_ir_string(block.input, path)
        return (
            {
                "type": BLOCK_TYPE_TOOL_USE,
                "id": block.id,
                "name": block.name,
                "input": raw,
            },
            [],
            True,
        )
    return (
        None,
        [
            loss(
                path,
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                "this IR block type has no Anthropic equivalent in this position",
            )
        ],
        False,
    )


def encode_stop_reason(stop: str, seq: str | None) -> tuple[str, str | None]:
    if stop == STOP_END_TURN:
        return STOP_REASON_END_TURN, None
    if stop == STOP_MAX_TOKENS:
        return STOP_REASON_MAX_TOKENS, None
    if stop == STOP_STOP_SEQUENCE:
        return STOP_REASON_STOP_SEQUENCE, seq if seq else None
    if stop == STOP_TOOL_USE:
        return STOP_REASON_TOOL_USE, None
    if stop == STOP_REFUSAL:
        return STOP_REASON_REFUSAL, None
    raise ValueError(f"anthropic: stop reason {stop!r} has no Anthropic equivalent")
