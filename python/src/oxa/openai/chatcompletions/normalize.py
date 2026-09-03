"""Normalization rules N-CC-1 through N-CC-10 (spec/10 §5)."""

from __future__ import annotations

from typing import Any
import urllib.parse

from oxa.ir import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_USER,
    TOOL_CHOICE_ANY,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_TOOL,
    Block,
    ImageBlock,
    Loss,
    Message as IrMessage,
    SystemBlock,
    TextBlock,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
)
from oxa.openai.chatcompletions.constants import (
    CONTENT_PART_TYPE_IMAGE_URL,
    CONTENT_PART_TYPE_TEXT,
    ROLE_ASSISTANT,
    ROLE_TOOL,
    TOOL_CHOICE_AUTO as CC_TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE as CC_TOOL_CHOICE_NONE,
    TOOL_CHOICE_REQUIRED as CC_TOOL_CHOICE_REQUIRED,
    TOOL_TYPE_FUNCTION,
)


def loss(path: str, field_name: str, reason: str, detail: str) -> Loss:
    return Loss(path=path, field=field_name, reason=reason, detail=detail)


def decode_content(
    content: Any,
    path: str,
) -> tuple[list[Block], list[Loss]]:
    """N-CC-1 normalizes Chat Completions string and parts-array content into IR blocks."""
    if content is None:
        return [TextBlock(text="")], []
    if isinstance(content, str):
        return [TextBlock(text=content)], []
    if isinstance(content, list):
        blocks: list[Block] = []
        losses: list[Loss] = []
        for index, part in enumerate(content):
            if isinstance(part, dict):
                part_blocks, part_losses = decode_content_part(part, path, index)
                blocks.extend(part_blocks)
                losses.extend(part_losses)
        return blocks, losses
    return [], []


def decode_content_part(part: dict[str, Any], path: str, index: usize_or_int) -> tuple[list[Block], list[Loss]]:
    kind = part.get("type", "")
    if kind == CONTENT_PART_TYPE_TEXT:
        return [TextBlock(text=part.get("text", ""))], []
    if kind == CONTENT_PART_TYPE_IMAGE_URL:
        img_url = part.get("image_url", {})
        raw_url = img_url.get("url", "") if isinstance(img_url, dict) else ""
        block, img_loss = decode_image_url(raw_url, f"{path}[{index}].image_url")
        if img_loss is not None:
            return [], [img_loss]
        return [block], []
    return [], [
        loss(
            f"{path}[{index}]",
            "type",
            LOSS_UNSUPPORTED_SEMANTIC,
            f"Chat Completions content part type {kind!r} has no IR equivalent",
        )
    ]


usize_or_int = int


def is_valid_https_url(raw: str) -> bool:
    if not raw.startswith("https://"):
        return False
    if any(c.isspace() or ord(c) < 32 for c in raw):
        return False
    parsed = urllib.parse.urlsplit(raw)
    return bool(parsed.scheme == "https" and parsed.netloc)


def decode_image_url(raw: str, path: str) -> tuple[Block, Loss | None]:
    """N-CC-2 normalizes supported https and data image URLs into image blocks."""
    if raw.startswith("https:"):
        if is_valid_https_url(raw):
            return ImageBlock(url=raw), None
        return TextBlock(text=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed https image URL has no IR equivalent",
        )
    if not raw.startswith("data:"):
        return TextBlock(text=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "only https and base64 data image URLs are supported",
        )
    data_part = raw[len("data:") :]
    if "," not in data_part:
        return TextBlock(text=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed data image URL has no IR equivalent",
        )
    metadata, data = data_part.split(",", 1)
    if not metadata.endswith(";base64"):
        return TextBlock(text=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed data image URL has no IR equivalent",
        )
    media_type = metadata[: -len(";base64")]
    if not media_type.lower().startswith("image/") or media_type == "image/":
        return TextBlock(text=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "non-image data URL has no IR equivalent",
        )
    return ImageBlock(media_type=media_type, data=data), None


def decode_tool_calls(calls: list[dict[str, Any]], path: str) -> tuple[list[Block], list[Loss]]:
    """N-CC-3 appends function tool calls after the assistant's normal content."""
    blocks: list[Block] = []
    losses: list[Loss] = []
    for index, call in enumerate(calls):
        call_type = call.get("type", "")
        if call_type != TOOL_TYPE_FUNCTION:
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Chat Completions tool call type {call_type!r} has no IR equivalent",
                )
            )
            continue
        func = call.get("function", {})
        blocks.append(
            ToolUseBlock(
                id=call.get("id", ""),
                name=func.get("name", ""),
                input=func.get("arguments", ""),
            )
        )
    return blocks, losses


def decode_tool_result_run(
    messages: list[dict[str, Any]],
    start: int,
) -> tuple[IrMessage, int, list[Loss]]:
    """N-CC-4 merges consecutive role:tool messages into one user message."""
    content: list[Block] = []
    losses: list[Loss] = []
    index = start
    while index < len(messages) and messages[index].get("role") == ROLE_TOOL:
        msg = messages[index]
        blocks, block_losses = decode_content(msg.get("content"), f"messages[{index}].content")
        losses.extend(block_losses)
        content.append(
            ToolResultBlock(
                tool_use_id=msg.get("tool_call_id", ""),
                content=blocks,
                is_error=False,
            )
        )
        if "function_call" in msg and msg["function_call"] is not None:
            losses.append(
                loss(
                    f"messages[{index}].function_call",
                    "function_call",
                    LOSS_UNMAPPED_FIELD,
                    "legacy Chat Completions function_call has no IR equivalent",
                )
            )
        index += 1
    return IrMessage(role=ROLE_USER, content=content), index, losses


def decode_tool_choice(value: Any) -> tuple[ToolChoice | None, list[Loss]]:
    """N-CC-5 maps the Chat Completions tool_choice forms to the IR modes."""
    if value is None:
        return None, []
    if isinstance(value, str):
        if value == CC_TOOL_CHOICE_AUTO:
            return ToolChoice(mode=TOOL_CHOICE_AUTO), []
        if value == CC_TOOL_CHOICE_NONE:
            return ToolChoice(mode=TOOL_CHOICE_NONE), []
        if value == CC_TOOL_CHOICE_REQUIRED:
            return ToolChoice(mode=TOOL_CHOICE_ANY), []
        return None, [
            loss(
                "tool_choice",
                "tool_choice",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Chat Completions tool_choice {value!r} has no IR equivalent",
            )
        ]
    if isinstance(value, dict):
        kind = value.get("type", "")
        func = value.get("function", {})
        name = func.get("name", "") if isinstance(func, dict) else ""
        if kind == TOOL_TYPE_FUNCTION and name:
            return ToolChoice(mode=TOOL_CHOICE_TOOL, name=name), []
        return None, [
            loss(
                "tool_choice",
                "tool_choice",
                LOSS_UNSUPPORTED_SEMANTIC,
                "only named function Chat Completions tool_choice values are supported",
            )
        ]
    return None, [
        loss(
            "tool_choice",
            "tool_choice",
            LOSS_UNSUPPORTED_SEMANTIC,
            "Chat Completions tool_choice has no IR equivalent",
        )
    ]


def encode_tool_choice(choice: ToolChoice | None) -> tuple[Any | None, Loss | None]:
    """N-CC-6 renders the IR's four tool-choice modes in Chat Completions form."""
    if choice is None:
        return None, None
    if choice.mode == TOOL_CHOICE_AUTO:
        return CC_TOOL_CHOICE_AUTO, None
    if choice.mode == TOOL_CHOICE_NONE:
        return CC_TOOL_CHOICE_NONE, None
    if choice.mode == TOOL_CHOICE_ANY:
        return CC_TOOL_CHOICE_REQUIRED, None
    if choice.mode == TOOL_CHOICE_TOOL:
        if choice.name:
            return {
                "type": TOOL_TYPE_FUNCTION,
                "function": {"name": choice.name},
            }, None
        return None, loss(
            "tool_choice",
            "tool_choice",
            LOSS_UNSUPPORTED_SEMANTIC,
            "IR named tool choice has no function name",
        )
    return None, loss(
        "tool_choice",
        "tool_choice",
        LOSS_UNSUPPORTED_SEMANTIC,
        f"IR tool_choice mode {choice.mode!r} has no Chat Completions equivalent",
    )


def encode_image_block(
    media_type: str | None,
    data: str | None,
    url: str | None,
    path: str,
) -> tuple[dict[str, Any] | None, Loss | None]:
    """N-CC-7 renders image blocks as the supported image_url content parts."""
    has_data = bool(data)
    has_url = bool(url)
    if has_data == has_url:
        return None, loss(
            path,
            "image",
            LOSS_UNSUPPORTED_SEMANTIC,
            "IR image must contain exactly one of data or URL",
        )
    if has_data:
        mtype = media_type or ""
        if not mtype.lower().startswith("image/") or mtype == "image/":
            return None, loss(
                path,
                "media_type",
                LOSS_UNSUPPORTED_SEMANTIC,
                "IR image media type has no Chat Completions image_url equivalent",
            )
        return {
            "type": CONTENT_PART_TYPE_IMAGE_URL,
            "image_url": {"url": f"data:{mtype};base64,{data}"},
        }, None
    if has_url:
        u = url or ""
        if is_valid_https_url(u):
            return {
                "type": CONTENT_PART_TYPE_IMAGE_URL,
                "image_url": {"url": u},
            }, None
    return None, loss(
        path,
        "image",
        LOSS_UNSUPPORTED_SEMANTIC,
        "IR image has no supported Chat Completions image_url equivalent",
    )


def encode_user_content(blocks: list[Block], path: str) -> tuple[Any, list[Loss]]:
    """N-CC-8 renders normal user content as a string when text-only and parts array with images."""
    parts: list[dict[str, Any]] = []
    text_chunks: list[str] = []
    has_image = False
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            parts.append({"type": CONTENT_PART_TYPE_TEXT, "text": block.text})
            text_chunks.append(block.text)
        elif isinstance(block, ImageBlock):
            part, img_loss = encode_image_block(
                block.media_type, block.data, block.url, f"{path}[{index}]"
            )
            if img_loss is not None:
                losses.append(img_loss)
            elif part is not None:
                has_image = True
                parts.append(part)
        else:
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block cannot be rendered in a Chat Completions user message",
                )
            )

    if has_image:
        return parts, losses
    return "".join(text_chunks), losses


def encode_assistant_message(blocks: list[Block], path: str) -> tuple[dict[str, Any], list[Loss]]:
    """N-CC-9 renders assistant text and tool_use blocks."""
    out: dict[str, Any] = {"role": ROLE_ASSISTANT}
    text_chunks: list[str] = []
    tool_calls: list[dict[str, Any]] = []
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            text_chunks.append(block.text)
        elif isinstance(block, ToolUseBlock):
            tool_calls.append(
                {
                    "id": block.id,
                    "type": TOOL_TYPE_FUNCTION,
                    "function": {
                        "name": block.name,
                        "arguments": block.input,
                    },
                }
            )
        elif isinstance(block, ImageBlock):
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "IR image block cannot be rendered in a Chat Completions assistant message",
                )
            )
        elif isinstance(block, ToolResultBlock):
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "IR tool_result block cannot be rendered in a Chat Completions assistant message",
                )
            )

    out["content"] = "".join(text_chunks)
    if tool_calls:
        out["tool_calls"] = tool_calls
    return out, losses


def encode_tool_result(block: Block, path: str) -> tuple[dict[str, Any], list[Loss]]:
    """N-CC-10 renders tool_result blocks as role:tool messages."""
    if not isinstance(block, ToolResultBlock):
        return {"role": ROLE_TOOL}, [
            loss(
                path,
                "content",
                LOSS_UNSUPPORTED_SEMANTIC,
                "IR block is not a tool_result block",
            )
        ]
    text_chunks: list[str] = []
    losses: list[Loss] = []
    for index, inner in enumerate(block.content):
        if isinstance(inner, TextBlock):
            text_chunks.append(inner.text)
        else:
            losses.append(
                loss(
                    f"{path}.content[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block cannot be rendered in a Chat Completions tool result",
                )
            )
    if block.is_error:
        losses.append(
            loss(
                f"{path}.is_error",
                "is_error",
                LOSS_UNMAPPED_FIELD,
                "Chat Completions tool messages have no is_error field",
            )
        )
    return {
        "role": ROLE_TOOL,
        "content": "".join(text_chunks),
        "tool_call_id": block.tool_use_id,
    }, losses


def content_system(blocks: list[Block], path: str) -> tuple[list[SystemBlock], list[Loss]]:
    system: list[SystemBlock] = []
    losses: list[Loss] = []
    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            system.append(block)
        else:
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "only text blocks are supported in Chat Completions system messages",
                )
            )
    return system, losses
