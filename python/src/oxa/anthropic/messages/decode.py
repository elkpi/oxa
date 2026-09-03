"""Anthropic Messages face → IR conversions (spec/12 §4)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    ROLE_ASSISTANT as AN_ROLE_ASSISTANT,
    ROLE_USER as AN_ROLE_USER,
    STOP_REASON_END_TURN,
    STOP_REASON_MAX_TOKENS,
    STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE,
    STOP_REASON_TOOL_USE,
)
from oxa.anthropic.messages.normalize import (
    decode_block,
    decode_content,
    decode_system,
    decode_tool_choice,
    loss,
    require_json_object,
)
from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    ROLE_ASSISTANT,
    ROLE_USER,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_OTHER,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    Block,
    Loss,
    Message as IrMessage,
    Params,
    Request,
    Response,
    Tool,
    Usage,
)
from oxa.modelmap import Table


def decode_request(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Request, list[Loss]]:
    """Converts an Anthropic Messages wire request to the IR (face → IR)."""
    losses: list[Loss] = []

    max_tokens = wire.get("max_tokens")
    if max_tokens is None or max_tokens <= 0:
        raise ValueError("anthropic: max_tokens is required and must be positive")

    if wire.get("metadata") is not None:
        losses.append(
            loss(
                "metadata",
                "metadata",
                LOSS_UNMAPPED_FIELD,
                "Anthropic request metadata (user_id) has no IR equivalent in v1.",
            )
        )

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    tools: list[Tool] | None = None
    if "tools" in wire and wire["tools"] is not None:
        tools = []
        for index, tool in enumerate(wire["tools"]):
            schema = tool.get("input_schema")
            require_json_object(schema, f"tools[{index}].input_schema")
            desc = tool.get("description")
            tools.append(
                Tool(
                    name=tool["name"],
                    description=desc if desc else None,
                    input_schema=schema,
                )
            )

    tool_choice, tc_losses = decode_tool_choice(wire.get("tool_choice"))
    losses.extend(tc_losses)

    system, sys_losses = decode_system(wire.get("system"))
    losses.extend(sys_losses)

    raw_messages = wire.get("messages", [])
    if not raw_messages:
        raise ValueError("anthropic: request carries no messages")

    messages: list[IrMessage] = []
    for index, msg in enumerate(raw_messages):
        role_str = msg.get("role", "")
        if role_str == AN_ROLE_USER:
            role = ROLE_USER
        elif role_str == AN_ROLE_ASSISTANT:
            role = ROLE_ASSISTANT
        else:
            raise ValueError(f"anthropic: messages[{index}]: unknown role {role_str!r}")

        content_path = f"messages[{index}].content"
        blocks, b_losses = decode_content(msg.get("content"), content_path)
        losses.extend(b_losses)
        messages.append(IrMessage(role=role, content=blocks))

    stop = wire.get("stop_sequences")
    stop_sequences = [s for s in stop if s] if isinstance(stop, list) and stop else None

    params = Params(
        temperature=wire.get("temperature"),
        top_p=wire.get("top_p"),
        max_tokens=max_tokens,
        stop_sequences=stop_sequences,
    )

    return (
        Request(
            model=model,
            messages=messages,
            system=system,
            tools=tools if tools else None,
            tool_choice=tool_choice,
            params=params,
        ),
        losses,
    )


def decode_response(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Response, list[Loss]]:
    """Converts an Anthropic Messages wire response to the IR (face → IR)."""
    losses: list[Loss] = []

    content_raw = wire.get("content", [])
    content: list[Block] = []
    for index, block in enumerate(content_raw):
        decoded, b_losses, mapped = decode_block(block, f"content[{index}]", raw_text)
        if mapped:
            content.extend(decoded)
        losses.extend(b_losses)

    stop_reason, stop_loss = decode_stop_reason(wire.get("stop_reason"))
    if stop_loss is not None:
        losses.append(stop_loss)

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    usage_raw = wire.get("usage", {})
    usage = Usage(
        input_tokens=int(usage_raw.get("input_tokens", 0)),
        output_tokens=int(usage_raw.get("output_tokens", 0)),
    )

    stop_seq = wire.get("stop_sequence")

    return (
        Response(
            id=str(wire.get("id", "")),
            model=model,
            content=content,
            stop_reason=stop_reason,
            usage=usage,
            stop_sequence=stop_seq if stop_seq else None,
        ),
        losses,
    )


def decode_stop_reason(stop: Any) -> tuple[str, Loss | None]:
    if stop == STOP_REASON_END_TURN:
        return STOP_END_TURN, None
    if stop == STOP_REASON_MAX_TOKENS:
        return STOP_MAX_TOKENS, None
    if stop == STOP_REASON_STOP_SEQUENCE:
        return STOP_STOP_SEQUENCE, None
    if stop == STOP_REASON_TOOL_USE:
        return STOP_TOOL_USE, None
    if stop == STOP_REASON_REFUSAL:
        return STOP_REFUSAL, None
    if not stop:
        raise ValueError("anthropic: stop_reason is missing")
    return (
        STOP_OTHER,
        loss(
            "stop_reason",
            "stop_reason",
            LOSS_UNMAPPED_VALUE,
            f"Anthropic stop_reason {stop!r} has no IR equivalent",
        ),
    )
