"""Chat Completions face → IR conversions (spec/10 §4)."""

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
    STOP_OTHER,
    STOP_REFUSAL,
    STOP_TOOL_USE,
    Block,
    Loss,
    Message as IrMessage,
    Params,
    Request,
    Response,
    SystemBlock,
    TextBlock,
    Tool,
    Usage,
)
from oxa.modelmap import Table
from oxa.openai.chatcompletions.constants import (
    FINISH_REASON_CONTENT_FILTER,
    FINISH_REASON_LENGTH,
    FINISH_REASON_STOP,
    FINISH_REASON_TOOL_CALLS,
    ROLE_ASSISTANT,
    ROLE_SYSTEM,
    ROLE_TOOL,
    ROLE_USER,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.chatcompletions.normalize import (
    content_system,
    decode_content,
    decode_tool_calls,
    decode_tool_choice,
    decode_tool_result_run,
    loss,
)


def decode_request(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Request, list[Loss]]:
    """Converts a Chat Completions wire request to the IR (face → IR)."""
    losses: list[Loss] = []

    unmapped_fields = [
        ("parallel_tool_calls", "Chat Completions parallel tool calls have no IR equivalent in v1."),
        ("functions", "legacy Chat Completions functions have no IR equivalent in v1."),
        ("function_call", "legacy Chat Completions function_call has no IR equivalent in v1."),
        ("response_format", "Chat Completions response_format has no IR equivalent in v1."),
        ("logprobs", "Chat Completions log-probability sampling has no IR equivalent in v1."),
        ("top_logprobs", "Chat Completions log-probability sampling has no IR equivalent in v1."),
        ("metadata", "Chat Completions request metadata has no IR equivalent in v1."),
    ]
    for name, detail in unmapped_fields:
        if wire.get(name) is not None:
            losses.append(loss(name, name, LOSS_UNMAPPED_FIELD, detail))

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    tools: list[Tool] | None = None
    if "tools" in wire and wire["tools"] is not None:
        tools = []
        for index, tool in enumerate(wire["tools"]):
            kind = tool.get("type", "")
            if kind != TOOL_TYPE_FUNCTION:
                losses.append(
                    loss(
                        f"tools[{index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Chat Completions tool type {kind!r} has no IR equivalent",
                    )
                )
                continue
            func = tool.get("function", {})
            desc = func.get("description")
            tools.append(
                Tool(
                    name=func.get("name", ""),
                    description=desc if desc else None,
                    input_schema=func.get("parameters", {}),
                )
            )

    tool_choice, tc_losses = decode_tool_choice(wire.get("tool_choice"))
    losses.extend(tc_losses)

    system: list[SystemBlock] = []
    messages: list[IrMessage] = []
    raw_messages = wire.get("messages", [])

    index = 0
    while index < len(raw_messages):
        msg = raw_messages[index]
        role = msg.get("role", "")
        if role == ROLE_TOOL:
            merged, next_idx, result_losses = decode_tool_result_run(raw_messages, index)
            messages.append(merged)
            losses.extend(result_losses)
            index = next_idx
            continue

        content_path = f"messages[{index}].content"
        content, content_losses = decode_content(msg.get("content"), content_path)
        losses.extend(content_losses)

        if role != ROLE_SYSTEM and not content:
            content = [TextBlock(text="")]

        if role == ROLE_SYSTEM:
            sys_blocks, sys_losses = content_system(content, content_path)
            system.extend(sys_blocks)
            losses.extend(sys_losses)
        elif role == ROLE_USER:
            messages.append(IrMessage(role=IR_ROLE_USER, content=content))
        elif role == ROLE_ASSISTANT:
            tool_calls = msg.get("tool_calls")
            if tool_calls is not None:
                calls_path = f"messages[{index}].tool_calls"
                tool_blocks, tool_losses = decode_tool_calls(tool_calls, calls_path)
                if msg.get("content") is None and tool_blocks:
                    content.clear()
                content.extend(tool_blocks)
                losses.extend(tool_losses)
            messages.append(IrMessage(role=IR_ROLE_ASSISTANT, content=content))
        else:
            raise ValueError(f"chatcompletions: messages[{index}]: unknown role {role!r}")

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

    if not messages:
        raise ValueError("chatcompletions: request carries no conversation messages")

    stop = wire.get("stop")
    stop_sequences: list[str] | None = None
    if isinstance(stop, str):
        stop_sequences = [stop] if stop else None
    elif isinstance(stop, list):
        stop_sequences = [s for s in stop if s] if stop else None

    max_tokens = wire.get("max_tokens")
    if max_tokens is None:
        max_tokens = wire.get("max_completion_tokens")

    params: Params | None = None
    if (
        wire.get("temperature") is not None
        or wire.get("top_p") is not None
        or max_tokens is not None
        or stop_sequences is not None
    ):
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
    """Converts a Chat Completions wire response to the IR (face → IR)."""
    choices = wire.get("choices")
    if not choices or not isinstance(choices, list):
        raise ValueError("chatcompletions: choices array is required and must not be empty")

    choice = choices[0]
    msg = choice.get("message", {})
    content, losses = decode_content(msg.get("content"), "choices[0].message.content")

    tool_calls = msg.get("tool_calls")
    if tool_calls is not None:
        tool_blocks, tool_losses = decode_tool_calls(tool_calls, "choices[0].message.tool_calls")
        if msg.get("content") is None and tool_blocks:
            content.clear()
        content.extend(tool_blocks)
        losses.extend(tool_losses)

    stop_reason, finish_loss = decode_finish_reason(choice.get("finish_reason"))
    if finish_loss is not None:
        losses.append(finish_loss)

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    usage_raw = wire.get("usage", {})
    usage = Usage(
        input_tokens=int(usage_raw.get("prompt_tokens", 0)),
        output_tokens=int(usage_raw.get("completion_tokens", 0)),
    )

    return (
        Response(
            id=str(wire.get("id", "")),
            model=model,
            content=content,
            stop_reason=stop_reason,
            usage=usage,
        ),
        losses,
    )


def decode_finish_reason(finish: str | None) -> tuple[str, Loss | None]:
    if not finish:
        raise ValueError("chatcompletions: choices[0].finish_reason is missing")
    if finish == FINISH_REASON_STOP:
        return STOP_END_TURN, None
    if finish == FINISH_REASON_LENGTH:
        return STOP_MAX_TOKENS, None
    if finish == FINISH_REASON_CONTENT_FILTER:
        return STOP_REFUSAL, None
    if finish == FINISH_REASON_TOOL_CALLS:
        return STOP_TOOL_USE, None
    return (
        STOP_OTHER,
        loss(
            "choices[0].finish_reason",
            "finish_reason",
            LOSS_UNMAPPED_VALUE,
            f"Chat Completions finish_reason {finish!r} has no IR equivalent",
        ),
    )
