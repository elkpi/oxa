"""Chat Completions IR → face conversions (spec/10 §4)."""

from __future__ import annotations

from typing import Any

from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    ROLE_ASSISTANT as IR_ROLE_ASSISTANT,
    ROLE_USER as IR_ROLE_USER,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    Block,
    Loss,
    Request,
    Response,
    ToolResultBlock,
)
from oxa.modelmap import Table
from oxa.openai.chatcompletions.constants import (
    FINISH_REASON_CONTENT_FILTER,
    FINISH_REASON_LENGTH,
    FINISH_REASON_STOP,
    FINISH_REASON_TOOL_CALLS,
    OBJECT_CHAT_COMPLETION,
    ROLE_SYSTEM,
    ROLE_USER,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.chatcompletions.normalize import (
    encode_assistant_message,
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

    if req.metadata:
        losses.append(
            loss(
                "metadata",
                "metadata",
                LOSS_UNMAPPED_FIELD,
                "Chat Completions requests have no metadata field; the IR metadata map is dropped.",
            )
        )

    model = req.model
    if table is not None:
        model = table.map(model)

    out: dict[str, Any] = {
        "model": model,
        "messages": [],
    }

    if req.tools:
        tools_wire: list[dict[str, Any]] = []
        for tool in req.tools:
            func: dict[str, Any] = {
                "name": tool.name,
            }
            if tool.description:
                func["description"] = tool.description
            if tool.input_schema is not None:
                func["parameters"] = tool.input_schema
            tools_wire.append(
                {
                    "type": TOOL_TYPE_FUNCTION,
                    "function": func,
                }
            )
        out["tools"] = tools_wire

    choice_wire, choice_loss = encode_tool_choice(req.tool_choice)
    if choice_wire is not None:
        out["tool_choice"] = choice_wire
    if choice_loss is not None:
        losses.append(choice_loss)

    if req.system:
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
                        f"messages[{index}]",
                        "ordering",
                        LOSS_DEGRADED,
                        "N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved",
                    )
                )

            for pos, res in enumerate(results):
                tool_msg, tool_losses = encode_tool_result(
                    res, f"messages[{index}].content[{pos}]"
                )
                out["messages"].append(tool_msg)
                losses.extend(tool_losses)

            if normal or not results:
                content, content_losses = encode_user_content(
                    normal, f"messages[{index}].content"
                )
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

    return out, losses


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

    model = resp.model
    if table is not None:
        model = table.map(model)

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
            "usage": {
                "prompt_tokens": resp.usage.input_tokens,
                "completion_tokens": resp.usage.output_tokens,
                "total_tokens": resp.usage.input_tokens + resp.usage.output_tokens,
            },
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
