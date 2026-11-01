"""Normalization rules N-R-1 through N-R-12 (spec/11 §5)."""

from __future__ import annotations

import urllib.parse
from typing import Any

import json
from dataclasses import replace
from oxa.ir.native import (
    NATIVE_KIND_PROMPT_CACHE_BREAKPOINT,
    PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL,
    NATIVE_SOURCE_OPENAI,
    NativeAnnotation,
    NativeOmission,
)
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    TOOL_CHOICE_ANY,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_TOOL,
    ApplyPatchCreateFileOperation,
    ApplyPatchDeleteFileOperation,
    ApplyPatchOperation,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    ApplyPatchUpdateFileOperation,
    Block,
    CustomToolResultBlock,
    CustomToolUseBlock,
    DocumentBlock,
    ImageBlock,
    Loss,
    SystemBlock,
    TextBlock,
    ThinkingBlock,
    RedactedThinkingBlock,
    RefusalBlock,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
    WebSearchCallBlock,
)
from oxa.ir import (
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
)
from oxa.ir import (
    ROLE_USER as IR_ROLE_USER,
)
from oxa.ir import (
    Message as IrMessage,
)
from oxa.openai.responses.constants import (
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_CUSTOM_TOOL_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING,
    ITEM_TYPE_WEB_SEARCH_CALL,
    PART_TYPE_INPUT_IMAGE,
    PART_TYPE_INPUT_TEXT,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_USER,
    TOOL_TYPE_FUNCTION,
    TOOL_TYPE_CUSTOM,
)
from oxa.openai.responses.constants import (
    TOOL_CHOICE_AUTO as RESP_TOOL_CHOICE_AUTO,
)
from oxa.openai.responses.constants import (
    TOOL_CHOICE_NONE as RESP_TOOL_CHOICE_NONE,
)
from oxa.openai.responses.constants import (
    TOOL_CHOICE_REQUIRED as RESP_TOOL_CHOICE_REQUIRED,
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


def decode_content(
    content: Any, path: str, is_user: bool = True
) -> tuple[list[Block], list[Loss]]:
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
                part_blocks, part_losses = decode_content_part(
                    part, path, index, is_user=is_user
                )
                blocks.extend(part_blocks)
                losses.extend(part_losses)
        return blocks, losses
    return [], []


def decode_content_part(
    part: dict[str, Any], path: str, index: int, is_user: bool = True
) -> tuple[list[Block], list[Loss]]:
    pcb_ann: NativeAnnotation | None = None
    if "prompt_cache_breakpoint" in part:
        pcb = part["prompt_cache_breakpoint"]
        if not isinstance(pcb, dict):
            raise TypeError(f"responses: {path}[{index}].prompt_cache_breakpoint must be an object")
        pcb_ann = NativeAnnotation(
            source=NATIVE_SOURCE_OPENAI,
            kind=NATIVE_KIND_PROMPT_CACHE_BREAKPOINT,
            value=json.dumps(pcb, separators=(",", ":")),
            omission=NativeOmission(
                field="prompt_cache_breakpoint",
                reason=LOSS_UNMAPPED_FIELD,
                detail=PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL,
            ),
        )
    blocks, losses = _decode_content_part_inner(part, path, index, is_user)
    if pcb_ann is not None:
        blocks = [replace(b, native=tuple(list(getattr(b, "native", ())) + [pcb_ann])) for b in blocks]
    return blocks, losses

def _decode_content_part_inner(
    part: dict[str, Any], path: str, index: int, is_user: bool = True
) -> tuple[list[Block], list[Loss]]:
    kind = part.get("type", "")
    if kind == PART_TYPE_INPUT_TEXT:
        return [TextBlock(text=part.get("text", ""))], []
    if kind == PART_TYPE_INPUT_IMAGE:
        img_url = part.get("image_url", "")
        if isinstance(img_url, dict):
            raw_url = img_url.get("url", "")
        else:
            raw_url = str(img_url) if img_url else ""
        block, img_loss = decode_image_url(raw_url, f"{path}[{index}].image_url")
        if img_loss is not None:
            return [], [img_loss]
        return [block], []
    if kind == PART_TYPE_OUTPUT_TEXT:
        return [TextBlock(text=part.get("text", ""))], []
    if kind == "refusal":
        ref = part.get("refusal")
        if ref is None or not isinstance(ref, str):
            raise TypeError(f"responses: {path}[{index}].refusal must be a string")
        if is_user:
            return [], [
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses content part type 'refusal' has no IR equivalent in user messages",
                )
            ]
        return [RefusalBlock(refusal=ref)], []
    if kind == "input_file":
        if not is_user:
            return [], [
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses content part type 'input_file' has no IR equivalent",
                )
            ]
        count = sum(1 for k in ("file_id", "file_url", "file_data") if part.get(k) is not None)
        if count != 1:
            raise ValueError(f"responses: {path}[{index}]: input_file must specify exactly one of file_id, file_url, or file_data")
        losses = []
        if "filename" in part and part["filename"] is not None:
            if not isinstance(part["filename"], str):
                raise TypeError(f"{path}[{index}].filename must be a string")
            losses.append(loss(f"{path}[{index}].filename", "filename", LOSS_UNMAPPED_FIELD, "Responses input_file filename has no IR equivalent"))
        if "detail" in part and part["detail"] is not None:
            if not isinstance(part["detail"], str):
                raise TypeError(f"{path}[{index}].detail must be a string")
            losses.append(loss(f"{path}[{index}].detail", "detail", LOSS_UNMAPPED_FIELD, "Responses input_file detail has no IR equivalent"))
        if part.get("file_id") is not None:
            fid = part["file_id"]
            if not isinstance(fid, str) or not fid:
                raise TypeError(f"{path}[{index}].file_id must be a non-empty string")
            return [DocumentBlock(file_id=fid)], losses
        if part.get("file_url") is not None:
            furl = part["file_url"]
            if not isinstance(furl, str) or not furl:
                raise TypeError(f"{path}[{index}].file_url must be a non-empty string")
            return [DocumentBlock(url=furl)], losses
        fdata = part["file_data"]
        if not isinstance(fdata, str) or not fdata:
            raise TypeError(f"{path}[{index}].file_data must be a non-empty string")
        if fdata.startswith("data:"):
            idx = fdata.find(";base64,")
            if idx == -1:
                raise ValueError(f"responses: {path}[{index}].file_data: malformed data file URL")
            mt = fdata[5:idx] or "application/pdf"
            payload = fdata[idx + len(";base64,"):]
            return [DocumentBlock(data=payload, media_type=mt)], losses
        return [DocumentBlock(data=fdata, media_type="application/pdf")], losses
    return [], [
        loss(
            f"{path}[{index}]",
            "type",
            LOSS_UNSUPPORTED_SEMANTIC,
            f"Responses content part type {kind!r} has no IR equivalent",
        )
    ]


def decode_image_url(raw: str, path: str) -> tuple[ImageBlock, Loss | None]:
    if raw.startswith("https:"):
        if is_valid_https_url(raw):
            return ImageBlock(url=raw), None
        return ImageBlock(url=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed https image URL has no IR equivalent",
        )
    if not raw.startswith("data:"):
        return ImageBlock(url=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "only https and base64 data image URLs are supported",
        )
    without_prefix = raw[5:]
    if "," not in without_prefix:
        return ImageBlock(url=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed data image URL has no IR equivalent",
        )
    metadata, data = without_prefix.split(",", 1)
    if not metadata.endswith(";base64"):
        return ImageBlock(url=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "malformed data image URL has no IR equivalent",
        )
    media_type = metadata[:-7]
    if not media_type.lower().startswith("image/") or media_type == "image/":
        return ImageBlock(url=""), loss(
            path,
            "image_url",
            LOSS_UNSUPPORTED_SEMANTIC,
            "non-image data URL has no IR equivalent",
        )
    return ImageBlock(media_type=media_type, data=data), None


def decode_apply_patch_operation(wire: Any, path: str) -> tuple[ApplyPatchOperation | None, Loss | None]:
    if not isinstance(wire, dict):
        raise ValueError(f"responses: {path}.operation is required")
    kind = wire.get("type")
    if not isinstance(kind, str) or not kind:
        raise ValueError(f"responses: {path}.operation.type is required")
    if kind not in ("create_file", "update_file", "delete_file"):
        return None, loss(path, "operation", LOSS_UNSUPPORTED_SEMANTIC, f"unsupported operation type {kind!r}")
    file_path = wire.get("path")
    if not isinstance(file_path, str):
        raise ValueError(f"responses: {path}.operation.path must be a string")
    if kind in ("create_file", "update_file"):
        diff = wire.get("diff")
        if not isinstance(diff, str):
            raise ValueError(f"responses: {path}.operation.diff is required for {kind}")
        op_cls = ApplyPatchCreateFileOperation if kind == "create_file" else ApplyPatchUpdateFileOperation
        return op_cls(path=file_path, diff=diff), None
    if kind == "delete_file":
        return ApplyPatchDeleteFileOperation(path=file_path), None
    return None, loss(path, "operation", LOSS_UNSUPPORTED_SEMANTIC, f"unsupported operation type {kind!r}")


def decode_apply_patch_call_status(status: Any, path: str) -> tuple[str | None, Loss | None]:
    if not isinstance(status, str) or not status:
        raise ValueError(f"responses: {path}.status is required")
    if status in ("in_progress", "completed"):
        return status, None
    return None, loss(f"{path}.status", "status", LOSS_UNMAPPED_VALUE, f"unknown apply_patch_call status {status!r}")


def decode_apply_patch_result_status(status: Any, path: str) -> tuple[str | None, Loss | None]:
    if not isinstance(status, str) or not status:
        raise ValueError(f"responses: {path}.status is required")
    if status in ("completed", "failed"):
        return status, None
    return None, loss(f"{path}.status", "status", LOSS_UNMAPPED_VALUE, f"unknown apply_patch_call_output status {status!r}")


def encode_apply_patch_operation(op: ApplyPatchOperation) -> dict[str, Any]:
    if isinstance(op, (ApplyPatchCreateFileOperation, ApplyPatchUpdateFileOperation)):
        return {"type": op.type, "path": op.path, "diff": op.diff}
    if isinstance(op, ApplyPatchDeleteFileOperation):
        return {"type": op.type, "path": op.path}
    raise ValueError(f"responses: unknown apply patch operation {type(op)}")


def precheck_apply_patch_pairs(
    items: list[dict[str, Any]],
) -> tuple[dict[int, Block], dict[int, Block], set[int], list[Loss]]:
    decoded_calls: dict[int, Block] = {}
    decoded_results: dict[int, Block] = {}
    dropped: set[int] = set()
    losses: list[Loss] = []

    i = 0
    while i < len(items):
        item = items[i]
        if not isinstance(item, dict):
            i += 1
            continue
        if item.get("type") == "apply_patch_call_output":
            raise ValueError(f"responses: input[{i}]: apply_patch tool result without preceding tool calls (INV-3)")
        kind = item.get("type", "")
        role = item.get("role", "")
        is_assistant = (
            (kind in ("", ITEM_TYPE_MESSAGE) and role == ROLE_ASSISTANT)
            or kind
            in (
                ITEM_TYPE_FUNCTION_CALL,
                ITEM_TYPE_CUSTOM_TOOL_CALL,
                "apply_patch_call",
                ITEM_TYPE_REASONING,
                ITEM_TYPE_WEB_SEARCH_CALL,
            )
        )
        if not is_assistant:
            i += 1
            continue

        calls: list[dict[str, Any]] = []
        seen_call_ids: set[str] = set()
        a_start = i
        while a_start < len(items):
            it = items[a_start]
            if not isinstance(it, dict):
                break
            k = it.get("type", "")
            r = it.get("role", "")
            cur_kind = ""
            if k == ITEM_TYPE_FUNCTION_CALL:
                cur_kind = "function"
            elif k == ITEM_TYPE_CUSTOM_TOOL_CALL:
                cur_kind = "custom"
            elif k == "apply_patch_call":
                cur_kind = "apply_patch"
            elif k in (ITEM_TYPE_REASONING, ITEM_TYPE_WEB_SEARCH_CALL) or (k in ("", ITEM_TYPE_MESSAGE) and r == ROLE_ASSISTANT):
                pass
            else:
                break
            if cur_kind:
                cid = it.get("call_id", "")
                if cur_kind == "apply_patch" and not cid:
                    raise ValueError(f"responses: input[{a_start}]: apply_patch tool call requires nonempty call_id (INV-3)")
                if cid in seen_call_ids:
                    raise ValueError(f"responses: input[{a_start}]: duplicate call_id {cid!r} in turn (INV-3)")
                seen_call_ids.add(cid)
                calls.append({"call_id": cid, "kind": cur_kind, "index": a_start})
            a_start += 1

        results: list[dict[str, Any]] = []
        o_start = a_start
        while o_start < len(items):
            it = items[o_start]
            if not isinstance(it, dict):
                break
            k = it.get("type", "")
            matched_kind = ""
            if k == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
                matched_kind = "function"
            elif k == ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT:
                matched_kind = "custom"
            elif k == "apply_patch_call_output":
                matched_kind = "apply_patch"
            else:
                break
            cid = it.get("call_id", "")
            if matched_kind == "apply_patch" and not cid:
                raise ValueError(f"responses: input[{o_start}]: apply_patch tool result requires nonempty call_id (INV-3)")
            results.append({"call_id": cid, "kind": matched_kind, "index": o_start})
            o_start += 1

        has_apply_patch = any(c["kind"] == "apply_patch" for c in calls) or any(r["kind"] == "apply_patch" for r in results)

        if has_apply_patch:
            if len(calls) != len(results):
                raise ValueError(f"responses: tool calls count ({len(calls)}) and results count ({len(results)}) mismatch (INV-3)")
            for k in range(len(calls)):
                if calls[k]["call_id"] != results[k]["call_id"] or calls[k]["kind"] != results[k]["kind"]:
                    raise ValueError("responses: tool result identity/type/order mismatch (INV-3)")

            for k in range(len(calls)):
                if calls[k]["kind"] != "apply_patch":
                    continue
                c_idx = calls[k]["index"]
                o_idx = results[k]["index"]
                c_item = items[c_idx]
                o_item = items[o_idx]

                op, op_loss = decode_apply_patch_operation(c_item.get("operation"), f"input[{c_idx}]")
                c_status, cs_loss = decode_apply_patch_call_status(c_item.get("status"), f"input[{c_idx}]")
                r_status, rs_loss = decode_apply_patch_result_status(o_item.get("status"), f"input[{o_idx}]")
                out = o_item.get("output")
                if out is not None and not isinstance(out, str):
                    raise ValueError(f"responses: input[{o_idx}].output must be a string")

                has_call_loss = op_loss is not None or cs_loss is not None
                has_res_loss = rs_loss is not None

                if has_call_loss or has_res_loss:
                    dropped.add(c_idx)
                    dropped.add(o_idx)
                    if has_call_loss:
                        if op_loss is not None:
                            losses.append(op_loss)
                        if cs_loss is not None:
                            losses.append(cs_loss)
                    else:
                        losses.append(loss(f"input[{c_idx}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "paired apply_patch result has unsupported semantic"))
                    if has_res_loss:
                        if rs_loss is not None:
                            losses.append(rs_loss)
                    else:
                        losses.append(loss(f"input[{o_idx}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "paired apply_patch call has unsupported semantic"))
                else:
                    decoded_calls[c_idx] = ApplyPatchToolUseBlock(
                        id=c_item.get("call_id", ""),
                        status=c_status,
                        operation=op,
                    )
                    decoded_results[o_idx] = ApplyPatchToolResultBlock(
                        tool_use_id=o_item.get("call_id", ""),
                        status=r_status,
                        output=out,
                    )

        i = o_start

    return decoded_calls, decoded_results, dropped, losses


def decode_assistant_run(
    items: list[dict[str, Any]],
    start: int,
    decoded_calls: dict[int, Block] | None = None,
    dropped: set[int] | None = None,
) -> tuple[IrMessage | None, int, list[Loss]]:
    """N-R-5 merges assistant items and function-call items into one assistant message."""
    thinkings: list[Block] = []
    text: list[Block] = []
    calls: list[Block] = []
    losses: list[Loss] = []
    index = start

    while index < len(items):
        item = items[index]
        kind = item.get("type", "")
        if kind == ITEM_TYPE_WEB_SEARCH_CALL:
            from oxa.openai.responses.web_search import decode_web_search_item

            block, call_losses = decode_web_search_item(item, f"input[{index}]")
            losses.extend(call_losses)
            calls.append(block)
            index += 1
            continue
        if kind == ITEM_TYPE_REASONING:
            for part_index, part in enumerate(item.get("summary", [])):
                part_type = part.get("type", "")
                if part_type in (PART_TYPE_OUTPUT_TEXT, "summary_text"):
                    if part.get("annotations"):
                        losses.append(
                            loss(
                                f"input[{index}].summary[{part_index}].annotations",
                                "annotations",
                                LOSS_UNMAPPED_FIELD,
                                "Responses reasoning summary annotations have no IR equivalent",
                            )
                        )
                    thinkings.append(ThinkingBlock(thinking=part.get("text", "")))
                else:
                    losses.append(
                        loss(
                            f"input[{index}].summary[{part_index}]",
                            "type",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            f"Responses reasoning summary type {part_type!r} has no IR equivalent",
                        )
                    )
            index += 1
            continue
        if kind == "apply_patch_call":
            if dropped is not None and index in dropped:
                index += 1
                continue
            if decoded_calls is not None and index in decoded_calls:
                calls.append(decoded_calls[index])
                index += 1
                continue
            raise ValueError(f"responses: input[{index}]: unpaired apply_patch_call (INV-3)")
        if kind == ITEM_TYPE_FUNCTION_CALL:
            raw_name = item.get("name", "")
            ns = item.get("namespace", "")
            final_name = f"{ns}.{raw_name}" if ns and not raw_name.startswith(ns + ".") else raw_name
            calls.append(ToolUseBlock(id=item.get("call_id", ""), name=final_name, input=item.get("arguments", "")))
            index += 1
            continue
        if kind == ITEM_TYPE_CUSTOM_TOOL_CALL:
            if "input" not in item:
                raise ValueError(f"responses: input[{index}].input is missing")
            raw_name = item.get("name", "")
            ns = item.get("namespace", "")
            final_name = f"{ns}.{raw_name}" if ns and not raw_name.startswith(ns + ".") else raw_name
            calls.append(CustomToolUseBlock(id=item.get("call_id", ""), name=final_name, input=item["input"]))
            index += 1
            continue

        if kind not in ("", ITEM_TYPE_MESSAGE) or item.get("role") != ROLE_ASSISTANT:
            break

        blocks, b_losses = decode_content(item.get("content"), f"input[{index}].content", is_user=False)
        text.extend(blocks)
        losses.extend(b_losses)
        index += 1

    content = thinkings + text + calls
    if not content:
        return None, index, losses
    return IrMessage(role=IR_ROLE_ASSISTANT, content=content), index, losses


def decode_output_run(
    items: list[dict[str, Any]],
    start: int,
    decoded_results: dict[int, Block] | None = None,
    dropped: set[int] | None = None,
) -> tuple[IrMessage | None, int, list[Loss]]:
    """Merge maximal function/custom result runs into a single user turn."""
    content: list[Block] = []
    losses: list[Loss] = []
    index = start
    while index < len(items) and items[index].get("type") in (
        ITEM_TYPE_FUNCTION_CALL_OUTPUT,
        ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
        "apply_patch_call_output",
    ):
        item = items[index]
        if item.get("type") == "apply_patch_call_output":
            if dropped is not None and index in dropped:
                index += 1
                continue
            if decoded_results is not None and index in decoded_results:
                content.append(decoded_results[index])
                index += 1
                continue
            raise ValueError(f"responses: input[{index}]: unpaired apply_patch_call_output (INV-3)")
        if item.get("type") == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
            content.append(ToolResultBlock(tool_use_id=item.get("call_id", ""), content=[TextBlock(text=item.get("output", ""))]))
        else:
            if "output" not in item:
                raise ValueError(f"responses: input[{index}].output is missing")
            output = item["output"]
            if isinstance(output, str):
                content.append(CustomToolResultBlock(tool_use_id=item.get("call_id", ""), output=output))
            elif isinstance(output, list):
                mapped: list[TextBlock | ImageBlock] = []
                for part_index, part in enumerate(output):
                    path = f"input[{index}].output[{part_index}]"
                    if not isinstance(part, dict):
                        losses.append(loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
                        continue
                    ptype = part.get("type", "")
                    if ptype == PART_TYPE_INPUT_TEXT:
                        mapped.append(TextBlock(text=part.get("text", "")))
                    elif ptype == PART_TYPE_INPUT_IMAGE:
                        image, img_loss = decode_image_url(part.get("image_url", ""), f"{path}.image_url")
                        if img_loss is None and isinstance(image, ImageBlock):
                            mapped.append(image)
                        else:
                            losses.append(img_loss)
                    else:
                        losses.append(loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
                content.append(CustomToolResultBlock(tool_use_id=item.get("call_id", ""), output=mapped))
            else:
                raise ValueError(f"responses: input[{index}].output must be a string or array")
        index += 1
    if not content:
        return None, index, losses
    return IrMessage(role=IR_ROLE_USER, content=content), index, losses


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
        ns = value.get("namespace", "")
        if ns and not name.startswith(ns + "."):
            name = f"{ns}.{name}"
        if kind == "apply_patch":
            return ToolChoice(mode=TOOL_CHOICE_TOOL, tool_type="apply_patch"), []
        if kind in (TOOL_TYPE_FUNCTION, TOOL_TYPE_CUSTOM) and name:
            return ToolChoice(mode=TOOL_CHOICE_TOOL, name=name, tool_type=kind), []
        return None, [loss("tool_choice", "tool_choice", LOSS_UNSUPPORTED_SEMANTIC, "only named function or custom Responses tool_choice values are supported")]
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
        if choice.tool_type == "apply_patch":
            return {"type": "apply_patch"}, None
        if choice.name:
            kind = TOOL_TYPE_CUSTOM if choice.tool_type == "custom" else TOOL_TYPE_FUNCTION
            dot_idx = choice.name.find(".")
            if dot_idx > 0:
                return {
                    "type": kind,
                    "namespace": choice.name[:dot_idx],
                    "name": choice.name[dot_idx + 1:],
                }, None
            return {"type": kind, "name": choice.name}, None
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
    other_blocks = 0
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, TextBlock):
            p_dict: dict[str, Any] = {"type": PART_TYPE_INPUT_TEXT, "text": block.text}
            for ann in getattr(block, "native", ()):
                if ann.source == "openai" and ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT:
                    p_dict["prompt_cache_breakpoint"] = json.loads(ann.value)
                    other_blocks += 1
                    break
            parts.append(p_dict)
            text_chunks.append(block.text)
            if block.citations:
                has_anthropic = any(c.source != "openai" for c in block.citations)
                has_openai = any(c.source == "openai" for c in block.citations)
                if has_anthropic:
                    losses.append(
                        loss(
                            f"{path}[{index}].citations",
                            "citations",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            "anthropic text citations; the target face cannot preserve source locations and replay metadata",
                        )
                    )
                if has_openai:
                    losses.append(
                        loss(
                            f"{path}[{index}].citations",
                            "citations",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            "openai text annotations; the target face cannot preserve source locations and replay metadata",
                        )
                    )
        elif isinstance(block, RedactedThinkingBlock):
            other_blocks += 1
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "anthropic encrypted thinking replay state; the target face cannot preserve this block",
                )
            )
        elif isinstance(block, ImageBlock):
            part, img_loss = encode_image_block(
                block.media_type, block.data, block.url, f"{path}[{index}]"
            )
            if img_loss is not None:
                losses.append(img_loss)
                continue
            if part is not None:
                has_image = True
                other_blocks += 1
                for ann in getattr(block, "native", ()):
                    if ann.source == "openai" and ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT:
                        part["prompt_cache_breakpoint"] = json.loads(ann.value)
                        break
                parts.append(part)
        elif isinstance(block, DocumentBlock):
            other_blocks += 1
            s_count = sum(1 for x in (block.file_id, block.url, block.data) if x)
            if s_count == 0:
                losses.append(
                    loss(
                        f"{path}[{index}]",
                        "content",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "IR DocumentBlock carries no supported file source",
                    )
                )
                continue
            if s_count > 1:
                losses.append(
                    loss(
                        f"{path}[{index}]",
                        "content",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "IR DocumentBlock carries conflicting file sources",
                    )
                )
                continue
            pcb = next((json.loads(ann.value) for ann in getattr(block, "native", ()) if ann.source == "openai" and ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT), None)
            f_dict: dict[str, Any]
            if block.file_id:
                f_dict = {"type": "input_file", "file_id": block.file_id}
            elif block.url:
                f_dict = {"type": "input_file", "file_url": block.url}
            else:
                f_dict = {"type": "input_file", "file_data": block.data}
            if pcb is not None:
                f_dict["prompt_cache_breakpoint"] = pcb
            parts.append(f_dict)
        else:
            other_blocks += 1
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block cannot be rendered in a Responses user message item",
                )
            )

    if len(text_chunks) <= 1 and not has_image and other_blocks == 0:
        return "".join(text_chunks), losses
    return parts, losses


def encode_user_message(
    message: IrMessage,
    path: str,
) -> tuple[list[dict[str, Any]], list[Loss]]:
    normal: list[Block] = []
    results: list[ToolResultBlock | CustomToolResultBlock | ApplyPatchToolResultBlock] = []
    first_normal: int | None = None
    last_result: int | None = None
    losses: list[Loss] = []

    for pos, block in enumerate(message.content):
        if isinstance(block, (ToolResultBlock, CustomToolResultBlock, ApplyPatchToolResultBlock)):
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
        if isinstance(res, ApplyPatchToolResultBlock):
            item_out: dict[str, Any] = {
                "type": "apply_patch_call_output",
                "call_id": res.tool_use_id,
                "status": res.status,
            }
            if res.output is not None:
                item_out["output"] = res.output
            items.append(item_out)
            continue
        if isinstance(res, CustomToolResultBlock):
            if isinstance(res.output, str):
                output = res.output
            else:
                output = []
                for c_idx, inner in enumerate(res.output):
                    if isinstance(inner, TextBlock):
                        output.append({"type": PART_TYPE_INPUT_TEXT, "text": inner.text})
                    elif isinstance(inner, ImageBlock):
                        part, img_loss = encode_image_block(inner.media_type, inner.data, inner.url, f"{path}.content[{index}].output[{c_idx}]")
                        if img_loss is not None: losses.append(img_loss)
                        elif part is not None: output.append(part)
                    else:
                        losses.append(loss(f"{path}.content[{index}].output[{c_idx}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
            items.append({"type": ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT, "call_id": res.tool_use_id, "output": output})
            continue
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
    thinkings: list[dict[str, Any]] = []
    text_chunks: list[str] = []
    has_text = False
    has_refusal = False
    parts: list[dict[str, Any]] = []
    calls: list[dict[str, Any]] = []
    losses: list[Loss] = []

    for index, block in enumerate(blocks):
        if isinstance(block, RefusalBlock):
            has_refusal = True
            if block.category:
                losses.append(
                    loss(
                        f"{path}[{index}].category",
                        "category",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "Anthropic refusal category has no Responses equivalent",
                    )
                )
            parts.append({"type": "refusal", "refusal": block.refusal})
        elif isinstance(block, TextBlock):
            text_chunks.append(block.text)
            has_text = True
            parts.append({"type": PART_TYPE_OUTPUT_TEXT, "text": block.text})
            if block.citations:
                has_anthropic = any(c.source != "openai" for c in block.citations)
                has_openai = any(c.source == "openai" for c in block.citations)
                if has_anthropic:
                    losses.append(
                        loss(
                            f"{path}[{index}].citations",
                            "citations",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            "anthropic text citations; the target face cannot preserve source locations and replay metadata",
                        )
                    )
                if has_openai:
                    losses.append(
                        loss(
                            f"{path}[{index}].citations",
                            "citations",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            "openai text annotations; the target face cannot preserve source locations and replay metadata",
                        )
                    )
        elif isinstance(block, RedactedThinkingBlock):
            losses.append(
                loss(
                    f"{path}[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "anthropic encrypted thinking replay state; the target face cannot preserve this block",
                )
            )
        elif isinstance(block, ThinkingBlock):
            thinkings.append(
                {
                    "type": PART_TYPE_OUTPUT_TEXT,
                    "text": block.thinking,
                    "annotations": [],
                }
            )
            if block.signature:
                losses.append(
                    loss(
                        f"{path}[{index}].signature",
                        "signature",
                        LOSS_UNMAPPED_FIELD,
                        "Responses reasoning input items carry no provider signature",
                    )
                )
        elif isinstance(block, ApplyPatchToolUseBlock):
            calls.append({
                "type": "apply_patch_call",
                "call_id": block.id,
                "status": block.status,
                "operation": encode_apply_patch_operation(block.operation),
            })
        elif isinstance(block, WebSearchCallBlock):
            from oxa.ir.web_search import inspect_web_search, validate_web_search_extra
            inspect_web_search(block.id, block.status, block.action)
            validate_web_search_extra(block.extra)
            calls.append({
                **block.extra,
                "type": ITEM_TYPE_WEB_SEARCH_CALL,
                "id": block.id,
                "status": block.status,
                "action": block.action,
            })
        elif isinstance(block, ToolUseBlock):
            dot_idx = block.name.find(".")
            call_dict = {"type": ITEM_TYPE_FUNCTION_CALL, "call_id": block.id, "arguments": block.input}
            if dot_idx > 0:
                call_dict["namespace"] = block.name[:dot_idx]
                call_dict["name"] = block.name[dot_idx + 1:]
            else:
                call_dict["name"] = block.name
            calls.append(call_dict)
        elif isinstance(block, CustomToolUseBlock):
            dot_idx = block.name.find(".")
            call_dict = {"type": ITEM_TYPE_CUSTOM_TOOL_CALL, "call_id": block.id, "input": block.input}
            if dot_idx > 0:
                call_dict["namespace"] = block.name[:dot_idx]
                call_dict["name"] = block.name[dot_idx + 1:]
            else:
                call_dict["name"] = block.name
            calls.append(call_dict)
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
    if thinkings:
        items.append({"type": ITEM_TYPE_REASONING, "summary": thinkings})
    if has_refusal:
        items.append({"role": ROLE_ASSISTANT, "content": parts})
    elif has_text or not blocks:
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
