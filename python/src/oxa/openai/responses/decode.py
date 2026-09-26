"""OpenAI Responses face → IR conversions (spec/11 §4)."""

from __future__ import annotations

from typing import Any, cast

from oxa.ir import (
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_OTHER,
    STOP_TOOL_USE,
    Block,
    InputTokensDetails,
    Loss,
    OutputTokensDetails,
    Params,
    ReasoningEffort,
    Request,
    Response,
    TextBlock,
    ThinkingBlock,
    Tool,
    ToolUseBlock,
    Usage,
)
from oxa.ir import (
    ROLE_USER as IR_ROLE_USER,
)
from oxa.ir import (
    Message as IrMessage,
)
from oxa.modelmap import Table
from oxa.openai.responses.constants import (
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_SYSTEM,
    ROLE_USER,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_INCOMPLETE,
    TOOL_TYPE_FUNCTION,
)
from oxa.openai.responses.normalize import (
    content_system,
    decode_assistant_run,
    decode_content,
    decode_output_run,
    decode_tool_choice,
    loss,
)


def decode_request(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Request, list[Loss]]:
    """Converts a Responses wire request to the IR (face → IR)."""
    losses: list[Loss] = []

    text_config = wire.get("text")
    unmapped_fields = [
        (
            "metadata",
            "metadata",
            wire.get("metadata") is not None,
            "Responses request metadata has no IR equivalent in v1.",
        ),
        (
            "text.verbosity",
            "verbosity",
            isinstance(text_config, dict) and text_config.get("verbosity") is not None,
            "Responses output verbosity has no IR equivalent in v1.",
        ),
        (
            "text.format",
            "format",
            isinstance(text_config, dict) and text_config.get("format") is not None,
            "Responses text output format has no IR equivalent in v1.",
        ),
        (
            "parallel_tool_calls",
            "parallel_tool_calls",
            wire.get("parallel_tool_calls") is not None,
            "Responses parallel tool calls have no IR equivalent in v1.",
        ),
    ]
    for path, field, present, detail in unmapped_fields:
        if present:
            losses.append(loss(path, field, LOSS_UNMAPPED_FIELD, detail))

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    req = Request(
        model=model,
        messages=[],
        system=[],
    )

    if wire.get("instructions"):
        req.system.append(TextBlock(text=wire["instructions"]))

    tools: list[Tool] = []
    for index, tool in enumerate(wire.get("tools", [])):
        kind = tool.get("type", "")
        if kind != TOOL_TYPE_FUNCTION:
            losses.append(
                loss(
                    f"tools[{index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Responses tool type {kind!r} has no IR equivalent",
                )
            )
            continue
        desc = tool.get("description")
        tools.append(
            Tool(
                name=tool.get("name", ""),
                description=desc if desc else None,
                input_schema=tool.get("parameters", {}),
            )
        )
        if tool.get("strict") is not None:
            losses.append(
                loss(
                    f"tools[{index}].strict",
                    "strict",
                    LOSS_UNMAPPED_FIELD,
                    "Responses function tool strict mode has no IR equivalent in v1.",
                )
            )

    tool_choice, tc_losses = decode_tool_choice(wire.get("tool_choice"))
    losses.extend(tc_losses)

    raw_input = wire.get("input")
    if isinstance(raw_input, str):
        req.messages.append(IrMessage(role=IR_ROLE_USER, content=[TextBlock(text=raw_input)]))
    elif isinstance(raw_input, list):
        index = 0
        while index < len(raw_input):
            item = raw_input[index]
            kind = item.get("type", "")
            if kind in ("", ITEM_TYPE_MESSAGE):
                role = item.get("role", "")
                if role == ROLE_SYSTEM:
                    path = f"input[{index}].content"
                    content, c_losses = decode_content(item.get("content"), path)
                    sys_blocks, s_losses = content_system(content, path)
                    req.system.extend(sys_blocks)
                    losses.extend(c_losses)
                    losses.extend(s_losses)
                    index += 1
                elif role == ROLE_USER:
                    path = f"input[{index}].content"
                    content, c_losses = decode_content(item.get("content"), path)
                    losses.extend(c_losses)
                    if not content:
                        content = [TextBlock(text="")]
                    req.messages.append(IrMessage(role=IR_ROLE_USER, content=content))
                    index += 1
                elif role == ROLE_ASSISTANT:
                    msg, next_idx, run_losses = decode_assistant_run(raw_input, index)
                    if msg is not None:
                        req.messages.append(msg)
                    losses.extend(run_losses)
                    index = next_idx
                else:
                    raise ValueError(f"responses: input[{index}]: unknown role {role!r}")
            elif kind in (ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_REASONING):
                msg, next_idx, run_losses = decode_assistant_run(raw_input, index)
                if msg is not None:
                    req.messages.append(msg)
                losses.extend(run_losses)
                index = next_idx
            elif kind == ITEM_TYPE_FUNCTION_CALL_OUTPUT:
                msg, next_idx = decode_output_run(raw_input, index)
                req.messages.append(msg)
                index = next_idx
            else:
                losses.append(
                    loss(
                        f"input[{index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        f"Responses input item type {kind!r} has no IR equivalent",
                    )
                )
                index += 1

    if not req.messages:
        raise ValueError("responses: request carries no conversation input")

    reasoning_effort: ReasoningEffort | None = None
    reasoning = wire.get("reasoning")
    raw_effort: str | None = None
    if isinstance(reasoning, str):
        raw_effort = reasoning
    elif isinstance(reasoning, dict):
        if "summary" in reasoning:
            losses.append(
                loss(
                    "reasoning.summary",
                    "summary",
                    LOSS_UNMAPPED_FIELD,
                    "Responses reasoning summary preference has no IR equivalent",
                )
            )
        for key in reasoning.keys() - {"effort", "summary"}:
            losses.append(
                loss(
                    f"reasoning.{key}",
                    key,
                    LOSS_UNMAPPED_FIELD,
                    f"Responses reasoning property {key!r} has no IR equivalent",
                )
            )
        effort_value = reasoning.get("effort")
        if effort_value is not None and not isinstance(effort_value, str):
            raise ValueError("responses: reasoning.effort must be a string")
        raw_effort = effort_value
    elif reasoning is not None:
        raise ValueError("responses: reasoning must be an object or string")

    if raw_effort is not None:
        if raw_effort in ("minimal", "low", "medium", "high"):
            reasoning_effort = cast(ReasoningEffort, raw_effort)
        else:
            losses.append(
                loss(
                    "reasoning.effort",
                    "effort",
                    LOSS_UNMAPPED_VALUE,
                    f"unknown reasoning effort value {raw_effort!r}",
                )
            )

    max_tokens = wire.get("max_output_tokens")
    params: Params | None = None
    if (
        wire.get("temperature") is not None
        or wire.get("top_p") is not None
        or max_tokens is not None
        or reasoning_effort is not None
    ):
        params = Params(
            temperature=wire.get("temperature"),
            top_p=wire.get("top_p"),
            max_tokens=max_tokens,
            reasoning_effort=reasoning_effort,
        )

    return (
        Request(
            model=model,
            messages=req.messages,
            system=req.system,
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
    """Converts a Responses wire response to the IR (face → IR)."""
    losses: list[Loss] = []
    thinkings: list[Block] = []
    text: list[Block] = []
    calls: list[Block] = []
    has_tool_use = False

    for item_index, item in enumerate(wire.get("output", [])):
        kind = item.get("type", "")
        if kind == ITEM_TYPE_MESSAGE:
            for content_index, part in enumerate(item.get("content", [])):
                p_kind = part.get("type", "")
                if p_kind != PART_TYPE_OUTPUT_TEXT:
                    losses.append(
                        loss(
                            f"output[{item_index}].content[{content_index}]",
                            "type",
                            LOSS_UNSUPPORTED_SEMANTIC,
                            f"Responses output content type {p_kind!r} has no IR equivalent",
                        )
                    )
                    continue
                if part.get("annotations"):
                    losses.append(
                        loss(
                            f"output[{item_index}].content[{content_index}].annotations",
                            "annotations",
                            LOSS_UNMAPPED_FIELD,
                            "Responses output annotations have no IR equivalent in v1.",
                        )
                    )
                text.append(TextBlock(text=part.get("text", "")))
        elif kind == ITEM_TYPE_REASONING:
            summary = item.get("summary", [])
            if not summary:
                losses.append(
                    loss(
                        f"output[{item_index}]",
                        "type",
                        LOSS_UNSUPPORTED_SEMANTIC,
                        "Responses reasoning output item with empty summary carries no convertible content",
                    )
                )
            else:
                for part_index, part in enumerate(summary):
                    if part.get("type") == PART_TYPE_OUTPUT_TEXT:
                        thinkings.append(ThinkingBlock(thinking=part.get("text", "")))
                    else:
                        part_type = part.get("type", "")
                        losses.append(
                            loss(
                                f"output[{item_index}].summary[{part_index}]",
                                "type",
                                LOSS_UNSUPPORTED_SEMANTIC,
                                f"Responses reasoning summary type {part_type!r} has no IR equivalent",
                            )
                        )
            if item.get("encrypted_content"):
                losses.append(
                    loss(
                        f"output[{item_index}].encrypted_content",
                        "encrypted_content",
                        LOSS_UNMAPPED_FIELD,
                        "Responses reasoning encrypted_content has no IR equivalent in v2.",
                    )
                )
        elif kind == ITEM_TYPE_FUNCTION_CALL:
            calls.append(
                ToolUseBlock(
                    id=item.get("call_id", ""),
                    name=item.get("name", ""),
                    input=item.get("arguments", ""),
                )
            )
            has_tool_use = True
        else:
            losses.append(
                loss(
                    f"output[{item_index}]",
                    "type",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"Responses output item type {kind!r} has no IR equivalent",
                )
            )

    stop_reason, status_losses = decode_status(wire, has_tool_use)
    losses.extend(status_losses)
    content = thinkings + text + calls

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    usage_raw = wire.get("usage", {})
    input_details = usage_raw.get("input_token_details")
    output_details = usage_raw.get("output_token_details")
    usage = Usage(
        input_tokens=int(usage_raw.get("input_tokens", 0)),
        output_tokens=int(usage_raw.get("output_tokens", 0)),
        input_tokens_details=(
            InputTokensDetails(cached_tokens=int(input_details["cached_tokens"]))
            if isinstance(input_details, dict) and input_details.get("cached_tokens") is not None
            else None
        ),
        output_tokens_details=(
            OutputTokensDetails(reasoning_tokens=int(output_details["reasoning_tokens"]))
            if isinstance(output_details, dict)
            and output_details.get("reasoning_tokens") is not None
            else None
        ),
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


def decode_status(wire: dict[str, Any], has_tool_use: bool) -> tuple[str, list[Loss]]:
    if wire.get("error"):
        error = wire["error"]
        code = error.get("code")
        msg = error.get("message", "")
        return (
            STOP_OTHER,
            [
                loss(
                    "error",
                    "error",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    f"failed Responses response carries error {code!r}: {msg}",
                )
            ],
        )

    status = wire.get("status", "")
    if status == STATUS_COMPLETED:
        return (STOP_TOOL_USE if has_tool_use else STOP_END_TURN), []
    if status == STATUS_INCOMPLETE:
        details = wire.get("incomplete_details", {})
        reason = details.get("reason", "")
        if reason == INCOMPLETE_REASON_MAX_OUTPUT_TOKENS:
            return STOP_MAX_TOKENS, []
        return (
            STOP_OTHER,
            [
                loss(
                    "incomplete_details.reason",
                    "reason",
                    LOSS_UNMAPPED_VALUE,
                    f"Responses incomplete_details reason {reason!r} has no IR equivalent",
                )
            ],
        )
    if status == STATUS_FAILED:
        return (
            STOP_OTHER,
            [
                loss(
                    "error",
                    "error",
                    LOSS_UNSUPPORTED_SEMANTIC,
                    "failed Responses response carries no error object",
                )
            ],
        )
    return STOP_OTHER, []
