"""OpenAI Responses face → IR conversions (spec/11 §4)."""

from __future__ import annotations

from dataclasses import replace
from typing import Any, cast

from oxa._metadata import string_metadata
from oxa._verbosity import decode_verbosity
from oxa._wire import RawDocument, audit_decode, raw_field
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_OTHER,
    STOP_TOOL_USE,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    Block,
    CustomToolResultBlock,
    CustomToolUseBlock,
    CustomToolFormatGrammar,
    CustomToolFormatText,
    ImageBlock,
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
    TextBlock,
    TextCitation,
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
from oxa.ir.native import (
    NativeAnnotation,
    NativeOmission,
    NATIVE_SOURCE_OPENAI,
    NATIVE_KIND_CONTAINER,
    NATIVE_KIND_DEFER_LOADING,
    DEFER_LOADING_LOSS_DETAIL,
)
import json
from oxa.modelmap import Table
from oxa.openai.responses.constants import (
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_CUSTOM_TOOL_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING,
    PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT,
    ROLE_DEVELOPER,
    ROLE_SYSTEM,
    ROLE_USER,
    STATUS_COMPLETED,
    STATUS_FAILED,
    STATUS_INCOMPLETE,
    TOOL_TYPE_FUNCTION,
    TOOL_TYPE_CUSTOM,
)
from oxa.openai.responses.normalize import (
    content_system,
    decode_apply_patch_call_status,
    decode_apply_patch_operation,
    decode_apply_patch_result_status,
    decode_assistant_run,
    decode_content,
    decode_image_url,
    decode_output_run,
    decode_tool_choice,
    loss,
    precheck_apply_patch_pairs,
)


@audit_decode("responses", "request")
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

    if wire.get("instructions") is not None:
        req.system.append(TextBlock(text=wire["instructions"]))

    seen_names: set[str] = set()
    tools: list[Tool] = []
    for index, tool in enumerate(wire.get("tools", [])):
        tools.extend(decode_responses_tool_item(tool, [], f"tools[{index}]", seen_names, losses, raw_text))

    tool_choice, tc_losses = decode_tool_choice(wire.get("tool_choice"))
    losses.extend(tc_losses)

    raw_input = wire.get("input")
    if isinstance(raw_input, str):
        req.messages.append(IrMessage(role=IR_ROLE_USER, content=[TextBlock(text=raw_input)]))
    elif isinstance(raw_input, list):
        decoded_calls, decoded_results, dropped, pair_losses = precheck_apply_patch_pairs(raw_input)
        losses.extend(pair_losses)

        index = 0
        while index < len(raw_input):
            item = raw_input[index]
            kind = item.get("type", "")
            if kind in ("", ITEM_TYPE_MESSAGE):
                role = item.get("role", "")
                if role in (ROLE_SYSTEM, ROLE_DEVELOPER):
                    path = f"input[{index}].content"
                    content, c_losses = decode_content(item.get("content"), path)
                    sys_blocks, s_losses = content_system(content, path)
                    if role == ROLE_DEVELOPER:
                        sys_blocks = [replace(block, role="developer") for block in sys_blocks]
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
                    msg, next_idx, run_losses = decode_assistant_run(raw_input, index, decoded_calls, dropped)
                    if msg is not None:
                        req.messages.append(msg)
                    losses.extend(run_losses)
                    index = next_idx
                else:
                    raise ValueError(f"responses: input[{index}]: unknown role {role!r}")
            elif kind in (ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_CUSTOM_TOOL_CALL, "apply_patch_call", ITEM_TYPE_REASONING, "web_search_call"):
                msg, next_idx, run_losses = decode_assistant_run(raw_input, index, decoded_calls, dropped)
                if msg is not None:
                    req.messages.append(msg)
                losses.extend(run_losses)
                index = next_idx
            elif kind in (ITEM_TYPE_FUNCTION_CALL_OUTPUT, ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT, "apply_patch_call_output"):
                msg, next_idx, run_losses = decode_output_run(raw_input, index, decoded_results, dropped)
                if msg is not None:
                    req.messages.append(msg)
                losses.extend(run_losses)
                index = next_idx
            elif kind == "additional_tools":
                item_tools = item.get("tools")
                if not isinstance(item_tools, list) or not item_tools:
                    raise ValueError(f"responses: input[{index}]: additional_tools requires non-empty tools array")
                for t_idx, t_entry in enumerate(item_tools):
                    tools.extend(decode_responses_tool_item(t_entry, [], f"input[{index}].tools[{t_idx}]", seen_names, losses, raw_text))
                index += 1
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
        if not req.system:
            raise ValueError("responses: request carries no conversation input")
        req.messages.append(IrMessage(role=IR_ROLE_USER, content=[TextBlock(text="")]))
        losses.append(
            loss(
                "input",
                "input",
                LOSS_DEGRADED,
                "An empty user message was added because the IR requires "
                "at least one conversation message.",
            )
        )

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
        if raw_effort in ("minimal", "low", "medium", "high", "xhigh", "max"):
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

    verbosity = decode_verbosity(text_config.get("verbosity") if isinstance(text_config, dict) else None, "text.verbosity", losses)
    max_tokens = wire.get("max_output_tokens")
    format_raw = text_config.get("format") if isinstance(text_config, dict) else None
    response_format = decode_text_format(format_raw, losses)
    if isinstance(response_format, ResponseFormatJsonSchema):
        response_format = replace(response_format, schema_raw=raw_field(raw_text, "text.format.schema"))
    params: Params | None = None
    if (
        wire.get("temperature") is not None
        or wire.get("top_p") is not None
        or max_tokens is not None
        or reasoning_effort is not None
        or verbosity is not None
        or response_format is not None
    ):
        params = Params(
            temperature=wire.get("temperature"),
            top_p=wire.get("top_p"),
            max_tokens=max_tokens,
            reasoning_effort=reasoning_effort,
            verbosity=verbosity,
            response_format=response_format,
        )

    return (
        Request(
            model=model,
            messages=req.messages,
            system=req.system,
            tools=tools if tools else None,
            tool_choice=tool_choice,
            params=params,
            metadata=string_metadata(wire.get("metadata"), "responses"),
        ),
        losses,
    )


def decode_custom_tool_format(value: Any, path: str, losses: list[Loss]) -> CustomToolFormat | None:
    if not isinstance(value, dict):
        raise ValueError(f"responses: {path} must be an object")
    kind = value.get("type")
    if kind == "text":
        return CustomToolFormatText()
    if kind == "grammar":
        syntax, definition = value.get("syntax"), value.get("definition")
        if not isinstance(definition, str):
            raise ValueError(f"responses: {path}.definition must be a string")
        if syntax not in ("lark", "regex"):
            losses.append(loss(f"{path}.syntax", "syntax", LOSS_UNSUPPORTED_SEMANTIC, f"unsupported custom grammar syntax {syntax!r}"))
            return None
        return CustomToolFormatGrammar(syntax=syntax, definition=definition)
    losses.append(loss(f"{path}.type", "type", LOSS_UNSUPPORTED_SEMANTIC, f"unsupported custom tool format {kind!r}"))
    return None


def decode_text_format(value: Any, losses: list[Loss]) -> ResponseFormat | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError("responses: text.format must be an object")
    kind = value.get("type")
    if kind == "text":
        return ResponseFormatText()
    if kind == "json_object":
        return ResponseFormatJsonObject()
    if kind == "json_schema":
        name = value.get("name")
        if not name or not isinstance(name, str):
            raise ValueError("responses: text.format of type json_schema requires string name")
        schema = value.get("schema")
        if not isinstance(schema, dict):
            raise ValueError("responses: text.format of type json_schema requires schema object")
        desc = value.get("description")
        if desc is not None and not isinstance(desc, str):
            raise ValueError("responses: text.format.description must be a string")
        strict = value.get("strict")
        if strict is not None and not isinstance(strict, bool):
            raise ValueError("responses: text.format.strict must be a boolean")
        return ResponseFormatJsonSchema(
            name=name,
            schema=schema,
            description=desc,
            strict=strict,
        )
    losses.append(
        loss(
            "text.format.type",
            "type",
            LOSS_UNMAPPED_VALUE,
            f"unknown text.format type {kind!r}",
        )
    )
    return None


@audit_decode("responses", "response")
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
                citations = None
                raw_anns = part.get("annotations")
                if raw_anns:
                    if not isinstance(raw_anns, list):
                        raise TypeError(f"output[{item_index}].content[{content_index}].annotations must be an array")
                    cite_list = []
                    for k, ann in enumerate(raw_anns):
                        if not isinstance(ann, dict):
                            raise TypeError(f"output[{item_index}].content[{content_index}].annotations[{k}] must be an object")
                        ann_type = ann.get("type")
                        if not isinstance(ann_type, str) or not ann_type:
                            raise TypeError(f"output[{item_index}].content[{content_index}].annotations[{k}].type must be a non-empty string")
                        compact = json.dumps(ann, sort_keys=True, separators=(',', ':'))
                        cite_list.append(TextCitation(source="openai", value=compact))
                    if cite_list:
                        citations = tuple(cite_list)
                text.append(TextBlock(text=part.get("text", ""), citations=citations))
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
                    if part.get("type") in (PART_TYPE_OUTPUT_TEXT, "summary_text"):
                        if part.get("annotations"):
                            losses.append(
                                loss(
                                    f"output[{item_index}].summary[{part_index}].annotations",
                                    "annotations",
                                    LOSS_UNMAPPED_FIELD,
                                    "Responses reasoning summary annotations have no IR equivalent",
                                )
                            )
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
        elif kind == "apply_patch_call":
            item_id = item.get("id")
            if not isinstance(item_id, str) or not item_id:
                raise ValueError(f"responses: output[{item_index}].id is required")
            call_id = item.get("call_id")
            if not isinstance(call_id, str) or not call_id:
                raise ValueError(f"responses: output[{item_index}].call_id is required")
            c_status, cs_loss = decode_apply_patch_call_status(item.get("status"), f"output[{item_index}]")
            if cs_loss is not None:
                losses.append(cs_loss)
                continue
            op, op_loss = decode_apply_patch_operation(item.get("operation"), f"output[{item_index}]")
            if op_loss is not None:
                losses.append(op_loss)
                continue
            calls.append(ApplyPatchToolUseBlock(id=call_id, status=c_status, operation=op))
            has_tool_use = True
        elif kind == "apply_patch_call_output":
            item_id = item.get("id")
            if not isinstance(item_id, str) or not item_id:
                raise ValueError(f"responses: output[{item_index}].id is required")
            call_id = item.get("call_id")
            if not isinstance(call_id, str) or not call_id:
                raise ValueError(f"responses: output[{item_index}].call_id is required")
            r_status, rs_loss = decode_apply_patch_result_status(item.get("status"), f"output[{item_index}]")
            if rs_loss is not None:
                losses.append(rs_loss)
                continue
            out = item.get("output")
            if out is not None and not isinstance(out, str):
                raise ValueError(f"responses: output[{item_index}].output must be a string")
            calls.append(ApplyPatchToolResultBlock(tool_use_id=call_id, status=r_status, output=out))
        elif kind == ITEM_TYPE_FUNCTION_CALL:
            raw_name = item.get("name", "")
            ns = item.get("namespace", "")
            final_name = f"{ns}.{raw_name}" if ns and not raw_name.startswith(ns + ".") else raw_name
            calls.append(ToolUseBlock(id=item.get("call_id", ""), name=final_name, input=item.get("arguments", "")))
            has_tool_use = True
        elif kind == ITEM_TYPE_CUSTOM_TOOL_CALL:
            if "input" not in item:
                raise ValueError(f"responses: output[{item_index}].input is missing")
            raw_name = item.get("name", "")
            ns = item.get("namespace", "")
            final_name = f"{ns}.{raw_name}" if ns and not raw_name.startswith(ns + ".") else raw_name
            calls.append(CustomToolUseBlock(id=item.get("call_id", ""), name=final_name, input=item["input"]))
            has_tool_use = True
        elif kind == ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT:
            output = item.get("output")
            if isinstance(output, str):
                calls.append(CustomToolResultBlock(tool_use_id=item.get("call_id", ""), output=output))
            elif isinstance(output, list):
                mapped: list[TextBlock | ImageBlock] = []
                for part_index, part in enumerate(output):
                    path = f"output[{item_index}].output[{part_index}]"
                    if not isinstance(part, dict):
                        losses.append(loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
                        continue
                    if part.get("type") == "input_text":
                        mapped.append(TextBlock(text=part.get("text", "")))
                    elif part.get("type") == "input_image":
                        image, img_loss = decode_image_url(part.get("image_url", ""), f"{path}.image_url")
                        if img_loss is None and isinstance(image, ImageBlock):
                            mapped.append(image)
                        elif img_loss is not None:
                            losses.append(img_loss)
                    else:
                        losses.append(loss(path, "type", LOSS_UNSUPPORTED_SEMANTIC, "custom tool output content is not supported"))
                calls.append(CustomToolResultBlock(tool_use_id=item.get("call_id", ""), output=mapped))
            else:
                raise ValueError(f"responses: output[{item_index}].output must be a string or array")
            has_tool_use = True
        elif kind == "web_search_call":
            from oxa.openai.responses.web_search import decode_web_search_item

            block, call_losses = decode_web_search_item(item, f"output[{item_index}]")
            losses.extend(call_losses)
            calls.append(block)
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
        reason = ""
        inc = wire.get("incomplete_details")
        if isinstance(inc, dict):
            reason = inc.get("reason", "")
        if reason == INCOMPLETE_REASON_MAX_OUTPUT_TOKENS:
            return STOP_MAX_TOKENS, []
        return STOP_OTHER, [
            loss(
                "incomplete_details.reason",
                "reason",
                LOSS_UNMAPPED_VALUE,
                f"Responses incomplete_details reason {reason!r} has no IR equivalent",
            )
        ]
    if status == STATUS_FAILED:
        return STOP_OTHER, [
            loss(
                "error",
                "error",
                LOSS_UNSUPPORTED_SEMANTIC,
                "failed Responses response carries no error object",
            )
        ]
    return STOP_OTHER, [
        loss(
            "status",
            "status",
            LOSS_UNMAPPED_VALUE,
            f"Responses status {status!r} has no IR equivalent",
        )
    ]


def is_known_openai_native_type(kind: str) -> bool:
    if kind in (
        "web_search",
        "file_search",
        "code_interpreter",
        "computer",
        "computer_use_preview",
        "mcp",
        "tool_search",
        "image_generation",
        "local_shell",
        "shell",
        "programmatic_tool_calling",
    ):
        return True
    if kind.startswith("web_search_") or kind.startswith("file_search_") or kind.startswith("computer_"):
        return True
    return False


def compact_json(raw: str) -> str:
    out: list[str] = []
    in_string = False
    escape = False
    for ch in raw:
        if in_string:
            out.append(ch)
            if escape:
                escape = False
            elif ch == "\\":
                escape = True
            elif ch == '"':
                in_string = False
        else:
            if ch == '"':
                in_string = True
                out.append(ch)
            elif ch not in (" ", "\t", "\n", "\r"):
                out.append(ch)
    return "".join(out)


def decode_responses_tool_item(
    tool: dict[str, Any],
    path_stack: list[dict[str, Any]],
    path: str,
    seen_names: set[str],
    losses: list[Loss],
    raw_text: str = "",
) -> list[Tool]:
    kind = tool.get("type", "")

    if kind in ("namespace", "toolbox"):
        name = tool.get("name")
        if not isinstance(name, str) or not name or name.startswith(".") or name.endswith(".") or ".." in name:
            raise ValueError(f"responses: {path}: invalid {kind} name {name!r}")
        child_tools = tool.get("tools")
        if not isinstance(child_tools, list) or not child_tools:
            raise ValueError(f"responses: {path}: {kind} requires non-empty tools array")
        desc = tool.get("description")
        entry: dict[str, Any] = {"type": kind, "name": name}
        if desc is not None:
            entry["description"] = desc
        new_stack = path_stack + [entry]
        res: list[Tool] = []
        for idx, child in enumerate(child_tools):
            res.extend(decode_responses_tool_item(child, new_stack, f"{path}.tools[{idx}]", seen_names, losses, raw_text))
        return res

    prefix = ".".join(e["name"] for e in path_stack)

    if kind == "apply_patch":
        return [Tool(type="apply_patch")]

    if kind == TOOL_TYPE_CUSTOM:
        raw_name = tool.get("name", "")
        if not raw_name:
            raise ValueError(f"responses: {path}: custom tool name is required")
        final_name = f"{prefix}.{raw_name}" if prefix and not raw_name.startswith(prefix + ".") else raw_name
        if final_name in seen_names:
            raise ValueError(f"responses: duplicate tool name {final_name!r}")
        seen_names.add(final_name)

        fmt = decode_custom_tool_format(tool.get("format"), f"{path}.format", losses) if "format" in tool else None
        if "format" in tool and fmt is None:
            return []

        native_ann: list[NativeAnnotation] = []
        if path_stack:
            native_ann.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_OPENAI,
                    kind=NATIVE_KIND_CONTAINER,
                    value=json.dumps({"path": path_stack}, separators=(",", ":")),
                    omission=NativeOmission(
                        field="type",
                        reason="degraded",
                        detail="tool container grouping was flattened into dotted tool names",
                    ),
                )
            )
        if "defer_loading" in tool:
            if not isinstance(tool["defer_loading"], bool):
                raise ValueError(f"responses: {path}.defer_loading must be a boolean")
            if tool["defer_loading"] is True:
                native_ann.append(
                    NativeAnnotation(
                        source=NATIVE_SOURCE_OPENAI,
                        kind=NATIVE_KIND_DEFER_LOADING,
                        value="true",
                        omission=NativeOmission(
                            field="defer_loading",
                            reason="degraded",
                            detail=DEFER_LOADING_LOSS_DETAIL,
                        ),
                    )
                )

        return [
            Tool(
                name=final_name,
                description=tool.get("description"),
                type="custom",
                format=fmt,
                native=tuple(native_ann) if native_ann else (),
            )
        ]

    if kind in (TOOL_TYPE_FUNCTION, ""):
        raw_name = tool.get("name", "")
        if not raw_name:
            raise ValueError(f"responses: {path}: function tool name is required")
        final_name = f"{prefix}.{raw_name}" if prefix and not raw_name.startswith(prefix + ".") else raw_name
        if final_name in seen_names:
            raise ValueError(f"responses: duplicate tool name {final_name!r}")
        seen_names.add(final_name)

        native_ann = []
        if path_stack:
            native_ann.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_OPENAI,
                    kind=NATIVE_KIND_CONTAINER,
                    value=json.dumps({"path": path_stack}, separators=(",", ":")),
                    omission=NativeOmission(
                        field="type",
                        reason="degraded",
                        detail="tool container grouping was flattened into dotted tool names",
                    ),
                )
            )
        if "defer_loading" in tool:
            if not isinstance(tool["defer_loading"], bool):
                raise ValueError(f"responses: {path}.defer_loading must be a boolean")
            if tool["defer_loading"] is True:
                native_ann.append(
                    NativeAnnotation(
                        source=NATIVE_SOURCE_OPENAI,
                        kind=NATIVE_KIND_DEFER_LOADING,
                        value="true",
                        omission=NativeOmission(
                            field="defer_loading",
                            reason="degraded",
                            detail=DEFER_LOADING_LOSS_DETAIL,
                        ),
                    )
                )

        desc = tool.get("description")
        return [
            Tool(
                name=final_name,
                description=desc if desc else None,
                input_schema=tool.get("parameters", {}),
                strict=tool.get("strict"),
                native=tuple(native_ann) if native_ann else (),
            )
        ]

    # Native tool declaration
    has_name = bool(tool.get("name"))
    has_server_label = bool(tool.get("server_label"))
    is_native = has_name or has_server_label or is_known_openai_native_type(kind)
    if not is_native:
        losses.append(
            loss(
                path,
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Responses tool type {kind!r} has no IR equivalent",
            )
        )
        return []

    tool_name = tool.get("name") or tool.get("server_label") or kind
    if tool_name in seen_names:
        raise ValueError(f"responses: duplicate tool name {tool_name!r}")
    seen_names.add(tool_name)

    raw_slice = raw_field(raw_text, path) if raw_text else None
    if raw_slice:
        definition = compact_json(raw_slice)
    else:
        definition = json.dumps(tool, separators=(",", ":"))

    return [
        Tool(
            type="native",
            name=tool_name,
            source="openai",
            definition=definition,
        )
    ]


def decode_request_json(raw_text: str, table: Table | None = None) -> tuple[Request, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_request(document.data, document, table)


def decode_response_json(raw_text: str, table: Table | None = None) -> tuple[Response, list[Loss]]:
    document = RawDocument(raw_text)
    return decode_response(document.data, document, table)
