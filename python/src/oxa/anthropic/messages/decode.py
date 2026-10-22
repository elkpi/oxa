"""Anthropic Messages face → IR conversions (spec/12 §4)."""

from __future__ import annotations

from typing import Any
import json

from oxa._wire import RawDocument, audit_decode, raw_field
from oxa.anthropic.messages.constants import (
    ROLE_ASSISTANT as AN_ROLE_ASSISTANT,
    ROLE_SYSTEM as AN_ROLE_SYSTEM,
    ROLE_USER as AN_ROLE_USER,
)
from oxa.anthropic.messages.constants import (
    STOP_REASON_END_TURN,
    STOP_REASON_MAX_TOKENS,
    STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE,
    STOP_REASON_TOOL_USE,
)
from oxa.anthropic.messages.native import CACHE_CONTROL_LOSS_DETAIL, decode_request_native, parse_cache_control
from oxa.anthropic.messages.normalize import (
    decode_block,
    decode_content,
    decode_system,
    decode_tool_choice,
    loss,
    require_json_object,
)
from oxa.ir import (
    LOSS_DEGRADED,
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
    Params,
    ReasoningEffort,
    Request,
    Response,
    TextBlock,
    Tool,
    Usage,
)
from oxa.ir.native import (
    NATIVE_KIND_CACHE_CONTROL,
    NATIVE_SOURCE_ANTHROPIC,
    NativeAnnotation,
    NativeOmission,
)
from oxa.ir import (
    Message as IrMessage,
)
from oxa.modelmap import Table


def is_custom_tool_input_schema(schema: Any) -> bool:
    if not isinstance(schema, dict) or schema.get("type") != "object":
        return False
    props = schema.get("properties")
    if not isinstance(props, dict) or list(props.keys()) != ["input"]:
        return False
    input_prop = props.get("input")
    return isinstance(input_prop, dict) and input_prop.get("type") == "string"


@audit_decode("anthropic", "request")
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

    if "metadata" in wire:
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
    custom_tools: set[str] = set()
    native_tools: set[str] = set()
    declared_tools: set[str] = set()
    if "tools" in wire and wire["tools"] is not None:
        tools = []
        for index, tool in enumerate(wire["tools"]):
            if not isinstance(tool, dict):
                raise ValueError(f"anthropic: tools[{index}] must be an object")
            name = tool.get("name")
            if name is None:
                raise ValueError(f"anthropic: tools[{index}].name is required")
            if not isinstance(name, str):
                raise ValueError(f"anthropic: tools[{index}].name must be a string")
            if not name:
                raise ValueError(f"anthropic: tools[{index}].name cannot be empty")
            declared_tools.add(name)

            tool_type = None
            if "type" in tool:
                tool_type = tool["type"]
                if not isinstance(tool_type, str):
                    raise ValueError(f"anthropic: tools[{index}].type must be a string")
                if tool_type and tool_type not in ("function", "custom"):
                    native_tools.add(name)
                    definition = raw_field(raw_text, f"tools[{index}]")
                    if not definition:
                        definition = json.dumps(tool, ensure_ascii=False)
                    tools.append(
                        Tool(
                            type="native",
                            name=name,
                            source="anthropic",
                            definition=definition,
                        )
                    )
                    continue

            schema = tool.get("input_schema")
            require_json_object(schema, f"tools[{index}].input_schema")
            desc = tool.get("description")
            native: tuple[NativeAnnotation, ...] = ()
            if "cache_control" in tool:
                canon, l = parse_cache_control(tool["cache_control"], f"tools[{index}].cache_control")
                if l is not None:
                    losses.append(l)
                if canon is not None:
                    native = (
                        NativeAnnotation(
                            source=NATIVE_SOURCE_ANTHROPIC,
                            kind=NATIVE_KIND_CACHE_CONTROL,
                            value=canon,
                            omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD, detail=CACHE_CONTROL_LOSS_DETAIL),
                        ),
                    )
            if is_custom_tool_input_schema(schema):
                custom_tools.add(name)
                tools.append(
                    Tool(
                        name=name,
                        description=desc if desc else None,
                        type="custom",
                        native=native,
                    )
                )
            else:
                tools.append(
                    Tool(
                        name=name,
                        description=desc if desc else None,
                        input_schema=schema,
                        native=native,
                    )
                )

    tool_choice, tc_losses = decode_tool_choice(wire.get("tool_choice"), custom_tools=custom_tools, native_tools=native_tools)
    losses.extend(tc_losses)

    system, sys_losses = decode_system(wire.get("system"))
    losses.extend(sys_losses)

    raw_messages = wire.get("messages", [])
    if not raw_messages:
        raise ValueError("anthropic: request carries no messages")

    for i, msg in enumerate(raw_messages):
        role_str = msg.get("role", "")
        if role_str == AN_ROLE_SYSTEM:
            if i == 0:
                raise ValueError("anthropic: messages[0]: system message cannot appear at messages[0]")
            prev_role = raw_messages[i - 1].get("role", "")
            if prev_role not in (AN_ROLE_USER, AN_ROLE_SYSTEM):
                raise ValueError(f"anthropic: messages[{i}]: system message must follow a user turn")
            last_in_run = i
            while last_in_run + 1 < len(raw_messages) and raw_messages[last_in_run + 1].get("role", "") == AN_ROLE_SYSTEM:
                last_in_run += 1
            if last_in_run + 1 < len(raw_messages):
                next_role = raw_messages[last_in_run + 1].get("role", "")
                if next_role != AN_ROLE_ASSISTANT:
                    raise ValueError(f"anthropic: messages[{i}]: system message must be followed by an assistant turn or be the last message")
        elif role_str not in (AN_ROLE_USER, AN_ROLE_ASSISTANT):
            raise ValueError(f"anthropic: messages[{i}]: unknown role {role_str!r}")

    custom_call_ids: set[str] = set()
    messages: list[IrMessage] = []
    for index, msg in enumerate(raw_messages):
        role_str = msg.get("role", "")
        if role_str == AN_ROLE_SYSTEM:
            content = msg.get("content")
            if content is None:
                raise ValueError(f"anthropic: messages[{index}]: content is missing")
            if isinstance(content, str):
                system.append(TextBlock(text=content))
            elif isinstance(content, list):
                for j, block in enumerate(content):
                    if not isinstance(block, dict) or block.get("type") != "text":
                        raise ValueError(f"anthropic: messages[{index}]: system content must be text")
                    native = ()
                    if "cache_control" in block:
                        canon, l = parse_cache_control(block["cache_control"], f"messages[{index}].content[{j}].cache_control")
                        if l is not None:
                            losses.append(l)
                        if canon is not None:
                            native = (
                                NativeAnnotation(
                                    source=NATIVE_SOURCE_ANTHROPIC,
                                    kind=NATIVE_KIND_CACHE_CONTROL,
                                    value=canon,
                                    omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD, detail=CACHE_CONTROL_LOSS_DETAIL),
                                ),
                            )
                    system.append(TextBlock(text=block.get("text", ""), native=native))
            else:
                raise ValueError(f"anthropic: messages[{index}]: system content must be text")

            losses.append(
                loss(
                    f"messages[{index}].role",
                    "role",
                    LOSS_DEGRADED,
                    "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.",
                )
            )
            continue

        if role_str == AN_ROLE_USER:
            role = ROLE_USER
        elif role_str == AN_ROLE_ASSISTANT:
            role = ROLE_ASSISTANT
        else:
            raise ValueError(f"anthropic: messages[{index}]: unknown role {role_str!r}")

        content_path = f"messages[{index}].content"
        blocks, b_losses = decode_content(
            msg.get("content"), content_path, raw_text, custom_tools, custom_call_ids, declared_tools, is_request=True
        )
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

    req = Request(
        model=model,
        messages=messages,
        system=system,
        tools=tools if tools else None,
        tool_choice=tool_choice,
        params=params,
    )

    req, native_losses = decode_request_native(wire, req)
    losses.extend(native_losses)

    return req, losses


@audit_decode("anthropic", "response")
def decode_response(
    wire: dict[str, Any],
    raw_text: str = "",
    table: Table | None = None,
) -> tuple[Response, list[Loss]]:
    """Converts an Anthropic Messages wire response to the IR (face → IR)."""
    losses: list[Loss] = []

    content_raw = wire.get("content", [])
    if not isinstance(content_raw, list):
        raise ValueError("anthropic: content must be a list of blocks")
    content: list[Block] = []
    for index, block in enumerate(content_raw):
        if not isinstance(block, dict):
            raise ValueError(f"anthropic: content[{index}] must be a block object")
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
        cache_read_input_tokens=(
            int(usage_raw["cache_read_input_tokens"])
            if usage_raw.get("cache_read_input_tokens") is not None
            else None
        ),
        cache_creation_input_tokens=(
            int(usage_raw["cache_creation_input_tokens"])
            if usage_raw.get("cache_creation_input_tokens") is not None
            else None
        ),
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


def decode_request_json(raw_text: str, table: Table | None = None) -> tuple[Request, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_request(document.data, document, table)


def decode_response_json(raw_text: str, table: Table | None = None) -> tuple[Response, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_response(document.data, document, table)
