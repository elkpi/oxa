"""Normalization rules N-AN-1 through N-AN-6 (spec/12 §5)."""

from __future__ import annotations

import dataclasses
import json
from typing import Any

from oxa._wire import raw_field
from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_DOCUMENT,
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    SOURCE_TYPE_BASE64,
    SOURCE_TYPE_FILE,
    SOURCE_TYPE_URL,
    TOOL_CHOICE_TYPE_ANY,
    TOOL_CHOICE_TYPE_AUTO,
    TOOL_CHOICE_TYPE_NONE,
    TOOL_CHOICE_TYPE_TOOL,
)
from oxa.anthropic.messages.native import (
    CACHE_CONTROL_LOSS_DETAIL,
    get_block_cache_control,
    parse_cache_control,
)
from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    TOOL_CHOICE_ANY,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_TOOL,
    Block,
    CustomToolResultBlock,
    CustomToolUseBlock,
    DocumentBlock,
    ImageBlock,
    Loss,
    SystemBlock,
    TextBlock,
    ThinkingBlock,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
)
from oxa.ir.native import (
    DOCUMENT_CITATIONS_LOSS_DETAIL,
    DOCUMENT_CONTEXT_LOSS_DETAIL,
    DOCUMENT_TITLE_LOSS_DETAIL,
    NATIVE_KIND_CACHE_CONTROL,
    NATIVE_KIND_DOCUMENT_CONTEXT,
    NATIVE_KIND_DOCUMENT_CITATIONS,
    NATIVE_KIND_DOCUMENT_TITLE,
    NATIVE_SOURCE_ANTHROPIC,
    NativeAnnotation,
    NativeOmission,
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
            native = ()
            if "cache_control" in block:
                canon, l = parse_cache_control(block["cache_control"], f"system[{index}].cache_control")
                if l is not None:
                    losses.append(l)
                if canon is not None:
                    native = (
                        NativeAnnotation(
                            source=NATIVE_SOURCE_ANTHROPIC,
                            kind=NATIVE_KIND_CACHE_CONTROL,
                            value=canon,
                            omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD, detail=CACHE_CONTROL_LOSS_DETAIL),
                        ),
                    )
            out.append(TextBlock(text=block.get("text", ""), native=native))
        return out, losses
    raise ValueError("anthropic: system must be a string or array of blocks")


def encode_system(system: list[SystemBlock]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for b in system:
        item: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": b.text}
        cc = get_block_cache_control(b.native)
        if cc is not None:
            item["cache_control"] = cc
        out.append(item)
    return out


def decode_content(
    content: Any,
    path: str,
    raw_text: str = "",
    custom_tools: set[str] | None = None,
    custom_call_ids: set[str] | None = None,
    declared_tools: set[str] | None = None,
    is_request: bool = False,
) -> tuple[list[Block], list[Loss]]:
    """Decodes wire message content: string or block array."""
    if content is None:
        raise ValueError(f"anthropic: {path} is missing")
    if isinstance(content, str):
        return [TextBlock(text=content)], []
    if isinstance(content, list):
        out: list[Block] = []
        losses: list[Loss] = []
        for index, block in enumerate(content):
            if not isinstance(block, dict):
                raise ValueError(f"anthropic: {path}[{index}] must be a block object")
            decoded, b_losses, mapped = decode_block(
                block, f"{path}[{index}]", raw_text, custom_tools, custom_call_ids, declared_tools, is_request=is_request
            )
            if mapped:
                out.extend(decoded)
            losses.extend(b_losses)
        return out, losses
    raise ValueError(f"anthropic: {path} must be a string or list of blocks")


def extract_tool_input_raw(raw_text, path: str) -> str | None:
    return raw_field(raw_text, path)


def decode_block(
    wire: dict[str, Any],
    path: str,
    raw_text: str = "",
    custom_tools: set[str] | None = None,
    custom_call_ids: set[str] | None = None,
    declared_tools: set[str] | None = None,
    is_request: bool = False,
) -> tuple[list[Block], list[Loss], bool]:
    """Decodes one wire block (N-AN-5)."""
    kind = wire.get("type", "")
    losses: list[Loss] = []
    block: Block | None = None

    if kind == BLOCK_TYPE_TEXT:
        text_val = wire.get("text")
        if not isinstance(text_val, str):
            raise ValueError(f"anthropic: {path}.text must be a string")
        block = TextBlock(text=text_val)

    elif kind == BLOCK_TYPE_THINKING:
        block = ThinkingBlock(
            thinking=wire.get("thinking", ""),
            signature=wire.get("signature"),
        )

    elif kind == BLOCK_TYPE_IMAGE:
        source = wire.get("source")
        if not isinstance(source, dict):
            raise ValueError(f"anthropic: {path}.source is required")
        src_kind = source.get("type", "")
        if "type" in source and not isinstance(src_kind, str):
            raise ValueError(f"anthropic: {path}.source.type must be a string")
        if src_kind == SOURCE_TYPE_BASE64:
            media_type = source.get("media_type", "")
            data = source.get("data", "")
            if not media_type:
                raise ValueError(f"anthropic: {path}.source.media_type is required")
            if not data:
                raise ValueError(f"anthropic: {path}.source.data is required")
            block = ImageBlock(media_type=media_type, data=data)
        elif src_kind == SOURCE_TYPE_URL:
            url = source.get("url", "")
            if not url:
                raise ValueError(f"anthropic: {path}.source.url is required")
            block = ImageBlock(url=url)
        else:
            losses.append(
                loss(
                    f"{path}.source",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Anthropic image source type {src_kind!r} has no IR equivalent",
                )
            )
            return [], losses, False

    elif kind == BLOCK_TYPE_DOCUMENT:
        if not is_request:
            losses.append(
                loss(
                    path,
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    'Anthropic block type "document" has no IR equivalent',
                )
            )
            return [], losses, False
        source = wire.get("source")
        if not isinstance(source, dict):
            raise ValueError(f"anthropic: {path}.source is required")
        src_kind = source.get("type", "")
        if "type" in source and not isinstance(src_kind, str):
            raise ValueError(f"anthropic: {path}.source.type must be a string")
        if src_kind == SOURCE_TYPE_BASE64:
            media_type = source.get("media_type", "")
            data = source.get("data", "")
            if not isinstance(media_type, str) or not media_type:
                raise ValueError(f"anthropic: {path}.source.media_type is required")
            if not isinstance(data, str) or not data:
                raise ValueError(f"anthropic: {path}.source.data is required")
            if "url" in source or "file_id" in source:
                raise ValueError(f"anthropic: {path}.source has conflicting source fields")
            block = DocumentBlock(media_type=media_type, data=data)
        elif src_kind == SOURCE_TYPE_URL:
            url = source.get("url", "")
            if not isinstance(url, str) or not url:
                raise ValueError(f"anthropic: {path}.source.url is required")
            if "media_type" in source or "data" in source or "file_id" in source:
                raise ValueError(f"anthropic: {path}.source has conflicting source fields")
            block = DocumentBlock(url=url)
        elif src_kind == SOURCE_TYPE_FILE:
            file_id = source.get("file_id", "")
            if not isinstance(file_id, str) or not file_id:
                raise ValueError(f"anthropic: {path}.source.file_id is required")
            if "media_type" in source or "data" in source or "url" in source:
                raise ValueError(f"anthropic: {path}.source has conflicting source fields")
            block = DocumentBlock(file_id=file_id)
        else:
            losses.append(
                loss(
                    path,
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Anthropic document source type {src_kind!r} has no IR equivalent",
                )
            )
            return [], losses, False
        doc_native = []
        if "title" in wire:
            title = wire["title"]
            if not isinstance(title, str):
                raise ValueError(f"anthropic: {path}.title must be a string")
            doc_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_DOCUMENT_TITLE,
                    value=json.dumps(title),
                    omission=NativeOmission(
                        field="title",
                        reason=LOSS_UNMAPPED_FIELD,
                        detail=DOCUMENT_TITLE_LOSS_DETAIL,
                    ),
                )
            )
        if "context" in wire:
            context = wire["context"]
            if not isinstance(context, str):
                raise ValueError(f"anthropic: {path}.context must be a string")
            doc_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_DOCUMENT_CONTEXT,
                    value=json.dumps(context),
                    omission=NativeOmission(
                        field="context",
                        reason=LOSS_UNMAPPED_FIELD,
                        detail=DOCUMENT_CONTEXT_LOSS_DETAIL,
                    ),
                )
            )
        citations = wire.get("citations")
        if citations is not None:
            if not isinstance(citations, dict):
                raise ValueError(f"anthropic: {path}.citations must be a JSON object")
            if "enabled" in citations and not isinstance(citations["enabled"], bool):
                raise ValueError(f"anthropic: {path}.citations.enabled must be a boolean")
            doc_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_DOCUMENT_CITATIONS,
                    value=json.dumps(citations, separators=(",", ":")),
                    omission=NativeOmission(
                        field="citations",
                        reason=LOSS_UNSUPPORTED_SEMANTIC,
                        detail=DOCUMENT_CITATIONS_LOSS_DETAIL,
                    ),
                )
            )
        block = dataclasses.replace(block, native=block.native + tuple(doc_native))

    elif kind == BLOCK_TYPE_TOOL_USE:
        tool_id = wire.get("id", "")
        name = wire.get("name", "")
        if not tool_id:
            raise ValueError(f"anthropic: {path}.id is required")
        if not name:
            raise ValueError(f"anthropic: {path}.name is required")
        raw_input = wire.get("input")
        if raw_input is None:
            raise ValueError(f"anthropic: {path}.input is required")
        if not isinstance(raw_input, dict):
            raise ValueError(f"anthropic: {path}.input must be a JSON object")

        is_custom = False
        custom_input = None
        if custom_tools and name in custom_tools:
            is_custom = True
            custom_input = str(raw_input.get("input", ""))
        elif declared_tools and name in declared_tools:
            is_custom = False
        elif (
            isinstance(raw_input, dict)
            and list(raw_input.keys()) == ["input"]
            and isinstance(raw_input["input"], str)
        ):
            is_custom = True
            custom_input = raw_input["input"]

        if is_custom:
            if custom_call_ids is not None:
                custom_call_ids.add(tool_id)
            block = CustomToolUseBlock(
                id=tool_id,
                name=name,
                input=custom_input if custom_input is not None else "",
            )
        else:
            raw_slice = extract_tool_input_raw(raw_text, f"{path}.input")
            if raw_slice is not None:
                input_text = raw_slice
            else:
                input_text = json.dumps(raw_input, separators=(",", ":"), ensure_ascii=False)
            block = ToolUseBlock(id=tool_id, name=name, input=input_text)

    elif kind == BLOCK_TYPE_TOOL_RESULT:
        tool_use_id = wire.get("tool_use_id", "")
        if not tool_use_id:
            raise ValueError(f"anthropic: {path}.tool_use_id is required")
        inner_content = wire.get("content", [])
        if custom_call_ids and tool_use_id in custom_call_ids:
            if isinstance(inner_content, str):
                block = CustomToolResultBlock(tool_use_id=tool_use_id, output=inner_content)
            else:
                content_blocks, c_losses = decode_content(
                    inner_content, f"{path}.content", raw_text, custom_tools, custom_call_ids, declared_tools, is_request=is_request
                )
                losses.extend(c_losses)
                block = CustomToolResultBlock(tool_use_id=tool_use_id, output=content_blocks)
        else:
            content_blocks: list[Block]
            if isinstance(inner_content, str):
                content_blocks = [TextBlock(text=inner_content)]
            else:
                content_blocks, c_losses = decode_content(
                    inner_content, f"{path}.content", raw_text, custom_tools, custom_call_ids, declared_tools, is_request=is_request
                )
                losses.extend(c_losses)
            is_error = bool(wire.get("is_error", False))
            block = ToolResultBlock(tool_use_id=tool_use_id, content=content_blocks, is_error=is_error)

    else:
        losses.append(
            loss(
                path,
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Anthropic block type {kind!r} has no IR equivalent",
            )
        )
        return [], losses, False

    if is_request and "cache_control" in wire and block is not None:
        canon, l = parse_cache_control(wire["cache_control"], f"{path}.cache_control")
        if l is not None:
            losses.append(l)
        if canon is not None:
            ann = NativeAnnotation(
                source=NATIVE_SOURCE_ANTHROPIC,
                kind=NATIVE_KIND_CACHE_CONTROL,
                value=canon,
                omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD, detail=CACHE_CONTROL_LOSS_DETAIL),
            )
            block = dataclasses.replace(block, native=block.native + (ann,))

    return [block] if block is not None else [], losses, True


def decode_tool_choice(
    choice: Any,
    custom_tools: set[str] | None = None,
    native_tools: set[str] | None = None,
) -> tuple[ToolChoice | None, list[Loss]]:
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
        if native_tools and name in native_tools:
            tool_type = "native"
        elif custom_tools and name in custom_tools:
            tool_type = "custom"
        else:
            tool_type = "function"
        decoded = ToolChoice(mode=TOOL_CHOICE_TOOL, name=name, tool_type=tool_type)
    else:
        losses.append(
            loss(
                "tool_choice",
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Anthropic tool_choice type {kind!r} has no IR equivalent",
            )
        )

    if "disable_parallel_tool_use" in choice:
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
        if choice.tool_type == "apply_patch":
            return None, [loss("tool_choice", "tool_choice", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch has no Anthropic tool choice mapping")]
        if not choice.name:
            raise ValueError("anthropic: tool_choice.name is required for mode tool")
        return {
            "type": TOOL_CHOICE_TYPE_TOOL,
            "name": choice.name,
        }, []
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
