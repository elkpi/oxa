"""Request-side positional tool changes and compaction replay."""

from dataclasses import replace
from typing import Any

from oxa.ir.loss import Loss

from oxa.ir.types import CompactionBlock, ToolChange, ToolChangeRef
from oxa.ir.native import NativeAnnotation, NativeOmission
from oxa.anthropic.messages.native import parse_cache_control, get_block_cache_control, CACHE_CONTROL_LOSS_DETAIL


def decode_change(block: dict[str, Any], path: str, losses: list[Loss]) -> ToolChange:
    kind = block.get("type")
    if kind not in ("tool_addition", "tool_removal"):
        raise ValueError(f"anthropic: {path}: invalid change type")
    target = block.get("tool")
    if not isinstance(target, dict): raise ValueError(f"anthropic: {path}.tool must be an object")
    ref_type = target.get("type")
    native = ()
    if "cache_control" in block:
        canon, loss = parse_cache_control(block["cache_control"], f"{path}.cache_control")
        if loss is not None or canon is None: raise ValueError(f"anthropic: {path}.cache_control is invalid")
        native = (NativeAnnotation(source="anthropic", kind="cache_control", value=canon, omission=NativeOmission(field="cache_control", reason="unmapped-field", detail=CACHE_CONTROL_LOSS_DETAIL)),)
    extra = {k: v for k, v in block.items() if k not in {"type", "tool", "cache_control"}} or None
    op = "add" if kind == "tool_addition" else "remove"
    if ref_type == "tool_definition":
        if op != "add" or not isinstance(target.get("definition"), dict): raise ValueError(f"anthropic: {path}.tool.definition is invalid")
        from oxa.anthropic.messages.decode import decode_request
        definition = target["definition"]
        req, tool_losses = decode_request({"model": "m", "max_tokens": 1, "messages": [{"role": "user", "content": "x"}], "tools": [definition]})
        known = {"type", "name", "description", "input_schema", "strict", "cache_control", "defer_loading", "eager_input_streaming"}
        losses.extend(
            replace(item, path=f"{path}.tool.definition" + item.path.removeprefix("tools[0]"))
            for item in tool_losses
            if not (item.field in definition and item.field not in known and item.path == f"tools[0].{item.field}")
        )
        return ToolChange(op=op, tool=req.tools[0], native=native, extra=extra,
                          tool_extra={k: v for k, v in target.items() if k not in {"type", "definition"}} or None,
                          definition_extra=({k: v for k, v in definition.items() if k not in known} or None) if req.tools[0].type != "native" else None)
    if ref_type not in ("tool_reference", "mcp_tool_reference", "mcp_toolset_reference"): raise ValueError(f"anthropic: {path}.tool.type is invalid")
    name, server = target.get("name"), target.get("server_name")
    if (ref_type != "mcp_toolset_reference" and (not isinstance(name, str) or not name)) or (ref_type != "tool_reference" and (not isinstance(server, str) or not server)) or (ref_type == "tool_reference" and "server_name" in target) or (ref_type == "mcp_toolset_reference" and "name" in target):
        raise ValueError(f"anthropic: {path}.tool reference is invalid")
    ref = ToolChangeRef(type=ref_type, name=name, server_name=server, extra={k: v for k, v in target.items() if k not in {"type", "name", "server_name"}} or None)
    return ToolChange(op=op, ref=ref, native=native, extra=extra)


def encode_change(change: ToolChange, path: str, losses: list[Loss]) -> dict[str, Any]:
    if change.ref is not None:
        ref = change.ref
        tool = {**(ref.extra or {}), "type": ref.type}
        if ref.name is not None: tool["name"] = ref.name
        if ref.server_name is not None: tool["server_name"] = ref.server_name
    elif change.op == "add" and change.tool is not None:
        from oxa.anthropic.messages.encode import encode_request
        from oxa.ir.types import Request, Message, Params, TextBlock
        wire, tool_losses = encode_request(Request(model="m", params=Params(max_tokens=1), messages=[Message(role="user", content=[TextBlock(text="x")])], tools=[change.tool]))
        losses.extend(replace(item, path=f"{path}.tool" + item.path.removeprefix("tools[0]")) for item in tool_losses)
        if len(wire.get("tools", [])) != 1: raise ValueError("tool definition cannot be represented")
        tool = {**(change.tool_extra or {}), "type": "tool_definition", "definition": {**(change.definition_extra or {}), **wire["tools"][0]}}
    else: raise ValueError("invalid tool change")
    result = {**(change.extra or {}), "type": "tool_addition" if change.op == "add" else "tool_removal", "tool": tool}
    for index, annotation in enumerate(change.native):
        if annotation.source == "anthropic" and annotation.kind == "cache_control":
            continue
        field = "kind" if annotation.source in ("anthropic", "openai") else "source"
        losses.append(Loss(path=f"{path}.native[{index}]", field=field, reason="unsupported-semantic"))
    cache = get_block_cache_control(change.native)
    if cache is not None: result["cache_control"] = cache
    return result


def decode_compaction(wire: dict[str, Any], path: str, losses: list[Loss]) -> CompactionBlock:
    content = wire.get("content")
    if content is not None and (not isinstance(content, str) or not content): raise ValueError(f"anthropic: {path}.content must be a nonempty string or null")
    for key in ("signature", "encrypted_content"):
        if key in wire and not isinstance(wire[key], str): raise ValueError(f"anthropic: {path}.{key} must be a string")
    changes = wire.get("tool_changes")
    if "tool_changes" in wire and not isinstance(changes, list): raise ValueError(f"anthropic: {path}.tool_changes must be an array")
    native = ()
    if "cache_control" in wire:
        canon, loss = parse_cache_control(wire["cache_control"], f"{path}.cache_control")
        if loss is not None or canon is None: raise ValueError(f"anthropic: {path}.cache_control is invalid")
        native = (NativeAnnotation(source="anthropic", kind="cache_control", value=canon, omission=NativeOmission(field="cache_control", reason="unmapped-field", detail=CACHE_CONTROL_LOSS_DETAIL)),)
    return CompactionBlock(content=content, content_present="content" in wire, encrypted_content=wire.get("encrypted_content"), signature=wire.get("signature"), tool_changes=[decode_change(c, f"{path}.tool_changes[{i}]", losses) for i, c in enumerate(changes)] if changes is not None else None, native=native, extra={k: v for k, v in wire.items() if k not in {"type", "content", "signature", "encrypted_content", "tool_changes", "cache_control"}} or None)


def encode_compaction(block: CompactionBlock, path: str, losses: list[Loss]) -> dict[str, Any]:
    result: dict[str, Any] = {**(block.extra or {}), "type": "compaction"}
    if block.content_present or block.content is not None: result["content"] = block.content
    if block.encrypted_content is not None: result["encrypted_content"] = block.encrypted_content
    if block.signature is not None: result["signature"] = block.signature
    if block.tool_changes is not None: result["tool_changes"] = [encode_change(c, f"{path}.tool_changes[{i}]", losses) for i, c in enumerate(block.tool_changes)]
    cache = get_block_cache_control(block.native)
    if cache is not None: result["cache_control"] = cache
    return result
