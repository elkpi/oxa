"""OpenAI Responses IR → face conversions (spec/11 §4)."""

from __future__ import annotations

from typing import Any

from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
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
from oxa.openai.responses.constants import (
    ERROR_CODE_REFUSAL,
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_MESSAGE,
    OBJECT_RESPONSE,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_USER,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_INCOMPLETE,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.responses.normalize import (
    encode_assistant_message,
    encode_tool_choice,
    encode_user_message,
    loss,
)


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

    if req.tools:
        tools_wire: list[dict[str, Any]] = []
        for tool in req.tools:
            t_dict: dict[str, Any] = {
                "type": TOOL_TYPE_FUNCTION,
                "name": tool.name,
                "parameters": tool.input_schema,
            }
            if tool.description:
                t_dict["description"] = tool.description
            tools_wire.append(t_dict)
        out["tools"] = tools_wire

    choice_wire, choice_loss = encode_tool_choice(req.tool_choice)
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
        if req.params.stop_sequences:
            losses.append(
                loss(
                    "params.stop_sequences",
                    "stop_sequences",
                    LOSS_UNMAPPED_FIELD,
                    "Responses requests have no stop-sequences parameter; the IR stop sequences are dropped.",
                )
            )

    return out, losses


def encode_response(
    resp: Response,
    table: Table | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Converts an IR response to a Responses wire response (IR → face)."""
    losses: list[Loss] = []
    text_chunks: list[str] = []
    has_text = False
    calls: list[dict[str, Any]] = []

    for index, block in enumerate(resp.content):
        if isinstance(block, TextBlock):
            text_chunks.append(block.text)
            has_text = True
        elif isinstance(block, ToolUseBlock):
            calls.append(
                {
                    "type": ITEM_TYPE_FUNCTION_CALL,
                    "id": "fc_abc123",
                    "status": STATUS_COMPLETED,
                    "call_id": block.id,
                    "name": block.name,
                    "arguments": block.input,
                }
            )
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

    out: dict[str, Any] = {
        "id": resp.id,
        "object": OBJECT_RESPONSE,
        "status": status,
        "model": model,
        "output": output,
        "usage": {
            "input_tokens": resp.usage.input_tokens,
            "output_tokens": resp.usage.output_tokens,
            "total_tokens": resp.usage.input_tokens + resp.usage.output_tokens,
        },
    }
    if incomplete_details is not None:
        out["incomplete_details"] = incomplete_details
    if error is not None:
        out["error"] = error

    return out, losses
