"""Chat Completions IR → face conversions (spec/10 §4)."""

from __future__ import annotations

import json
from typing import Any

from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    ApplyPatchToolResultBlock,
    Block,
    CustomToolFormatGrammar,
    CustomToolResultBlock,
    Loss,
    Request,
    Response,
    ResponseFormat,
    ResponseFormatJsonObject,
    ResponseFormatJsonSchema,
    ResponseFormatText,
    ToolResultBlock,
)
from oxa.ir.native import omitted_native_request_losses
from oxa.ir import (
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
)
from oxa.ir import (
    ROLE_USER as IR_ROLE_USER,
)
from oxa.modelmap import Table
from oxa.openai.chatcompletions.constants import (
    FINISH_REASON_CONTENT_FILTER,
    FINISH_REASON_LENGTH,
    FINISH_REASON_STOP,
    FINISH_REASON_TOOL_CALLS,
    OBJECT_CHAT_COMPLETION,
    ROLE_DEVELOPER,
    ROLE_SYSTEM,
    ROLE_USER,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.chatcompletions.normalize import (
    encode_assistant_message,
    encode_custom_tool_result,
    encode_tool_choice,
    encode_tool_result,
    encode_user_content,
    loss,
)


def encode_request(
    req: Request,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR request to a Chat Completions wire request (IR → face)."""
    losses: list[Loss] = []

    model = req.model
    if table is not None:
        model = table.map(model)

    out: dict[str, Any] = {
        "model": model,
        "messages": [],
    }
    if req.metadata:
        out["metadata"] = dict(req.metadata)

    tools_wire: list[dict[str, Any]] = []
    if req.tools:
        for index, tool in enumerate(req.tools):
            if tool.type == "native":
                losses.append(
                    loss(
                        f"tools[{index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f'native tool "{tool.name}" from "{tool.source}" cannot be represented by the target protocol',
                    )
                )
                continue
            if tool.type == "custom":
                c_dict: dict[str, Any] = {"name": tool.name}
                if tool.description is not None:
                    c_dict["description"] = tool.description
                if tool.format is not None:
                    if isinstance(tool.format, CustomToolFormatGrammar):
                        c_dict["format"] = {
                            "type": "grammar",
                            "syntax": tool.format.syntax,
                            "definition": tool.format.definition,
                        }
                    else:
                        c_dict["format"] = {"type": "text"}
                tools_wire.append({"type": "custom", "custom": c_dict})
                continue
            if tool.type == "apply_patch":
                losses.append(loss(f"tools[{index}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tools have no Chat Completions mapping"))
                continue
            func: dict[str, Any] = {"name": tool.name}
            if tool.description:
                func["description"] = tool.description
            if tool.input_schema is not None:
                func["parameters"] = tool.input_schema
            if tool.strict is not None:
                func["strict"] = tool.strict
            tools_wire.append({"type": TOOL_TYPE_FUNCTION, "function": func})
        if tools_wire:
            out["tools"] = tools_wire

    native_named_choice_dropped = (
        req.tool_choice is not None
        and req.tool_choice.mode == "tool"
        and (
            req.tool_choice.tool_type == "native"
            or any(t.type == "native" and t.name == req.tool_choice.name for t in (req.tools or ()))
        )
        and not any(
            (t.get("function", {}).get("name") == req.tool_choice.name or t.get("custom", {}).get("name") == req.tool_choice.name)
            for t in tools_wire
        )
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

    if req.system:
        if any(getattr(block, "role", "") == "developer" for block in req.system):
            for block in req.system:
                role = ROLE_DEVELOPER if getattr(block, "role", "") == "developer" else ROLE_SYSTEM
                out["messages"].append({"role": role, "content": block.text})
        else:
            text = "".join(block.text for block in req.system)
            out["messages"].append({"role": ROLE_SYSTEM, "content": text})

    for index, message in enumerate(req.messages):
        if message.role == IR_ROLE_ASSISTANT:
            msg, msg_losses = encode_assistant_message(
                message.content, f"messages[{index}].content"
            )
            out["messages"].append(msg)
            losses.extend(msg_losses)
        elif message.role == IR_ROLE_USER:
            normal: list[Block] = []
            results: list[Block] = []
            first_normal: int | None = None
            last_result: int | None = None

            for pos, block in enumerate(message.content):
                if isinstance(block, (ToolResultBlock, CustomToolResultBlock, ApplyPatchToolResultBlock)):
                    if isinstance(block, (ToolResultBlock, CustomToolResultBlock)):
                        results.append(block)
                    else:
                        losses.append(loss(f"messages[{index}].content[{pos}]", "type", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool results have no Chat Completions mapping"))
                    last_result = pos
                else:
                    if first_normal is None:
                        first_normal = pos
                    normal.append(block)

            if first_normal is not None and last_result is not None and first_normal < last_result:
                losses.append(
                    loss(
                        f"messages[{index}]",
                        "ordering",
                        LOSS_DEGRADED,
                        "N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved",
                    )
                )

            for pos, res in enumerate(results):
                if isinstance(res, CustomToolResultBlock):
                    tool_msg, tool_losses = encode_custom_tool_result(res, f"messages[{index}].content[{pos}]")
                else:
                    tool_msg, tool_losses = encode_tool_result(res, f"messages[{index}].content[{pos}]")
                out["messages"].append(tool_msg)
                losses.extend(tool_losses)

            if normal or not results:
                content, content_losses = encode_user_content(normal, f"messages[{index}].content")
                out["messages"].append({"role": ROLE_USER, "content": content})
                losses.extend(content_losses)
        else:
            raise ValueError(f"chatcompletions: messages[{index}]: unknown role {message.role!r}")

    if req.params:
        if req.params.temperature is not None:
            out["temperature"] = req.params.temperature
        if req.params.top_p is not None:
            out["top_p"] = req.params.top_p
        if req.params.max_tokens is not None:
            out["max_tokens"] = req.params.max_tokens
        if req.params.stop_sequences:
            out["stop"] = req.params.stop_sequences
        if req.params.reasoning_effort is not None:
            out["reasoning_effort"] = req.params.reasoning_effort
        if req.params.verbosity is not None:
            out["verbosity"] = req.params.verbosity
        if req.params.response_format is not None:
            rf = req.params.response_format
            if isinstance(rf, ResponseFormatJsonSchema) and not rf.name:
                losses.append(loss("params.response_format", "name", LOSS_UNSUPPORTED_SEMANTIC, "Chat Completions requires a non-empty schema name"))
            else:
                out["response_format"] = encode_response_format(rf)

    if req.native:
        has_max_completion_tokens = any(
            n.source == "openai" and n.kind == "max_completion_tokens"
            for n in req.native
        )
        if has_max_completion_tokens and "max_tokens" in out:
            out["max_completion_tokens"] = out.pop("max_tokens")
        for n in req.native:
            if n.source == "openai":
                if n.kind == "store":
                    try:
                        val = json.loads(n.value)
                        if isinstance(val, bool) or val is None:
                            out["store"] = val
                    except Exception:
                        pass
                elif n.kind == "stream":
                    try:
                        val = json.loads(n.value)
                        if isinstance(val, bool) or val is None:
                            out["stream"] = val
                    except Exception:
                        pass
                elif n.kind == "stream_options":
                    try:
                        val = json.loads(n.value)
                        if isinstance(val, dict) or val is None:
                            out["stream_options"] = val
                    except Exception:
                        pass

    losses.extend(omitted_native_request_losses(req, "chatcompletions"))

    return out, losses


def encode_response_format(rf: ResponseFormat) -> dict[str, Any]:
    if isinstance(rf, ResponseFormatText):
        return {"type": "text"}
    if isinstance(rf, ResponseFormatJsonObject):
        return {"type": "json_object"}
    if isinstance(rf, ResponseFormatJsonSchema):
        js: dict[str, Any] = {
            "name": rf.name,
            "schema": rf.schema,
        }
        if rf.description is not None:
            js["description"] = rf.description
        if rf.strict is not None:
            js["strict"] = rf.strict
        return {
            "type": "json_schema",
            "json_schema": js,
        }
    raise ValueError(f"chatcompletions: unknown response format type: {type(rf)}")


def encode_response(
    resp: Response,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR response to a Chat Completions wire response (IR → face)."""
    finish_reason, finish_loss = encode_finish_reason(resp.stop_reason)
    losses: list[Loss] = []
    if finish_loss is not None:
        losses.append(finish_loss)

    msg, msg_losses = encode_assistant_message(resp.content, "content")
    losses.extend(msg_losses)
    if resp.stop_reason == STOP_TOOL_USE and not msg.get("tool_calls"):
        finish_reason, finish_loss = encode_finish_reason(STOP_END_TURN)
        losses.append(loss("stop_reason", "stop_reason", LOSS_DEGRADED, "tool_use was degraded because the target cannot retain custom tool calls"))
    model = resp.model
    if table is not None:
        model = table.map(model)

    usage: dict[str, Any] = {
        "prompt_tokens": resp.usage.input_tokens,
        "completion_tokens": resp.usage.output_tokens,
        "total_tokens": resp.usage.input_tokens + resp.usage.output_tokens,
    }
    if resp.usage.input_tokens_details is not None:
        usage["prompt_tokens_details"] = {
            "cached_tokens": resp.usage.input_tokens_details.cached_tokens
        }
    if resp.usage.output_tokens_details is not None:
        usage["completion_tokens_details"] = {
            "reasoning_tokens": resp.usage.output_tokens_details.reasoning_tokens
        }

    return (
        {
            "id": resp.id,
            "object": OBJECT_CHAT_COMPLETION,
            "created": 0,
            "model": model,
            "choices": [
                {
                    "index": 0,
                    "message": msg,
                    "finish_reason": finish_reason,
                }
            ],
            "usage": usage,
        },
        losses,
    )


def encode_finish_reason(stop: str) -> tuple[str, Loss | None]:
    if stop == STOP_END_TURN:
        return FINISH_REASON_STOP, None
    if stop == STOP_MAX_TOKENS:
        return FINISH_REASON_LENGTH, None
    if stop == STOP_REFUSAL:
        return FINISH_REASON_CONTENT_FILTER, None
    if stop == STOP_TOOL_USE:
        return FINISH_REASON_TOOL_CALLS, None
    if stop == STOP_STOP_SEQUENCE:
        return (
            FINISH_REASON_STOP,
            loss(
                "",
                "stop_sequence",
                LOSS_UNMAPPED_VALUE,
                'Chat Completions finish_reason "stop" does not identify the matched stop sequence',
            ),
        )
    raise ValueError(f"chatcompletions: stop reason {stop!r} has no Chat Completions equivalent")
