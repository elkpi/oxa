"""Chat Completions face → IR conversions (spec/10 §4)."""

from __future__ import annotations

import json
from typing import Any, cast

from oxa._wire import RawDocument, audit_decode
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STREAM_OPTIONS_LOSS_DETAIL,
    NativeAnnotation,
    NativeOmission,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_OTHER,
    STOP_REFUSAL,
    STOP_TOOL_USE,
    CustomToolFormatGrammar,
    CustomToolFormatText,
    InputTokensDetails,
    Loss,
    OutputTokensDetails,
    Params,
    ReasoningEffort,
    Request,
    Response,
    ResponseFormat,
    ResponseFormatJsonObject,
    ResponseFormatJsonSchema,
    ResponseFormatText,
    SystemBlock,
    TextBlock,
    ThinkingBlock,
    Tool,
    Usage,
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


@audit_decode("chatcompletions", "request")
def decode_request(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Request, list[Loss]]:
    """Converts a Chat Completions wire request to the IR (face → IR)."""
    losses: list[Loss] = []

    unmapped_fields = [
        (
            "parallel_tool_calls",
            "Chat Completions parallel tool calls have no IR equivalent in v1.",
        ),
        ("functions", "legacy Chat Completions functions have no IR equivalent in v1."),
        ("function_call", "legacy Chat Completions function_call has no IR equivalent in v1."),
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
            if kind == "custom":
                custom = tool.get("custom", {})
                fmt_dict = custom.get("format")
                format_obj = None
                if fmt_dict is not None and isinstance(fmt_dict, dict):
                    ftype = fmt_dict.get("type")
                    if ftype == "grammar":
                        syntax = fmt_dict.get("syntax")
                        if syntax in ("lark", "regex"):
                            format_obj = CustomToolFormatGrammar(
                                syntax=syntax,
                                definition=fmt_dict.get("definition", ""),
                            )
                        else:
                            losses.append(
                                loss(
                                    f"tools[{index}].custom.format",
                                    "syntax",
                                    LOSS_UNSUPPORTED_SEMANTIC,
                                    f"unknown custom grammar syntax {syntax!r}",
                                )
                            )
                    elif ftype == "text":
                        format_obj = CustomToolFormatText()
                    else:
                        losses.append(
                            loss(
                                f"tools[{index}].custom.format",
                                "type",
                                LOSS_UNSUPPORTED_SEMANTIC,
                                f"unknown custom format {ftype!r}",
                            )
                        )
                tools.append(
                    Tool(
                        name=custom.get("name", ""),
                        description=custom.get("description"),
                        type="custom",
                        format=format_obj,
                    )
                )
                continue
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

    custom_call_ids: set[str] = set()
    for m in raw_messages:
        if m.get("role") == ROLE_ASSISTANT and isinstance(m.get("tool_calls"), list):
            for tc in m["tool_calls"]:
                if isinstance(tc, dict) and tc.get("type") == "custom":
                    custom_call_ids.add(str(tc.get("id", "")))

    index = 0
    while index < len(raw_messages):
        msg = raw_messages[index]
        role = msg.get("role", "")
        if role == ROLE_TOOL:
            merged, next_idx, result_losses = decode_tool_result_run(
                raw_messages, index, custom_call_ids=custom_call_ids
            )
            messages.append(merged)
            losses.extend(result_losses)
            index = next_idx
            continue

        content_path = f"messages[{index}].content"
        content, content_losses = decode_content(msg.get("content"), content_path)
        losses.extend(content_losses)
        if role == ROLE_ASSISTANT and msg.get("reasoning_content") is not None:
            reasoning_content = msg["reasoning_content"]
            if not isinstance(reasoning_content, str):
                raise ValueError(
                    f"chatcompletions: messages[{index}].reasoning_content must be a string"
                )
            content.insert(0, ThinkingBlock(thinking=reasoning_content))

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
                    content = [block for block in content if isinstance(block, ThinkingBlock)]
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

    if wire.get("max_tokens") is not None and wire.get("max_completion_tokens") is not None:
        raise ValueError("chatcompletions: cannot specify both max_tokens and max_completion_tokens")

    native: list[NativeAnnotation] = []
    max_tokens = wire.get("max_tokens")
    if "max_completion_tokens" in wire and wire["max_completion_tokens"] is not None:
        mct = wire["max_completion_tokens"]
        if isinstance(mct, bool) or not isinstance(mct, int) or mct <= 0:
            raise ValueError("chatcompletions: max_completion_tokens must be a positive integer")
        max_tokens = mct
        native.append(
            NativeAnnotation(
                source="openai",
                kind="max_completion_tokens",
                value="true",
            )
        )
    if "store" in wire:
        if wire["store"] is not None:
            store_val = wire["store"]
            if not isinstance(store_val, bool):
                raise ValueError("chatcompletions: store must be a boolean or null")
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="store",
                    value="true" if store_val else "false",
                )
            )
        else:
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="store",
                    value="null",
                )
            )
    if "stream" in wire:
        if wire["stream"] is not None:
            stream_val = wire["stream"]
            if not isinstance(stream_val, bool):
                raise ValueError("chatcompletions: stream must be a boolean or null")
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="stream",
                    value="true" if stream_val else "false",
                )
            )
        else:
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="stream",
                    value="null",
                )
            )
    if "stream_options" in wire:
        if wire["stream_options"] is not None:
            so = wire["stream_options"]
            if not isinstance(so, dict):
                raise ValueError("chatcompletions: stream_options must be an object or null")
            if "include_usage" in so and not isinstance(so["include_usage"], bool):
                raise ValueError("chatcompletions: stream_options.include_usage must be a boolean")
            if "include_obfuscation" in so and not isinstance(so["include_obfuscation"], bool):
                raise ValueError("chatcompletions: stream_options.include_obfuscation must be a boolean")
            clean_so: dict[str, bool] = {}
            if isinstance(so.get("include_obfuscation"), bool):
                clean_so["include_obfuscation"] = so["include_obfuscation"]
            if isinstance(so.get("include_usage"), bool):
                clean_so["include_usage"] = so["include_usage"]
            om = None
            if clean_so.get("include_usage") is True:
                om = NativeOmission(
                    field="stream_options",
                    reason=LOSS_DEGRADED,
                    detail=STREAM_OPTIONS_LOSS_DETAIL,
                )
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="stream_options",
                    value=json.dumps(clean_so, separators=(",", ":")),
                    omission=om,
                )
            )
        else:
            native.append(
                NativeAnnotation(
                    source="openai",
                    kind="stream_options",
                    value="null",
                )
            )

    reasoning_effort = decode_reasoning_effort(wire.get("reasoning_effort"), losses)
    response_format = decode_response_format(wire.get("response_format"), losses)

    params: Params | None = None
    if (
        wire.get("temperature") is not None
        or wire.get("top_p") is not None
        or max_tokens is not None
        or stop_sequences is not None
        or reasoning_effort is not None
        or response_format is not None
    ):
        params = Params(
            temperature=wire.get("temperature"),
            top_p=wire.get("top_p"),
            max_tokens=max_tokens,
            stop_sequences=stop_sequences,
            reasoning_effort=reasoning_effort,
            response_format=response_format,
        )

    return (
        Request(
            model=model,
            messages=messages,
            system=system,
            tools=tools if tools else None,
            tool_choice=tool_choice,
            params=params,
            native=tuple(native) if native else None,
        ),
        losses,
    )


def decode_response_format(value: Any, losses: list[Loss]) -> ResponseFormat | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("chatcompletions: response_format must be an object")
    kind = value.get("type")
    if kind == "text":
        return ResponseFormatText()
    if kind == "json_object":
        return ResponseFormatJsonObject()
    if kind == "json_schema":
        js = value.get("json_schema")
        if not isinstance(js, dict):
            raise ValueError(
                "chatcompletions: response_format of type json_schema requires json_schema object"
            )
        name = js.get("name")
        if not name or not isinstance(name, str):
            raise ValueError("chatcompletions: response_format.json_schema requires string name")
        schema = js.get("schema")
        if not isinstance(schema, dict):
            raise ValueError("chatcompletions: response_format.json_schema requires schema object")
        desc = js.get("description")
        if desc is not None and not isinstance(desc, str):
            raise ValueError(
                "chatcompletions: response_format.json_schema.description must be a string"
            )
        strict = js.get("strict")
        if strict is not None and not isinstance(strict, bool):
            raise ValueError(
                "chatcompletions: response_format.json_schema.strict must be a boolean"
            )
        return ResponseFormatJsonSchema(
            name=name,
            schema=schema,
            description=desc,
            strict=strict,
        )
    losses.append(
        loss(
            "response_format.type",
            "type",
            LOSS_UNMAPPED_VALUE,
            f"unknown response_format type {kind!r}",
        )
    )
    return None


def decode_reasoning_effort(value: Any, losses: list[Loss]) -> ReasoningEffort | None:
    if value is None:
        return None
    if value in ("minimal", "low", "medium", "high", "xhigh", "max"):
        return cast(ReasoningEffort, value)
    losses.append(
        loss(
            "reasoning_effort",
            "reasoning_effort",
            LOSS_UNMAPPED_VALUE,
            f"Chat Completions reasoning_effort {value!r} has no IR equivalent",
        )
    )
    return None


@audit_decode("chatcompletions", "response")
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
    if msg.get("reasoning_content") is not None:
        reasoning_content = msg["reasoning_content"]
        if not isinstance(reasoning_content, str):
            raise ValueError("chatcompletions: reasoning_content must be a string")
        content.insert(0, ThinkingBlock(thinking=reasoning_content))

    tool_calls = msg.get("tool_calls")
    if tool_calls is not None:
        tool_blocks, tool_losses = decode_tool_calls(tool_calls, "choices[0].message.tool_calls")
        if msg.get("content") is None and tool_blocks:
            content = [block for block in content if isinstance(block, ThinkingBlock)]
        content.extend(tool_blocks)
        losses.extend(tool_losses)

    stop_reason, finish_loss = decode_finish_reason(choice.get("finish_reason"))
    if finish_loss is not None:
        losses.append(finish_loss)

    model = str(wire.get("model", ""))
    if table is not None:
        model = table.map(model)

    usage_raw = wire.get("usage", {})
    prompt_details = usage_raw.get("prompt_tokens_details")
    completion_details = usage_raw.get("completion_tokens_details")
    usage = Usage(
        input_tokens=int(usage_raw.get("prompt_tokens", 0)),
        output_tokens=int(usage_raw.get("completion_tokens", 0)),
        input_tokens_details=(
            InputTokensDetails(cached_tokens=int(prompt_details["cached_tokens"]))
            if isinstance(prompt_details, dict) and prompt_details.get("cached_tokens") is not None
            else None
        ),
        output_tokens_details=(
            OutputTokensDetails(reasoning_tokens=int(completion_details["reasoning_tokens"]))
            if isinstance(completion_details, dict)
            and completion_details.get("reasoning_tokens") is not None
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


def decode_request_json(raw_text: str, table: Table | None = None) -> tuple[Request, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_request(document.data, document, table)


def decode_response_json(raw_text: str, table: Table | None = None) -> tuple[Response, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_response(document.data, document, table)
