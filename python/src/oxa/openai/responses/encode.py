"""OpenAI Responses IR → face conversions (spec/11 §4)."""

from __future__ import annotations

from typing import Any

from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    CustomToolResultBlock,
    CustomToolUseBlock,
    CustomToolFormatGrammar,
    CustomToolFormatText,
    ImageBlock,
    Loss,
    Request,
    Response,
    ResponseFormat,
    ResponseFormatJsonObject,
    ResponseFormatJsonSchema,
    ResponseFormatText,
    TextBlock,
    ThinkingBlock,
    ToolUseBlock,
)
from oxa.ir.native import omitted_native_request_losses
from oxa.ir import (
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
)
from oxa.ir import (
    ROLE_USER as IR_ROLE_USER,
)
from oxa.modelmap import Table
from oxa.openai.responses.constants import (
    ERROR_CODE_REFUSAL,
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING,
    OBJECT_RESPONSE,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_USER,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_INCOMPLETE,
    TOOL_TYPE_FUNCTION,
    TOOL_TYPE_CUSTOM,
)
from oxa.openai.responses.normalize import (
    encode_apply_patch_operation,
    encode_assistant_message,
    encode_image_block,
    encode_tool_choice,
    encode_user_message,
    loss,
)


import json
from oxa.ir.native import (
    NATIVE_SOURCE_OPENAI,
    NATIVE_KIND_CONTAINER,
    NATIVE_KIND_DEFER_LOADING,
)


def encode_responses_grouped_tools(tools: Sequence[Tool], losses: list[Loss]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    i = 0
    while i < len(tools):
        tool = tools[i]
        if tool.type == "native":
            if tool.source == "openai":
                out.append(json.loads(tool.definition))
            else:
                losses.append(
                    loss(
                        f"tools[{i}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f'native tool "{tool.name}" from "{tool.source}" cannot be represented by the target protocol',
                    )
                )
            i += 1
            continue

        if tool.type == "apply_patch":
            out.append({"type": "apply_patch"})
            i += 1
            continue

        container_kind = ""
        for ann in tool.native or ():
            if ann.source == NATIVE_SOURCE_OPENAI and ann.kind == NATIVE_KIND_CONTAINER:
                try:
                    container_kind = json.loads(ann.value)
                except Exception:
                    container_kind = ann.value.strip('"')
                break

        dot_idx = tool.name.find(".") if tool.name else -1
        if dot_idx > 0 and tool.name:
            prefix = tool.name[:dot_idx]
            if not container_kind:
                container_kind = "namespace"
            child_tools: list[dict[str, Any]] = []
            j = i
            while j < len(tools):
                next_tool = tools[j]
                if not next_tool.name or not next_tool.name.startswith(prefix + "."):
                    break
                stripped_name = next_tool.name[len(prefix) + 1:]
                has_defer = any(ann.kind == NATIVE_KIND_DEFER_LOADING and ann.value == "true" for ann in (next_tool.native or ()))
                if next_tool.type == "custom":
                    c_dict: dict[str, Any] = {"type": TOOL_TYPE_CUSTOM, "name": stripped_name}
                    if next_tool.description is not None:
                        c_dict["description"] = next_tool.description
                    if next_tool.format is not None:
                        c_dict["format"] = encode_custom_tool_format(next_tool.format)
                    if has_defer:
                        c_dict["defer_loading"] = True
                    child_tools.append(c_dict)
                else:
                    c_dict = {"type": TOOL_TYPE_FUNCTION, "name": stripped_name, "parameters": next_tool.input_schema}
                    if next_tool.description:
                        c_dict["description"] = next_tool.description
                    if has_defer:
                        c_dict["defer_loading"] = True
                    child_tools.append(c_dict)
                j += 1
            out.append({
                "type": container_kind,
                "name": prefix,
                "tools": child_tools,
            })
            i = j
            continue

        has_defer = any(ann.kind == NATIVE_KIND_DEFER_LOADING and ann.value == "true" for ann in (tool.native or ()))
        if tool.type == "custom":
            t_dict = {"type": TOOL_TYPE_CUSTOM, "name": tool.name}
            if tool.description is not None:
                t_dict["description"] = tool.description
            if tool.format is not None:
                t_dict["format"] = encode_custom_tool_format(tool.format)
            if has_defer:
                t_dict["defer_loading"] = True
            out.append(t_dict)
        else:
            if tool.input_schema is None:
                raise ValueError(f"responses: tools[{len(out)}].input_schema is required")
            t_dict = {"type": TOOL_TYPE_FUNCTION, "name": tool.name, "parameters": tool.input_schema}
            if tool.description:
                t_dict["description"] = tool.description
            if has_defer:
                t_dict["defer_loading"] = True
            out.append(t_dict)
        i += 1
    return out


def encode_request(
    req: Request,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR request to a Responses wire request (IR → face)."""
    losses: list[Loss] = []

    if req.metadata:
        losses.append(
            loss(
                "metadata",
                "metadata",
                LOSS_UNMAPPED_FIELD,
                "Responses requests have a string-valued metadata field with no IR equivalent; the IR metadata map is dropped.",
            )
        )

    model = req.model
    if table is not None:
        model = table.map(model)

    out: dict[str, Any] = {
        "model": model,
    }

    if req.system:
        instructions = "".join(b.text for b in req.system)
        out["instructions"] = instructions

    tools_wire: list[dict[str, Any]] = []
    if req.tools:
        tools_wire = encode_responses_grouped_tools(req.tools, losses)
        if tools_wire:
            out["tools"] = tools_wire

    native_named_choice_dropped = (
        req.tool_choice is not None
        and req.tool_choice.mode == "tool"
        and (
            req.tool_choice.tool_type == "native"
            or any(t.type == "native" and t.name == req.tool_choice.name for t in (req.tools or ()))
        )
        and not any(t.type == "native" and t.name == req.tool_choice.name and t.source == "openai" for t in (req.tools or ()))
    )
    choice_wire, choice_loss = encode_tool_choice(req.tool_choice)
    if req.tools and not tools_wire and req.tool_choice is not None and req.tool_choice.mode == "any":
        choice_wire = None
        choice_loss = loss("tool_choice", "tool_choice", LOSS_UNSUPPORTED_SEMANTIC, "all tool declarations were dropped")
    elif native_named_choice_dropped:
        choice_wire = None
        choice_loss = loss(
            "tool_choice",
            "tool_choice",
            LOSS_UNSUPPORTED_SEMANTIC,
            f'native tool choice "{req.tool_choice.name}" cannot be represented by the target protocol',
        )
    if choice_wire is not None:
        out["tool_choice"] = choice_wire
    if choice_loss is not None:
        losses.append(choice_loss)

    items: list[dict[str, Any]] = []
    for index, message in enumerate(req.messages):
        if message.role == IR_ROLE_USER:
            msg_items, m_losses = encode_user_message(message, f"messages[{index}]")
            items.extend(msg_items)
            losses.extend(m_losses)
        elif message.role == IR_ROLE_ASSISTANT:
            msg_items, m_losses = encode_assistant_message(
                message.content, f"messages[{index}].content"
            )
            items.extend(msg_items)
            losses.extend(m_losses)

    if (
        len(req.system) == 0
        and len(items) == 1
        and items[0].get("role") == ROLE_USER
        and isinstance(items[0].get("content"), str)
    ):
        out["input"] = items[0]["content"]
    else:
        out["input"] = items

    if req.params:
        if req.params.temperature is not None:
            out["temperature"] = req.params.temperature
        if req.params.top_p is not None:
            out["top_p"] = req.params.top_p
        if req.params.max_tokens is not None:
            out["max_output_tokens"] = req.params.max_tokens
        if req.params.reasoning_effort is not None:
            out["reasoning"] = {"effort": req.params.reasoning_effort}
        if req.params.response_format is not None:
            out["text"] = {"format": encode_text_format(req.params.response_format)}
        if req.params.stop_sequences:
            losses.append(
                loss(
                    "params.stop_sequences",
                    "stop_sequences",
                    LOSS_UNMAPPED_FIELD,
                    "Responses requests have no stop-sequences parameter; the IR stop sequences are dropped.",
                )
            )

    losses.extend(omitted_native_request_losses(req, "responses"))

    return out, losses


def encode_custom_tool_format(fmt: Any) -> dict[str, Any]:
    if isinstance(fmt, CustomToolFormatText):
        return {"type": "text"}
    if isinstance(fmt, CustomToolFormatGrammar):
        if fmt.syntax not in ("lark", "regex"):
            raise ValueError("responses: custom grammar syntax must be lark or regex")
        return {"type": "grammar", "syntax": fmt.syntax, "definition": fmt.definition}
    raise ValueError(f"responses: unknown custom tool format {type(fmt)}")


def encode_text_format(rf: ResponseFormat) -> dict[str, Any]:
    if isinstance(rf, ResponseFormatText):
        return {"type": "text"}
    if isinstance(rf, ResponseFormatJsonObject):
        return {"type": "json_object"}
    if isinstance(rf, ResponseFormatJsonSchema):
        res: dict[str, Any] = {
            "type": "json_schema",
            "name": rf.name,
            "schema": rf.schema,
        }
        if rf.description is not None:
            res["description"] = rf.description
        if rf.strict is not None:
            res["strict"] = rf.strict
        return res
    raise ValueError(f"responses: unknown response format type: {type(rf)}")


def encode_response(
    resp: Response,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR response to a Responses wire response (IR → face)."""
    losses: list[Loss] = []
    thinkings: list[dict[str, Any]] = []
    text_chunks: list[str] = []
    has_text = False
    calls: list[dict[str, Any]] = []
    apc_index = 0
    apco_index = 0

    for index, block in enumerate(resp.content):
        if isinstance(block, TextBlock):
            text_chunks.append(block.text)
            has_text = True
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
                        f"content[{index}].signature",
                        "signature",
                        LOSS_UNMAPPED_FIELD,
                        "Responses reasoning output items carry no provider signature",
                    )
                )
        elif isinstance(block, ApplyPatchToolUseBlock):
            calls.append({
                "type": "apply_patch_call",
                "id": f"apc_{apc_index}",
                "status": block.status,
                "call_id": block.id,
                "operation": encode_apply_patch_operation(block.operation),
            })
            apc_index += 1
        elif isinstance(block, ApplyPatchToolResultBlock):
            item = {
                "type": "apply_patch_call_output",
                "id": f"apco_{apco_index}",
                "status": block.status,
                "call_id": block.tool_use_id,
            }
            if block.output is not None:
                item["output"] = block.output
            calls.append(item)
            apco_index += 1
        elif isinstance(block, ToolUseBlock):
            dot_idx = block.name.find(".")
            call_dict = {"type": ITEM_TYPE_FUNCTION_CALL, "id": "fc_abc123", "status": STATUS_COMPLETED, "call_id": block.id, "arguments": block.input}
            if dot_idx > 0:
                call_dict["namespace"] = block.name[:dot_idx]
                call_dict["name"] = block.name[dot_idx + 1:]
            else:
                call_dict["name"] = block.name
            calls.append(call_dict)
        elif isinstance(block, CustomToolUseBlock):
            dot_idx = block.name.find(".")
            call_dict = {"type": ITEM_TYPE_CUSTOM_TOOL_CALL, "id": "ctc_abc123", "call_id": block.id, "input": block.input}
            if dot_idx > 0:
                call_dict["namespace"] = block.name[:dot_idx]
                call_dict["name"] = block.name[dot_idx + 1:]
            else:
                call_dict["name"] = block.name
            calls.append(call_dict)
        elif isinstance(block, CustomToolResultBlock):
            if isinstance(block.output, str):
                output = block.output
            else:
                output = []
                for c_idx, inner in enumerate(block.output):
                    if isinstance(inner, TextBlock):
                        output.append({"type": "input_text", "text": inner.text})
                    elif isinstance(inner, ImageBlock):
                        part, img_loss = encode_image_block(inner.media_type, inner.data, inner.url, f"content[{index}].output[{c_idx}]")
                        if img_loss is not None: losses.append(img_loss)
                        elif part is not None: output.append(part)
                    else:
                        losses.append(loss(f"content[{index}].output[{c_idx}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
            calls.append({"type": ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT, "call_id": block.tool_use_id, "output": output})
        else:
            losses.append(
                loss(
                    f"content[{index}]",
                    "content",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "this IR block cannot be rendered in a Responses output item",
                )
            )

    output = list(calls)
    if has_text or not resp.content:
        output.insert(
            0,
            {
                "type": ITEM_TYPE_MESSAGE,
                "id": "msg_abc123",
                "status": STATUS_COMPLETED,
                "role": ROLE_ASSISTANT,
                "content": [
                    {
                        "type": PART_TYPE_OUTPUT_TEXT,
                        "text": "".join(text_chunks),
                        "annotations": [],
                    }
                ],
            },
        )
    if thinkings:
        output.insert(
            0,
            {
                "type": ITEM_TYPE_REASONING,
                "id": "rs_abc123",
                "summary": thinkings,
            },
        )

    status = STATUS_COMPLETED
    incomplete_details = None
    error = None

    if resp.stop_reason in (STOP_END_TURN, STOP_TOOL_USE):
        status = STATUS_COMPLETED
    elif resp.stop_reason == STOP_MAX_TOKENS:
        status = STATUS_INCOMPLETE
        incomplete_details = {"reason": INCOMPLETE_REASON_MAX_OUTPUT_TOKENS}
    elif resp.stop_reason == STOP_STOP_SEQUENCE:
        losses.append(
            loss(
                "",
                "stop_sequence",
                LOSS_UNMAPPED_VALUE,
                "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
            )
        )
        status = STATUS_COMPLETED
    elif resp.stop_reason == STOP_REFUSAL:
        status = STATUS_FAILED
        error = {"code": ERROR_CODE_REFUSAL, "message": ""}
    else:
        raise ValueError(f"responses: stop reason {resp.stop_reason!r} has no Responses equivalent")

    model = resp.model
    if table is not None:
        model = table.map(model)

    usage: dict[str, Any] = {
        "input_tokens": resp.usage.input_tokens,
        "output_tokens": resp.usage.output_tokens,
        "total_tokens": resp.usage.input_tokens + resp.usage.output_tokens,
    }
    if resp.usage.input_tokens_details is not None:
        usage["input_token_details"] = {
            "cached_tokens": resp.usage.input_tokens_details.cached_tokens
        }
    if resp.usage.output_tokens_details is not None:
        usage["output_token_details"] = {
            "reasoning_tokens": resp.usage.output_tokens_details.reasoning_tokens
        }

    out: dict[str, Any] = {
        "id": resp.id,
        "object": OBJECT_RESPONSE,
        "status": status,
        "model": model,
        "output": output,
        "usage": usage,
    }
    if incomplete_details is not None:
        out["incomplete_details"] = incomplete_details
    if error is not None:
        out["error"] = error

    return out, losses
