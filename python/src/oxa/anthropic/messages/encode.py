"""Anthropic Messages IR → face conversions (spec/12 §4)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING,
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
from oxa.anthropic.messages.native import (
    encode_request_native,
    get_block_cache_control,
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
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    Block,
    CustomToolResultBlock,
    CustomToolUseBlock,
    ImageBlock,
    Loss,
    Request,
    Response,
    TextBlock,
    ThinkingBlock,
    ToolResultBlock,
    ToolUseBlock,
)
from oxa.ir import (
    ROLE_USER as IR_ROLE_USER,
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

    tools_wire: list[dict[str, Any]] = []
    if req.tools:
        for index, tool in enumerate(req.tools):
            if tool.type == "custom":
                if tool.format is not None and getattr(tool.format, "type", "") == "grammar":
                    losses.append(
                        loss(
                            f"tools[{index}].format",
                            "format",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            "Anthropic Messages structured tool wrapper cannot represent custom tool format constraints",
                        )
                    )
                t_dict = {
                    "name": tool.name,
                    "input_schema": {
                        "type": "object",
                        "properties": {"input": {"type": "string"}},
                        "required": ["input"],
                    },
                }
                if tool.description:
                    t_dict["description"] = tool.description
                tools_wire.append(t_dict)
                continue
            if tool.type == "apply_patch":
                losses.append(loss(f"tools[{index}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool declarations have no Anthropic mapping"))
                continue
            require_json_object(tool.input_schema, f"tools[{index}].input_schema")
            t_dict = {
                "name": tool.name,
                "input_schema": tool.input_schema,
            }
            if tool.description:
                t_dict["description"] = tool.description
            tools_wire.append(t_dict)
        if tools_wire:
            out["tools"] = tools_wire

    choice_wire, tc_losses = encode_tool_choice(req.tool_choice)
    if req.tools and not tools_wire and req.tool_choice is not None and req.tool_choice.mode == "any":
        choice_wire = None
        tc_losses.append(loss("tool_choice", "type", LOSS_UNSUPPORTED_SEMANTIC, "all declarations for the required tool choice were dropped"))
    if choice_wire is not None:
        out["tool_choice"] = choice_wire
    losses.extend(tc_losses)

    shorthand = (
        len(req.system) == 0
        and len(req.messages) == 1
        and len(req.messages[0].content) == 1
        and isinstance(req.messages[0].content[0], TextBlock)
        and not req.messages[0].content[0].native
    )

    for index, message in enumerate(req.messages):
        role = ROLE_USER if message.role == IR_ROLE_USER else ROLE_ASSISTANT
        blocks_wire, b_losses = encode_request_blocks(message.content, f"messages[{index}].content")
        losses.extend(b_losses)

        if shorthand:
            first_block = req.messages[0].content[0]
            assert isinstance(first_block, TextBlock)
            content_val: Any = first_block.text
        elif not blocks_wire and message.content:
            content_val = ""
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
        if req.params.response_format is not None:
            losses.append(
                loss(
                    "params.response_format",
                    "response_format",
                    LOSS_UNMAPPED_FIELD,
                    "Anthropic Messages has no native response_format request parameter; "
                    "structured output preference is dropped.",
                )
            )

    native_losses = encode_request_native(req, out)
    losses.extend(native_losses)

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
    encoded: dict[str, Any] | None = None
    losses: list[Loss] = []

    if isinstance(block, TextBlock):
        encoded = {"type": BLOCK_TYPE_TEXT, "text": block.text}

    elif isinstance(block, ThinkingBlock):
        signature = block.signature if block.signature else None
        if signature is None:
            losses.append(
                loss(
                    path,
                    "signature",
                    LOSS_DEGRADED,
                    "unsigned thinking block; Anthropic may reject on replay",
                )
            )
        encoded = {"type": BLOCK_TYPE_THINKING, "thinking": block.thinking}
        if signature is not None:
            encoded["signature"] = signature

    elif isinstance(block, ImageBlock):
        encoded, img_losses, mapped = encode_image_block(
            block.media_type, block.data, block.url, path, block.native
        )
        losses.extend(img_losses)
        if not mapped:
            return None, losses, False

    elif isinstance(block, (ApplyPatchToolUseBlock, ApplyPatchToolResultBlock)):
        return None, [loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tools have no Anthropic mapping")], False

    elif isinstance(block, CustomToolUseBlock):
        if not block.id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not block.name:
            raise ValueError(f"anthropic: {path}.name is required")
        encoded = {
            "type": BLOCK_TYPE_TOOL_USE,
            "id": block.id,
            "name": block.name,
            "input": {"input": block.input},
        }

    elif isinstance(block, CustomToolResultBlock):
        if not block.tool_use_id:
            raise ValueError(f"anthropic: {path}.tool_use_id is required")
        if isinstance(block.output, str):
            encoded = {
                "type": BLOCK_TYPE_TOOL_RESULT,
                "tool_use_id": block.tool_use_id,
                "content": block.output,
            }
        else:
            wire_content: list[dict[str, Any]] = []
            for i, inner in enumerate(block.output or []):
                if isinstance(inner, TextBlock):
                    item: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": inner.text}
                    cc = get_block_cache_control(inner.native)
                    if cc is not None:
                        item["cache_control"] = cc
                    wire_content.append(item)
                elif isinstance(inner, ImageBlock):
                    img_wire, img_losses, mapped = encode_image_block(
                        inner.media_type, inner.data, inner.url, f"{path}.content[{i}]", inner.native
                    )
                    if mapped and img_wire:
                        wire_content.append(img_wire)
                    losses.extend(img_losses)
            encoded = {
                "type": BLOCK_TYPE_TOOL_RESULT,
                "tool_use_id": block.tool_use_id,
                "content": wire_content,
            }

    elif isinstance(block, ToolUseBlock):
        if not block.id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not block.name:
            raise ValueError(f"anthropic: {path}.name is required")
        raw = input_from_ir_string(block.input, path)
        encoded = {
            "type": BLOCK_TYPE_TOOL_USE,
            "id": block.id,
            "name": block.name,
            "input": raw,
        }

    elif isinstance(block, ToolResultBlock):
        return encode_tool_result_block(block, path)

    else:
        return None, [], False

    if encoded is not None:
        cc = get_block_cache_control(block.native)
        if cc is not None:
            encoded["cache_control"] = cc

    return encoded, losses, True


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
    native: tuple[Any, ...] = (),
) -> tuple[dict[str, Any] | None, list[Loss], bool]:
    has_data = bool(data)
    has_url = bool(url)
    if has_data == has_url:
        return (
            None,
            [
                loss(
                    path,
                    "image",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "image must contain exactly one of data or url",
                )
            ],
            False,
        )
    if has_data:
        if not media_type:
            return (
                None,
                [
                    loss(
                        path,
                        "image",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "base64 image data requires media_type",
                    )
                ],
                False,
            )
        encoded: dict[str, Any] = {
            "type": BLOCK_TYPE_IMAGE,
            "source": {
                "type": SOURCE_TYPE_BASE64,
                "media_type": media_type,
                "data": data,
            },
        }
        cc = get_block_cache_control(native)
        if cc is not None:
            encoded["cache_control"] = cc
        return (
            encoded,
            [],
            True,
        )
    if media_type:
        return (
            None,
            [loss(path, "image", LOSS_UNSUPPORTED_SEMANTIC, "URL image must not carry media_type")],
            False,
        )
    encoded = {
        "type": BLOCK_TYPE_IMAGE,
        "source": {
            "type": SOURCE_TYPE_URL,
            "url": url,
        },
    }
    cc = get_block_cache_control(native)
    if cc is not None:
        encoded["cache_control"] = cc
    return (
        encoded,
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
            item: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": inner.text}
            cc = get_block_cache_control(inner.native)
            if cc is not None:
                item["cache_control"] = cc
            wire_content.append(item)
        elif isinstance(inner, ImageBlock):
            img_wire, img_losses, mapped = encode_image_block(
                inner.media_type, inner.data, inner.url, content_path, inner.native
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

    cc = get_block_cache_control(block.native)
    if cc is not None:
        out["cache_control"] = cc

    return out, losses, True


def encode_response(
    resp: Response,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR response to an Anthropic Messages wire response (IR → face)."""
    model = resp.model
    if table is not None:
        model = table.map(model)

    usage: dict[str, Any] = {
        "input_tokens": resp.usage.input_tokens,
        "output_tokens": resp.usage.output_tokens,
    }
    if resp.usage.cache_read_input_tokens is not None:
        usage["cache_read_input_tokens"] = resp.usage.cache_read_input_tokens
    if resp.usage.cache_creation_input_tokens is not None:
        usage["cache_creation_input_tokens"] = resp.usage.cache_creation_input_tokens

    out: dict[str, Any] = {
        "id": resp.id,
        "type": TYPE_MESSAGE,
        "role": ROLE_ASSISTANT,
        "model": model,
        "content": [],
        "usage": usage,
    }

    losses: list[Loss] = []
    for index, block in enumerate(resp.content):
        encoded, b_losses, mapped = encode_response_block(block, f"content[{index}]")
        if mapped and encoded is not None:
            out["content"].append(encoded)
        losses.extend(b_losses)

    effective_stop = resp.stop_reason
    if effective_stop == STOP_TOOL_USE and not any(isinstance(b, (ToolUseBlock, CustomToolUseBlock)) for b in resp.content):
        effective_stop = STOP_END_TURN
        losses.append(loss("stop_reason", "stop_reason", LOSS_DEGRADED, "tool_use was degraded because the target cannot retain tool calls"))
    if resp.content and not out["content"]:
        out["content"].append({"type": BLOCK_TYPE_TEXT, "text": ""})
    reason, seq = encode_stop_reason(effective_stop, resp.stop_sequence)
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
    if isinstance(block, ThinkingBlock):
        signature = block.signature if block.signature else None
        losses = []
        if signature is None:
            losses.append(
                loss(
                    path,
                    "signature",
                    LOSS_DEGRADED,
                    "unsigned thinking block; Anthropic may reject on replay",
                )
            )
        encoded = {"type": BLOCK_TYPE_THINKING, "thinking": block.thinking}
        if signature is not None:
            encoded["signature"] = signature
        return encoded, losses, True
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
    if isinstance(block, CustomToolUseBlock):
        if not block.id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not block.name:
            raise ValueError(f"anthropic: {path}.name is required")
        return (
            {
                "type": BLOCK_TYPE_TOOL_USE,
                "id": block.id,
                "name": block.name,
                "input": {"input": block.input},
            },
            [],
            True,
        )
    if isinstance(block, (CustomToolResultBlock, ApplyPatchToolUseBlock, ApplyPatchToolResultBlock)):
        return None, [loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tools have no Anthropic mapping")], False
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
