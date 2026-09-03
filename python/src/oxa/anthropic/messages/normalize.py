"""Normalization rules N-AN-1 through N-AN-6 (spec/12 §5)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    SOURCE_TYPE_BASE64,
    SOURCE_TYPE_URL,
    TOOL_CHOICE_TYPE_ANY,
    TOOL_CHOICE_TYPE_AUTO,
    TOOL_CHOICE_TYPE_NONE,
    TOOL_CHOICE_TYPE_TOOL,
)
from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    TOOL_CHOICE_ANY,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_TOOL,
    Block,
    ImageBlock,
    Loss,
    SystemBlock,
    TextBlock,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
)


def loss(path: str, field_name: str, reason: str, detail: str) -> Loss:
    return Loss(path=path, field=field_name, reason=reason, detail=detail)


def require_json_object(raw: Any, path: str) -> None:
    if isinstance(raw, dict):
        return
    if isinstance(raw, str):
        trimmed = raw.strip()
        if not trimmed:
            raise ValueError(f"anthropic: {path} is required")
        if not (trimmed.startswith("{") and trimmed.endswith("}")):
            raise ValueError(f"anthropic: {path} must be a JSON object")
        try:
            parsed = json.loads(trimmed)
            if not isinstance(parsed, dict):
                raise ValueError(f"anthropic: {path} must be a JSON object")
        except json.JSONDecodeError as exc:
            raise ValueError(f"anthropic: {path} is not valid JSON") from exc
        return
    raise ValueError(f"anthropic: {path} must be a JSON object")


def decode_system(system: Any) -> tuple[list[SystemBlock], list[Loss]]:
    """Decodes wire system content (string or block array)."""
    if system is None:
        return [], []
    if isinstance(system, str):
        return [TextBlock(text=system)], []
    if isinstance(system, list):
        out: list[SystemBlock] = []
        losses: list[Loss] = []
        for index, block in enumerate(system):
            if not isinstance(block, dict) or block.get("type") != BLOCK_TYPE_TEXT:
                kind = block.get("type") if isinstance(block, dict) else type(block).__name__
                raise ValueError(f"anthropic: system[{index}]: unsupported block type {kind!r}")
            out.append(TextBlock(text=block.get("text", "")))
            if block.get("cache_control") is not None:
                losses.append(
                    loss(
                        f"system[{index}].cache_control",
                        "cache_control",
                        LOSS_UNMAPPED_FIELD,
                        "Anthropic prompt caching annotations have no IR equivalent in v1.",
                    )
                )
        return out, losses
    raise ValueError("anthropic: system must be a string or array of blocks")


def encode_system(system: list[SystemBlock]) -> list[dict[str, Any]]:
    return [{"type": BLOCK_TYPE_TEXT, "text": b.text} for b in system]


def decode_content(content: Any, path: str) -> tuple[list[Block], list[Loss]]:
    """Decodes wire message content: string or block array."""
    if content is None:
        raise ValueError(f"anthropic: {path} is missing")
    if isinstance(content, str):
        return [TextBlock(text=content)], []
    if isinstance(content, list):
        out: list[Block] = []
        losses: list[Loss] = []
        for index, block in enumerate(content):
            if isinstance(block, dict):
                decoded, b_losses, mapped = decode_block(block, f"{path}[{index}]")
                if mapped:
                    out.extend(decoded)
                losses.extend(b_losses)
        return out, losses
    raise ValueError(f"anthropic: {path} must be a string or list of blocks")


def decode_block(wire: dict[str, Any], path: str) -> tuple[list[Block], list[Loss], bool]:
    """Decodes one wire block (N-AN-5)."""
    kind = wire.get("type", "")
    losses: list[Loss] = []

    if kind == BLOCK_TYPE_TEXT:
        block = TextBlock(text=wire.get("text", ""))
        if wire.get("cache_control") is not None:
            losses.append(
                loss(
                    f"{path}.cache_control",
                    "cache_control",
                    LOSS_UNMAPPED_FIELD,
                    "Anthropic prompt caching annotations have no IR equivalent in v1.",
                )
            )
        return [block], losses, True

    if kind == BLOCK_TYPE_IMAGE:
        source = wire.get("source")
        if not isinstance(source, dict):
            raise ValueError(f"anthropic: {path}.source is required")
        src_kind = source.get("type", "")
        if src_kind == SOURCE_TYPE_BASE64:
            media_type = source.get("media_type", "")
            data = source.get("data", "")
            if not media_type:
                raise ValueError(f"anthropic: {path}.source.media_type is required")
            if not data:
                raise ValueError(f"anthropic: {path}.source.data is required")
            block = ImageBlock(media_type=media_type, data=data)
            return [block], losses, True
        if src_kind == SOURCE_TYPE_URL:
            url = source.get("url", "")
            if not url:
                raise ValueError(f"anthropic: {path}.source.url is required")
            block = ImageBlock(url=url)
            return [block], losses, True
        losses.append(
            loss(
                f"{path}.source",
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Anthropic image source type {src_kind!r} has no IR equivalent",
            )
        )
        return [], losses, False

    if kind == BLOCK_TYPE_TOOL_USE:
        tool_id = wire.get("id", "")
        name = wire.get("name", "")
        if not tool_id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not name:
            raise ValueError(f"anthropic: {path}.name is required")
        raw_input = wire.get("input")
        if raw_input is None:
            raise ValueError(f"anthropic: {path}.input is required")
        require_json_object(raw_input, f"{path}.input")
        if isinstance(raw_input, dict):
            input_text = json.dumps(raw_input, separators=(",", ":"), ensure_ascii=False)
        else:
            input_text = str(raw_input)
        block = ToolUseBlock(id=tool_id, name=name, input=input_text)
        return [block], losses, True

    if kind == BLOCK_TYPE_TOOL_RESULT:
        tool_use_id = wire.get("tool_use_id", "")
        if not tool_use_id:
            raise ValueError(f"anthropic: {path}.tool_use_id is required")
        inner_content = wire.get("content", [])
        if isinstance(inner_content, str):
            content_blocks = [TextBlock(text=inner_content)]
        else:
            content_blocks, c_losses = decode_content(inner_content, f"{path}.content")
            losses.extend(c_losses)
        is_error = bool(wire.get("is_error", False))
        block = ToolResultBlock(tool_use_id=tool_use_id, content=content_blocks, is_error=is_error)
        return [block], losses, True

    losses.append(
        loss(
            path,
            "type",
            LOSS_UNSUPPORTED_SEMANTIC,
            f"Anthropic block type {kind!r} has no IR equivalent",
        )
    )
    return [], losses, False


def decode_tool_choice(choice: Any) -> tuple[ToolChoice | None, list[Loss]]:
    """Decodes the wire tool_choice object (N-AN-6)."""
    if choice is None:
        return None, []
    if not isinstance(choice, dict):
        return None, []
    kind = choice.get("type", "")
    losses: list[Loss] = []
    decoded: ToolChoice | None = None

    if kind == TOOL_CHOICE_TYPE_AUTO:
        decoded = ToolChoice(mode=TOOL_CHOICE_AUTO)
    elif kind == TOOL_CHOICE_TYPE_ANY:
        decoded = ToolChoice(mode=TOOL_CHOICE_ANY)
    elif kind == TOOL_CHOICE_TYPE_NONE:
        decoded = ToolChoice(mode=TOOL_CHOICE_NONE)
    elif kind == TOOL_CHOICE_TYPE_TOOL:
        name = choice.get("name", "")
        if not name:
            raise ValueError("anthropic: tool_choice.name is required for type tool")
        decoded = ToolChoice(mode=TOOL_CHOICE_TOOL, name=name)
    else:
        losses.append(
            loss(
                "tool_choice",
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Anthropic tool_choice type {kind!r} has no IR equivalent",
            )
        )

    if choice.get("disable_parallel_tool_use"):
        losses.append(
            loss(
                "tool_choice.disable_parallel_tool_use",
                "disable_parallel_tool_use",
                LOSS_UNMAPPED_FIELD,
                "Anthropic disable_parallel_tool_use has no IR equivalent in v1.",
            )
        )

    return decoded, losses


def encode_tool_choice(choice: ToolChoice | None) -> tuple[dict[str, Any] | None, list[Loss]]:
    if choice is None:
        return None, []
    if choice.mode == TOOL_CHOICE_AUTO:
        return {"type": TOOL_CHOICE_TYPE_AUTO}, []
    if choice.mode == TOOL_CHOICE_ANY:
        return {"type": TOOL_CHOICE_TYPE_ANY}, []
    if choice.mode == TOOL_CHOICE_NONE:
        return {"type": TOOL_CHOICE_TYPE_NONE}, []
    if choice.mode == TOOL_CHOICE_TOOL:
        if not choice.name:
            raise ValueError("anthropic: tool_choice.name is required for mode tool")
        return {
            "type": TOOL_CHOICE_TYPE_TOOL,
            "name": choice.name,
        }, []
    return None, [
        loss(
            "tool_choice",
            "type",
            LOSS_UNSUPPORTED_SEMANTIC,
            f"IR tool_choice mode {choice.mode!r} has no Anthropic equivalent",
        )
    ]
