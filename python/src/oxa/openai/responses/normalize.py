"""Normalization rules N-R-1 through N-R-12 (spec/11 §5)."""

from __future__ import annotations

from typing import Any
import urllib.parse

from oxa.ir import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
    ROLE_USER as IR_ROLE_USER,
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
from oxa.openai.responses.constants import (
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    PART_TYPE_INPUT_IMAGE,
    PART_TYPE_INPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_USER,
    TOOL_CHOICE_AUTO as RESP_TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE as RESP_TOOL_CHOICE_NONE,
    TOOL_CHOICE_REQUIRED as RESP_TOOL_CHOICE_REQUIRED,
    TOOL_TYPE_FUNCTION,
)


def loss(path: str, field_name: str, reason: str, detail: str) -> Loss:
    return Loss(path=path, field=field_name, reason=reason, detail=detail)


def is_valid_https_url(raw: str) -> bool:
    if not raw.startswith("https://"):
        return False
    if any(c.isspace() or ord(c) < 32 for c in raw):
        return False
    parsed = urllib.parse.urlsplit(raw)
    return bool(parsed.scheme == "https" and parsed.netloc)


def decode_content(content: Any, path: str) -> tuple[list[Block], list[Loss]]:
    """Decodes content (string or content-part list) into IR blocks."""
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


def decode_content_part(part: dict[str, Any], path: str, index: int) -> tuple[list[Block], list[Loss]]:
    kind = part.get("type", "")
    if kind == PART_TYPE_INPUT_TEXT:
        return [TextBlock(text=part.get("text", ""))], []
    if kind == PART_TYPE_INPUT_IMAGE:
        img_url = part.get("image_url", "")
        block, img_loss = decode_image_url(img_url, f"{path}[{index}].image_url")
        if img_loss is not None:
            return [], [img_loss]
        return [block], []
    return [], [
        loss(
            f"{path}[{index}]",
            "type",
            LOSS_UNSUPPORTED_SEMANTIC,
            f"Responses input content part type {kind!r} has no IR equivalent",
        )
    ]


def decode_image_url(raw: str, path: str) -> tuple[Block, Loss | None]:
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


def decode_assistant_run(
    items: list[dict[str, Any]],
    start: int,
) -> tuple[IrMessage | None, int, list[Loss]]:
    """N-R-5 merges assistant items and function-call items into one assistant message."""
    text: list[Block] = []
    calls: list[Block] = []
    losses: list[Loss] = []
    index = start

    while index < len(items):
        item = items[index]
        kind = item.get("type", "")
        if kind == ITEM_TYPE_FUNCTION_CALL:
            calls.append(
                ToolUseBlock(
                    id=item.get("call_id", ""),
                    name=item.get("name", ""),
                    input=item.get("arguments", ""),
                )
            )
            index += 1
            continue

        if kind not in ("", ITEM_TYPE_MESSAGE) or item.get("role") != ROLE_ASSISTANT:
            break

        blocks, b_losses = decode_content(item.get("content"), f"input[{index}].content")
        text.extend(blocks)
        losses.extend(b_losses)
        index += 1

    text.extend(calls)
    if not text:
        return None, index, losses
    return IrMessage(role=IR_ROLE_ASSISTANT, content=text), index, losses


def decode_output_run(
    items: list[dict[str, Any]],
    start: int,
) -> tuple[IrMessage, int]:
    """N-R-6 merges a maximal function_call_output run into one user message."""
    content: list[Block] = []
    index = start
    while index < len(items) and items[index].get("type") == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
        item = items[index]
        content.append(
            ToolResultBlock(
                tool_use_id=item.get("call_id", ""),
                content=[TextBlock(text=item.get("output", ""))],
                is_error=False,
            )
        )
        index += 1
    return IrMessage(role=IR_ROLE_USER, content=content), index


def decode_tool_choice(value: Any) -> tuple[ToolChoice | None, list[Loss]]:
    """N-R-9 maps Responses tool choice forms to the IR modes."""
    if value is None:
        return None, []
    if isinstance(value, str):
        if value == RESP_TOOL_CHOICE_AUTO:
            return ToolChoice(mode=TOOL_CHOICE_AUTO), []
        if value == RESP_TOOL_CHOICE_NONE:
            return ToolChoice(mode=TOOL_CHOICE_NONE), []
        if value == RESP_TOOL_CHOICE_REQUIRED:
            return ToolChoice(mode=TOOL_CHOICE_ANY), []
        return None, [
            loss(
                "tool_choice",
                "tool_choice",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Responses tool_choice {value!r} has no IR equivalent",
            )
        ]
    if isinstance(value, dict):
        kind = value.get("type", "")
        name = value.get("name", "")
        if kind == TOOL_TYPE_FUNCTION and name:
            return ToolChoice(mode=TOOL_CHOICE_TOOL, name=name), []
        return None, [
            loss(
                "tool_choice",
                "tool_choice",
                LOSS_UNSUPPORTED_SEMANTIC,
                "only named function Responses tool_choice values are supported",
            )
        ]
    return None, [
        loss(
            "tool_choice",
            "tool_choice",
            LOSS_UNSUPPORTED_SEMANTIC,
            "Responses tool_choice has no IR equivalent",
        )
    ]


def encode_tool_choice(choice: ToolChoice | None) -> tuple[Any | None, Loss | None]:
    if choice is None:
        return None, None
    if choice.mode == TOOL_CHOICE_AUTO:
        return RESP_TOOL_CHOICE_AUTO, None
    if choice.mode == TOOL_CHOICE_NONE:
        return RESP_TOOL_CHOICE_NONE, None
    if choice.mode == TOOL_CHOICE_ANY:
        return RESP_TOOL_CHOICE_REQUIRED, None
    if choice.mode == TOOL_CHOICE_TOOL:
        if choice.name:
            return {
                "type": TOOL_TYPE_FUNCTION,
                "name": choice.name,
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
        f"IR tool_choice mode {choice.mode!r} has no Responses equivalent",
    )


def encode_image_block(
    media_type: str | None,
    data: str | None,
    url: str | None,
    path: str,
) -> tuple[dict[str, Any] | None, Loss | None]:
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
                "IR image media type has no Responses input_image equivalent",
            )
        return {
            "type": PART_TYPE_INPUT_IMAGE,
            "image_url": f"data:{mtype};base64,{data}",
        }, None
    if has_url:
        u = url or ""
        if is_valid_https_url(u):
            return {
                "type": PART_TYPE_INPUT_IMAGE,
                "image_url": u,
            }, None
    return None, loss(
        path,
        "image",
        LOSS_UNSUPPORTED_SEMANTIC,
        "IR image has no supported Responses input_image equivalent",
    )


def encode_user_content(blocks: list[Block], path: str) -> tuple[Any, list[Loss]]:
    parts: list[dict[str, Any]] = []
    text_chunks: list[str] = []
    has_image = False
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            parts.append({"type": PART_TYPE_INPUT_TEXT, "text": block.text})
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
                    "this IR block cannot be rendered in a Responses user message",
                )
            )

    if has_image:
        return parts, losses
    return "".join(text_chunks), losses


def encode_user_message(
    message: IrMessage,
    path: str,
) -> tuple[list[dict[str, Any]], list[Loss]]:
    normal: list[Block] = []
    results: list[ToolResultBlock] = []
    first_normal: int | None = None
    last_result: int | None = None
    losses: list[Loss] = []

    for pos, block in enumerate(message.content):
        if isinstance(block, ToolResultBlock):
            results.append(block)
            last_result = pos
        else:
            if first_normal is None:
                first_normal = pos
            normal.append(block)

    if first_normal is not None and last_result is not None and first_normal < last_result:
        losses.append(
            loss(
                path,
                "ordering",
                LOSS_DEGRADED,
                "Responses function_call_output items are hoisted ahead of user content; the source turn's relative ordering is lost",
            )
        )

    items: list[dict[str, Any]] = []
    for index, res in enumerate(results):
        out_chunks: list[str] = []
        for c_idx, inner in enumerate(res.content):
            if isinstance(inner, TextBlock):
                out_chunks.append(inner.text)
            else:
                losses.append(
                    loss(
                        f"{path}.content[{index}].content[{c_idx}]",
                        "content",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "this IR block cannot be rendered in a Responses function_call_output item",
                    )
                )
        if res.is_error:
            losses.append(
                loss(
                    f"{path}.content[{index}].is_error",
                    "is_error",
                    LOSS_UNMAPPED_FIELD,
                    "Responses function_call_output items have no is_error field",
                )
            )
        items.append(
            {
                "type": ITEM_TYPE_FUNCTION_CALL_OUTPUT,
                "call_id": res.tool_use_id,
                "output": "".join(out_chunks),
            }
        )

    if normal or not results:
        content, c_losses = encode_user_content(normal, f"{path}.content")
        items.append(
            {
                "role": ROLE_USER,
                "content": content,
            }
        )
        losses.extend(c_losses)

    return items, losses


def encode_assistant_message(
    blocks: list[Block],
    path: str,
) -> tuple[list[dict[str, Any]], list[Loss]]:
    text_chunks: list[str] = []
    has_text = False
    calls: list[dict[str, Any]] = []
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            text_chunks.append(block.text)
            has_text = True
        elif isinstance(block, ToolUseBlock):
            calls.append(
                {
                    "type": ITEM_TYPE_FUNCTION_CALL,
                    "call_id": block.id,
                    "name": block.name,
                    "arguments": block.input,
                }
            )
        else:
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block cannot be rendered in a Responses assistant input item",
                )
            )

    items: list[dict[str, Any]] = []
    if has_text or not blocks:
        items.append(
            {
                "role": ROLE_ASSISTANT,
                "content": "".join(text_chunks),
            }
        )
    items.extend(calls)
    return items, losses


def content_system(blocks: list[Block], path: str) -> tuple[list[SystemBlock], list[Loss]]:
    out: list[SystemBlock] = []
    losses: list[Loss] = []
    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            out.append(block)
        else:
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses input system messages carry only text content",
                )
            )
    return out, losses
