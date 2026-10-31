"""Canonical JSON codec for Intermediate Representation (IR) documents (spec/01)."""

from __future__ import annotations

import json
from typing import Any

from oxa.ir.constants import (
    BLOCK_TYPE_CUSTOM_TOOL_RESULT,
    BLOCK_TYPE_CUSTOM_TOOL_USE,
    BLOCK_TYPE_APPLY_PATCH_TOOL_USE,
    BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT,
    BLOCK_TYPE_WEB_SEARCH_CALL,
    BLOCK_TYPE_DOCUMENT,
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING,
    BLOCK_TYPE_REDACTED_THINKING,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA,
    DELTA_TYPE_INPUT_JSON_DELTA,
    DELTA_TYPE_SIGNATURE_DELTA,
    DELTA_TYPE_TEXT_DELTA,
    DELTA_TYPE_THINKING_DELTA,
    DELTA_TYPE_CITATION,
    EVENT_TYPE_CONTENT_BLOCK_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA,
    EVENT_TYPE_MESSAGE_DONE,
    EVENT_TYPE_MESSAGE_START,
    SPEC_VERSION,
    STOP_STOP_SEQUENCE,
    SUPPORTED_SPEC_VERSIONS,
    TOOL_CHOICE_TOOL,
)
from oxa.ir.loss import Loss
from oxa.ir.native import (
    CodecError,
    NativeAnnotation,
    NativeOmission,
    NativeProjection,
)
from oxa.ir.types import (
    ApplyPatchCreateFileOperation,
    ApplyPatchDeleteFileOperation,
    ApplyPatchOperation,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    WebSearchCallBlock,
    ApplyPatchUpdateFileOperation,
    Block,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    CustomToolFormat,
    CustomToolFormatGrammar,
    CustomToolFormatText,
    CustomToolInputDelta,
    CustomToolResultBlock,
    CustomToolUseBlock,
    Delta,
    Event,
    EventStream,
    DocumentBlock,
    ImageBlock,
    InputJsonDelta,
    InputTokensDetails,
    Message,
    MessageDelta,
    MessageDone,
    MessageStart,
    OutputTokensDetails,
    Params,
    Request,
    Response,
    ResponseFormat,
    ResponseFormatJsonObject,
    ResponseFormatJsonSchema,
    ResponseFormatText,
    SignatureDelta,
    TextBlock,
    TextCitation,
    TextDelta,
    ThinkingBlock,
    ThinkingDelta,
    RedactedThinkingBlock,
    CitationDelta,
    Tool,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
    Usage,
)


def dump_native_annotation(ann: NativeAnnotation) -> dict[str, Any]:
    out: dict[str, Any] = {
        "source": ann.source,
        "kind": ann.kind,
        "value": ann.value,
    }
    if ann.omission is not None:
        om_dict: dict[str, Any] = {
            "field": ann.omission.field,
            "reason": ann.omission.reason,
        }
        if ann.omission.detail is not None:
            om_dict["detail"] = ann.omission.detail
        out["omission"] = om_dict
    if ann.projection is not None:
        out["projection"] = {
            "origin": ann.projection.origin,
            "effort": ann.projection.effort,
        }
    return out


def load_native_annotation(data: dict[str, Any]) -> NativeAnnotation:
    if not isinstance(data, dict):
        raise CodecError("native annotation must be an object")
    source = data.get("source")
    kind = data.get("kind")
    value = data.get("value")
    if not isinstance(source, str) or not isinstance(kind, str) or not isinstance(value, str):
        raise CodecError("native annotation requires string source, kind, and value")

    omission: NativeOmission | None = None
    if "omission" in data:
        om_data = data["omission"]
        if not isinstance(om_data, dict):
            raise CodecError("native omission must be an object")
        field = om_data.get("field")
        reason = om_data.get("reason")
        if not isinstance(field, str) or not isinstance(reason, str):
            raise CodecError("native omission requires string field and reason")
        detail = om_data.get("detail")
        if detail is not None and not isinstance(detail, str):
            raise CodecError("native omission detail must be a string")
        omission = NativeOmission(field=field, reason=reason, detail=detail)

    projection: NativeProjection | None = None
    if "projection" in data:
        proj_data = data["projection"]
        if not isinstance(proj_data, dict):
            raise CodecError("native projection must be an object")
        origin = proj_data.get("origin")
        effort = proj_data.get("effort")
        if origin not in ("budget", "explicit"):
            raise CodecError("native projection origin must be 'budget' or 'explicit'")
        if effort not in ("minimal", "low", "medium", "high", "xhigh", "max"):
            raise CodecError("native projection effort is invalid")
        projection = NativeProjection(origin=origin, effort=effort)

    return NativeAnnotation(
        source=source,
        kind=kind,
        value=value,
        omission=omission,
        projection=projection,
    )


def _dump_usage(usage: Usage) -> dict[str, Any]:
    out: dict[str, Any] = {
        "input_tokens": usage.input_tokens,
        "output_tokens": usage.output_tokens,
    }
    if usage.cache_read_input_tokens is not None:
        out["cache_read_input_tokens"] = usage.cache_read_input_tokens
    if usage.cache_creation_input_tokens is not None:
        out["cache_creation_input_tokens"] = usage.cache_creation_input_tokens
    if usage.input_tokens_details is not None:
        out["input_tokens_details"] = {"cached_tokens": usage.input_tokens_details.cached_tokens}
    if usage.output_tokens_details is not None:
        out["output_tokens_details"] = {
            "reasoning_tokens": usage.output_tokens_details.reasoning_tokens
        }
    return out


def _load_usage(data: dict[str, Any]) -> Usage:
    input_details = data.get("input_tokens_details")
    output_details = data.get("output_tokens_details")
    return Usage(
        input_tokens=int(data.get("input_tokens", 0)),
        output_tokens=int(data.get("output_tokens", 0)),
        cache_read_input_tokens=(
            int(data["cache_read_input_tokens"])
            if data.get("cache_read_input_tokens") is not None
            else None
        ),
        cache_creation_input_tokens=(
            int(data["cache_creation_input_tokens"])
            if data.get("cache_creation_input_tokens") is not None
            else None
        ),
        input_tokens_details=(
            InputTokensDetails(cached_tokens=int(input_details["cached_tokens"]))
            if input_details is not None
            else None
        ),
        output_tokens_details=(
            OutputTokensDetails(reasoning_tokens=int(output_details["reasoning_tokens"]))
            if output_details is not None
            else None
        ),
    )


# ---- Blocks ----------------------------------------------------------------


def validate_citation_value(val: str) -> None:
    if not val:
        raise CodecError("ir: citation value cannot be empty")
    try:
        obj = json.loads(val)
    except Exception as e:
        raise CodecError(f"ir: citation value must be a JSON object: {e}") from e
    if not isinstance(obj, dict):
        raise CodecError("ir: citation value must be a JSON object")
    cite_type = obj.get("type")
    if not isinstance(cite_type, str):
        raise CodecError("ir: citation value requires type field")
    if cite_type == "char_location":
        for k in obj:
            if k not in ("type", "cited_text", "document_index", "document_title", "start_char_index", "end_char_index", "file_id"):
                raise CodecError(f'ir: unknown property "{k}" on char_location citation')
        if not isinstance(obj.get("cited_text"), str):
            raise CodecError("ir: char_location citation requires cited_text")
        if not (isinstance(obj.get("document_index"), int) and not isinstance(obj.get("document_index"), bool) and obj["document_index"] >= 0):
            raise CodecError("ir: char_location citation requires document_index")
        if obj.get("document_title") is not None and not isinstance(obj.get("document_title"), str):
            raise CodecError("ir: char_location citation requires document_title")
        if not (isinstance(obj.get("start_char_index"), int) and not isinstance(obj.get("start_char_index"), bool) and obj["start_char_index"] >= 0):
            raise CodecError("ir: char_location citation requires start_char_index")
        if not (isinstance(obj.get("end_char_index"), int) and not isinstance(obj.get("end_char_index"), bool) and obj["end_char_index"] > obj["start_char_index"]):
            raise CodecError("ir: char_location citation requires end_char_index")
    elif cite_type == "page_location":
        for k in obj:
            if k not in ("type", "cited_text", "document_index", "document_title", "start_page_number", "end_page_number", "file_id"):
                raise CodecError(f'ir: unknown property "{k}" on page_location citation')
        if not isinstance(obj.get("cited_text"), str):
            raise CodecError("ir: page_location citation requires cited_text")
        if not (isinstance(obj.get("document_index"), int) and not isinstance(obj.get("document_index"), bool) and obj["document_index"] >= 0):
            raise CodecError("ir: page_location citation requires document_index")
        if obj.get("document_title") is not None and not isinstance(obj.get("document_title"), str):
            raise CodecError("ir: page_location citation requires document_title")
        if not (isinstance(obj.get("start_page_number"), int) and not isinstance(obj.get("start_page_number"), bool) and obj["start_page_number"] >= 1):
            raise CodecError("ir: page_location citation requires start_page_number")
        if not (isinstance(obj.get("end_page_number"), int) and not isinstance(obj.get("end_page_number"), bool) and obj["end_page_number"] >= obj["start_page_number"]):
            raise CodecError("ir: page_location citation requires end_page_number")
    elif cite_type == "content_block_location":
        for k in obj:
            if k not in ("type", "cited_text", "document_index", "document_title", "start_block_index", "end_block_index", "file_id"):
                raise CodecError(f'ir: unknown property "{k}" on content_block_location citation')
        if not isinstance(obj.get("cited_text"), str):
            raise CodecError("ir: content_block_location citation requires cited_text")
        if not (isinstance(obj.get("document_index"), int) and not isinstance(obj.get("document_index"), bool) and obj["document_index"] >= 0):
            raise CodecError("ir: content_block_location citation requires document_index")
        if obj.get("document_title") is not None and not isinstance(obj.get("document_title"), str):
            raise CodecError("ir: content_block_location citation requires document_title")
        if not (isinstance(obj.get("start_block_index"), int) and not isinstance(obj.get("start_block_index"), bool) and obj["start_block_index"] >= 0):
            raise CodecError("ir: content_block_location citation requires start_block_index")
        if not (isinstance(obj.get("end_block_index"), int) and not isinstance(obj.get("end_block_index"), bool) and obj["end_block_index"] > obj["start_block_index"]):
            raise CodecError("ir: content_block_location citation requires end_block_index")
    elif cite_type == "web_search_result_location":
        for k in obj:
            if k not in ("type", "cited_text", "encrypted_index", "title", "url"):
                raise CodecError(f'ir: unknown property "{k}" on web_search_result_location citation')
        if not isinstance(obj.get("cited_text"), str):
            raise CodecError("ir: web_search_result_location citation requires cited_text")
        if not isinstance(obj.get("encrypted_index"), str):
            raise CodecError("ir: web_search_result_location citation requires encrypted_index")
        if obj.get("title") is not None and not isinstance(obj.get("title"), str):
            raise CodecError("ir: web_search_result_location citation requires title")
        if not isinstance(obj.get("url"), str):
            raise CodecError("ir: web_search_result_location citation requires url")
    elif cite_type == "search_result_location":
        for k in obj:
            if k not in ("type", "cited_text", "search_result_index", "source", "title", "start_block_index", "end_block_index"):
                raise CodecError(f'ir: unknown property "{k}" on search_result_location citation')
        if not isinstance(obj.get("cited_text"), str):
            raise CodecError("ir: search_result_location citation requires cited_text")
        if not (isinstance(obj.get("search_result_index"), int) and not isinstance(obj.get("search_result_index"), bool) and obj["search_result_index"] >= 0):
            raise CodecError("ir: search_result_location citation requires search_result_index")
        if not isinstance(obj.get("source"), str):
            raise CodecError("ir: search_result_location citation requires source")
        if obj.get("title") is not None and not isinstance(obj.get("title"), str):
            raise CodecError("ir: search_result_location citation requires title")
        if not (isinstance(obj.get("start_block_index"), int) and not isinstance(obj.get("start_block_index"), bool) and obj["start_block_index"] >= 0):
            raise CodecError("ir: search_result_location citation requires start_block_index")
        if not (isinstance(obj.get("end_block_index"), int) and not isinstance(obj.get("end_block_index"), bool) and obj["end_block_index"] > obj["start_block_index"]):
            raise CodecError("ir: search_result_location citation requires end_block_index")
    else:
        raise CodecError(f"ir: unknown citation type {cite_type!r}")


def _validate_web_search(identifier: str, status: str, action: object) -> None:
    from oxa.ir.web_search import inspect_web_search

    try:
        inspect_web_search(identifier, status, action)
    except ValueError as exc:
        raise CodecError(str(exc)) from exc


def load_web_search_block(data: dict[str, Any]) -> WebSearchCallBlock:
    identifier = data.get("id")
    status = data.get("status")
    action = data.get("action")
    if not isinstance(identifier, str) or not isinstance(status, str):
        raise CodecError("web_search_call requires non-empty id and status")
    _validate_web_search(identifier, status, action)
    from oxa.ir.web_search import validate_web_search_extra
    extra = data.get("extra", {})
    try:
        validate_web_search_extra(extra)
    except ValueError as exc:
        raise CodecError(str(exc)) from exc
    return WebSearchCallBlock(id=identifier, status=status, action=dict(action), extra=dict(extra))


def dump_block(block: Block) -> dict[str, Any]:
    if getattr(block, "native", ()):
        raise CodecError("native is only permitted on request blocks")
    if isinstance(block, TextBlock):
        out: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": block.text}
        if block.citations:
            for c in block.citations:
                if c.source != "anthropic":
                    raise CodecError(f"ir: unknown citation source {c.source!r}")
                validate_citation_value(c.value)
            out["citations"] = [{"source": c.source, "value": c.value} for c in block.citations]
        return out
    if isinstance(block, RedactedThinkingBlock):
        return {"type": BLOCK_TYPE_REDACTED_THINKING, "data": block.data}
    if isinstance(block, ThinkingBlock):
        out: dict[str, Any] = {
            "type": BLOCK_TYPE_THINKING,
            "thinking": block.thinking,
        }
        if block.signature is not None:
            out["signature"] = block.signature
        return out
    if isinstance(block, ImageBlock):
        has_data = bool(block.data)
        has_url = bool(block.url)
        if has_data == has_url:
            raise CodecError("image block must carry exactly one of data or url")
        if has_data:
            if not block.media_type:
                raise CodecError("image block with data requires media_type")
            return {
                "type": BLOCK_TYPE_IMAGE,
                "media_type": block.media_type,
                "data": block.data,
            }
        return {"type": BLOCK_TYPE_IMAGE, "url": block.url}
    if isinstance(block, ToolUseBlock):
        if not block.id:
            raise CodecError("tool_use block requires non-empty id")
        if not block.name:
            raise CodecError("tool_use block requires non-empty name")
        if block.input is None:
            raise CodecError("tool_use block requires input")
        return {
            "type": BLOCK_TYPE_TOOL_USE,
            "id": block.id,
            "name": block.name,
            "input": block.input,
        }
    if isinstance(block, ToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("tool_result block requires non-empty tool_use_id")
        res: dict[str, Any] = {
            "type": BLOCK_TYPE_TOOL_RESULT,
            "tool_use_id": block.tool_use_id,
            "content": [dump_block(b) for b in block.content],
        }
        if block.is_error:
            res["is_error"] = True
        return res
    if isinstance(block, CustomToolUseBlock):
        if not block.id or not block.name or not isinstance(block.input, str):
            raise CodecError("custom_tool_use requires non-empty id/name and string input")
        return {"type": BLOCK_TYPE_CUSTOM_TOOL_USE, "id": block.id, "name": block.name, "input": block.input}
    if isinstance(block, CustomToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("custom_tool_result block requires non-empty tool_use_id")
        output = block.output
        if isinstance(output, list):
            if any(not isinstance(b, (TextBlock, ImageBlock)) for b in output):
                raise CodecError("custom_tool_result output array accepts only text and image blocks")
            output = [dump_block(b) for b in output]
        elif not isinstance(output, str):
            raise CodecError("custom_tool_result output must be a string or array")
        return {"type": BLOCK_TYPE_CUSTOM_TOOL_RESULT, "tool_use_id": block.tool_use_id, "output": output}
    if isinstance(block, ApplyPatchToolUseBlock):
        if not block.id:
            raise CodecError("apply_patch_tool_use requires non-empty id")
        return {
            "type": BLOCK_TYPE_APPLY_PATCH_TOOL_USE,
            "id": block.id,
            "status": block.status,
            "operation": dump_apply_patch_operation(block.operation),
        }
    if isinstance(block, ApplyPatchToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("apply_patch_tool_result requires non-empty tool_use_id")
        res = {
            "type": BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT,
            "tool_use_id": block.tool_use_id,
            "status": block.status,
        }
        if block.output is not None:
            res["output"] = block.output
        return res
    if isinstance(block, WebSearchCallBlock):
        _validate_web_search(block.id, block.status, block.action)
        from oxa.ir.web_search import validate_web_search_extra
        validate_web_search_extra(block.extra)
        return {
            "type": BLOCK_TYPE_WEB_SEARCH_CALL,
            "id": block.id,
            "status": block.status,
            "action": block.action,
            **({"extra": block.extra} if block.extra else {}),
        }
    raise CodecError(f"unknown block type: {type(block)}")


def dump_system_block(block: TextBlock) -> dict[str, Any]:
    if not isinstance(block, TextBlock):
        raise CodecError("system block must be text")
    out: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": block.text}
    if block.role:
        if block.role != "developer":
            raise CodecError(f"ir: system role {block.role!r} is not supported")
        out["role"] = block.role
    if block.native:
        out["native"] = [dump_native_annotation(a) for a in block.native]
    return out


def dump_request_block(block: Block) -> dict[str, Any]:
    if isinstance(block, TextBlock):
        out: dict[str, Any] = {"type": BLOCK_TYPE_TEXT, "text": block.text}
        if block.citations:
            for c in block.citations:
                if c.source != "anthropic":
                    raise CodecError(f"ir: unknown citation source {c.source!r}")
                validate_citation_value(c.value)
            out["citations"] = [{"source": c.source, "value": c.value} for c in block.citations]
        if block.role:
            raise CodecError("role is only permitted on system blocks")
    elif isinstance(block, RedactedThinkingBlock):
        out = {"type": BLOCK_TYPE_REDACTED_THINKING, "data": block.data}
    elif isinstance(block, ThinkingBlock):
        out = {"type": BLOCK_TYPE_THINKING, "thinking": block.thinking}
        if block.signature is not None:
            out["signature"] = block.signature
    elif isinstance(block, ImageBlock):
        has_data = bool(block.data)
        has_url = bool(block.url)
        if has_data == has_url:
            raise CodecError("image block must carry exactly one of data or url")
        if has_data:
            if not block.media_type:
                raise CodecError("image block with data requires media_type")
            out = {
                "type": BLOCK_TYPE_IMAGE,
                "media_type": block.media_type,
                "data": block.data,
            }
        else:
            out = {"type": BLOCK_TYPE_IMAGE, "url": block.url}
    elif isinstance(block, DocumentBlock):
        has_data = bool(block.data)
        has_url = bool(block.url)
        has_file = bool(block.file_id)
        if has_data + has_url + has_file != 1:
            raise CodecError("document block must carry exactly one of data, url, or file_id")
        if has_data:
            if not block.media_type:
                raise CodecError("document block with data requires media_type")
            out = {
                "type": BLOCK_TYPE_DOCUMENT,
                "media_type": block.media_type,
                "data": block.data,
            }
        elif has_url:
            if block.media_type:
                raise CodecError("document block with url must not carry media_type")
            out = {"type": BLOCK_TYPE_DOCUMENT, "url": block.url}
        else:
            if block.media_type:
                raise CodecError("document block with file_id must not carry media_type")
            out = {"type": BLOCK_TYPE_DOCUMENT, "file_id": block.file_id}
    elif isinstance(block, ToolUseBlock):
        if not block.id:
            raise CodecError("tool_use block requires non-empty id")
        if not block.name:
            raise CodecError("tool_use block requires non-empty name")
        if block.input is None:
            raise CodecError("tool_use block requires input")
        out = {
            "type": BLOCK_TYPE_TOOL_USE,
            "id": block.id,
            "name": block.name,
            "input": block.input,
        }
    elif isinstance(block, ToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("tool_result block requires non-empty tool_use_id")
        out = {
            "type": BLOCK_TYPE_TOOL_RESULT,
            "tool_use_id": block.tool_use_id,
            "content": [dump_request_block(b) for b in block.content],
        }
        if block.is_error:
            out["is_error"] = True
    elif isinstance(block, CustomToolUseBlock):
        if not block.id or not block.name or not isinstance(block.input, str):
            raise CodecError("custom_tool_use requires non-empty id/name and string input")
        out = {"type": BLOCK_TYPE_CUSTOM_TOOL_USE, "id": block.id, "name": block.name, "input": block.input}
    elif isinstance(block, CustomToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("custom_tool_result block requires non-empty tool_use_id")
        output = block.output
        if isinstance(output, list):
            if any(not isinstance(b, (TextBlock, ImageBlock)) for b in output):
                raise CodecError("custom_tool_result output array accepts only text and image blocks")
            output_dumped = [dump_request_block(b) for b in output]
        elif not isinstance(output, str):
            raise CodecError("custom_tool_result output must be a string or array")
        else:
            output_dumped = output
        out = {"type": BLOCK_TYPE_CUSTOM_TOOL_RESULT, "tool_use_id": block.tool_use_id, "output": output_dumped}
    elif isinstance(block, ApplyPatchToolUseBlock):
        if not block.id:
            raise CodecError("apply_patch_tool_use requires non-empty id")
        out = {
            "type": BLOCK_TYPE_APPLY_PATCH_TOOL_USE,
            "id": block.id,
            "status": block.status,
            "operation": dump_apply_patch_operation(block.operation),
        }
    elif isinstance(block, ApplyPatchToolResultBlock):
        if not block.tool_use_id:
            raise CodecError("apply_patch_tool_result requires non-empty tool_use_id")
        out = {
            "type": BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT,
            "tool_use_id": block.tool_use_id,
            "status": block.status,
        }
        if block.output is not None:
            out["output"] = block.output
    elif isinstance(block, WebSearchCallBlock):
        _validate_web_search(block.id, block.status, block.action)
        from oxa.ir.web_search import validate_web_search_extra
        validate_web_search_extra(block.extra)
        out = {
            "type": BLOCK_TYPE_WEB_SEARCH_CALL,
            "id": block.id,
            "status": block.status,
            "action": block.action,
            **({"extra": block.extra} if block.extra else {}),
        }
    else:
        raise CodecError(f"unknown block type: {type(block)}")

    if getattr(block, "native", ()):
        out["native"] = [dump_native_annotation(a) for a in block.native]
    return out


def dump_apply_patch_operation(op: ApplyPatchOperation) -> dict[str, Any]:
    if isinstance(op, (ApplyPatchCreateFileOperation, ApplyPatchUpdateFileOperation)):
        return {"type": op.type, "path": op.path, "diff": op.diff}
    if isinstance(op, ApplyPatchDeleteFileOperation):
        return {"type": op.type, "path": op.path}
    raise CodecError(f"unknown apply patch operation: {type(op)}")


def load_apply_patch_operation(data: dict[str, Any]) -> ApplyPatchOperation:
    if not isinstance(data, dict):
        raise CodecError("operation must be a dict")
    kind = data.get("type")
    path = data.get("path")
    if not isinstance(path, str):
        raise CodecError("operation path must be a string")
    if kind == "create_file":
        diff = data.get("diff")
        if not isinstance(diff, str):
            raise CodecError("create_file operation requires diff string")
        return ApplyPatchCreateFileOperation(path=path, diff=diff)
    if kind == "update_file":
        diff = data.get("diff")
        if not isinstance(diff, str):
            raise CodecError("update_file operation requires diff string")
        return ApplyPatchUpdateFileOperation(path=path, diff=diff)
    if kind == "delete_file":
        if "diff" in data:
            raise CodecError("delete_file operation must not carry diff")
        return ApplyPatchDeleteFileOperation(path=path)
    raise CodecError(f"unknown apply patch operation type: {kind}")


def load_apply_patch_call_status(status: Any) -> str:
    if status not in ("in_progress", "completed"):
        raise CodecError(f"invalid apply_patch_call status: {status}")
    return status


def load_apply_patch_result_status(status: Any) -> str:
    if status not in ("completed", "failed"):
        raise CodecError(f"invalid apply_patch_call_output status: {status}")
    return status


def load_block(data: dict[str, Any]) -> Block:
    if "native" in data:
        raise CodecError("native is only permitted on request blocks")
    kind = data.get("type")
    if kind == BLOCK_TYPE_DOCUMENT:
        raise CodecError('unknown block type "document"')
    if kind == BLOCK_TYPE_TEXT:
        cites = None
        if "citations" in data:
            c_raw = data["citations"]
            if not isinstance(c_raw, list):
                raise CodecError("citations must be an array")
            cites = []
            for item in c_raw:
                if not isinstance(item, dict) or item.get("source") != "anthropic":
                    raise CodecError("unknown citation source")
                val = item.get("value")
                if not isinstance(val, str):
                    raise CodecError("citation value must be a string")
                validate_citation_value(val)
                cites.append(TextCitation(source="anthropic", value=val))
        return TextBlock(text=data["text"], citations=tuple(cites) if cites else None)
    if kind == BLOCK_TYPE_REDACTED_THINKING:
        d = data.get("data")
        if not isinstance(d, str):
            raise CodecError("redacted_thinking requires string data")
        return RedactedThinkingBlock(data=d)
    if kind == BLOCK_TYPE_THINKING:
        return ThinkingBlock(thinking=data["thinking"], signature=data.get("signature"))
    if kind == BLOCK_TYPE_IMAGE:
        return ImageBlock(
            media_type=data.get("media_type"),
            data=data.get("data"),
            url=data.get("url"),
        )
    if kind == BLOCK_TYPE_TOOL_USE:
        return ToolUseBlock(
            id=data["id"],
            name=data["name"],
            input=data["input"],
        )
    if kind == BLOCK_TYPE_TOOL_RESULT:
        content_raw = data.get("content", [])
        return ToolResultBlock(
            tool_use_id=data["tool_use_id"],
            content=[load_block(b) for b in content_raw],
            is_error=bool(data.get("is_error", False)),
        )
    if kind == BLOCK_TYPE_CUSTOM_TOOL_USE:
        call_id, name, input_text = data.get("id"), data.get("name"), data.get("input")
        if not isinstance(call_id, str) or not call_id or not isinstance(name, str) or not name or not isinstance(input_text, str):
            raise CodecError("custom_tool_use requires non-empty id/name and string input")
        return CustomToolUseBlock(id=call_id, name=name, input=input_text)
    if kind == BLOCK_TYPE_CUSTOM_TOOL_RESULT:
        call_id, output = data.get("tool_use_id"), data.get("output")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("custom_tool_result requires non-empty tool_use_id")
        if isinstance(output, list):
            blocks = [load_block(b) for b in output]
            if any(not isinstance(b, (TextBlock, ImageBlock)) for b in blocks):
                raise CodecError("custom_tool_result output array accepts only text and image blocks")
            output = blocks
        elif not isinstance(output, str):
            raise CodecError("custom_tool_result output must be a string or array")
        return CustomToolResultBlock(tool_use_id=call_id, output=output)
    if kind == BLOCK_TYPE_APPLY_PATCH_TOOL_USE:
        call_id = data.get("id")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("apply_patch_tool_use requires non-empty id")
        return ApplyPatchToolUseBlock(
            id=call_id,
            status=load_apply_patch_call_status(data.get("status")),
            operation=load_apply_patch_operation(data.get("operation")),
        )
    if kind == BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT:
        call_id = data.get("tool_use_id")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("apply_patch_tool_result requires non-empty tool_use_id")
        output = data.get("output")
        if output is not None and not isinstance(output, str):
            raise CodecError("apply_patch_tool_result output must be a string")
        return ApplyPatchToolResultBlock(
            tool_use_id=call_id,
            status=load_apply_patch_result_status(data.get("status")),
            output=output,
        )
    if kind == BLOCK_TYPE_WEB_SEARCH_CALL:
        return load_web_search_block(data)
    raise CodecError(f"unknown block discriminant: {kind}")


def load_request_block(data: dict[str, Any]) -> Block:
    if "role" in data:
        raise CodecError("role is only permitted on system blocks")
    native = tuple(load_native_annotation(a) for a in data.get("native", []))
    kind = data.get("type")
    if kind == BLOCK_TYPE_TEXT:
        cites = None
        if "citations" in data:
            c_raw = data["citations"]
            if not isinstance(c_raw, list):
                raise CodecError("citations must be an array")
            cites = []
            for item in c_raw:
                if not isinstance(item, dict) or item.get("source") != "anthropic":
                    raise CodecError("unknown citation source")
                val = item.get("value")
                if not isinstance(val, str):
                    raise CodecError("citation value must be a string")
                validate_citation_value(val)
                cites.append(TextCitation(source="anthropic", value=val))
        return TextBlock(text=data["text"], citations=tuple(cites) if cites else None, native=native)
    if kind == BLOCK_TYPE_REDACTED_THINKING:
        d = data.get("data")
        if not isinstance(d, str):
            raise CodecError("redacted_thinking requires string data")
        return RedactedThinkingBlock(data=d, native=native)
    if kind == BLOCK_TYPE_THINKING:
        return ThinkingBlock(thinking=data["thinking"], signature=data.get("signature"), native=native)
    if kind == BLOCK_TYPE_IMAGE:
        return ImageBlock(
            media_type=data.get("media_type"),
            data=data.get("data"),
            url=data.get("url"),
            native=native,
        )
    if kind == BLOCK_TYPE_DOCUMENT:
        return DocumentBlock(
            media_type=data.get("media_type"),
            data=data.get("data"),
            url=data.get("url"),
            file_id=data.get("file_id"),
            native=native,
        )
    if kind == BLOCK_TYPE_TOOL_USE:
        return ToolUseBlock(
            id=data["id"],
            name=data["name"],
            input=data["input"],
            native=native,
        )
    if kind == BLOCK_TYPE_TOOL_RESULT:
        content_raw = data.get("content", [])
        return ToolResultBlock(
            tool_use_id=data["tool_use_id"],
            content=[load_request_block(b) for b in content_raw],
            is_error=bool(data.get("is_error", False)),
            native=native,
        )
    if kind == BLOCK_TYPE_CUSTOM_TOOL_USE:
        call_id, name, input_text = data.get("id"), data.get("name"), data.get("input")
        if not isinstance(call_id, str) or not call_id or not isinstance(name, str) or not name or not isinstance(input_text, str):
            raise CodecError("custom_tool_use requires non-empty id/name and string input")
        return CustomToolUseBlock(id=call_id, name=name, input=input_text, native=native)
    if kind == BLOCK_TYPE_CUSTOM_TOOL_RESULT:
        call_id, output = data.get("tool_use_id"), data.get("output")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("custom_tool_result requires non-empty tool_use_id")
        if isinstance(output, list):
            blocks = [load_request_block(b) for b in output]
            if any(not isinstance(b, (TextBlock, ImageBlock)) for b in blocks):
                raise CodecError("custom_tool_result output array accepts only text and image blocks")
            output = blocks
        elif not isinstance(output, str):
            raise CodecError("custom_tool_result output must be a string or array")
        return CustomToolResultBlock(tool_use_id=call_id, output=output, native=native)
    if kind == BLOCK_TYPE_APPLY_PATCH_TOOL_USE:
        call_id = data.get("id")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("apply_patch_tool_use requires non-empty id")
        return ApplyPatchToolUseBlock(
            id=call_id,
            status=load_apply_patch_call_status(data.get("status")),
            operation=load_apply_patch_operation(data.get("operation")),
            native=native,
        )
    if kind == BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT:
        call_id = data.get("tool_use_id")
        if not isinstance(call_id, str) or not call_id:
            raise CodecError("apply_patch_tool_result requires non-empty tool_use_id")
        output = data.get("output")
        if output is not None and not isinstance(output, str):
            raise CodecError("apply_patch_tool_result output must be a string")
        return ApplyPatchToolResultBlock(
            tool_use_id=call_id,
            status=load_apply_patch_result_status(data.get("status")),
            output=output,
            native=native,
        )
    if kind == BLOCK_TYPE_WEB_SEARCH_CALL:
        return load_web_search_block(data)
    raise CodecError(f"unknown block discriminant: {kind}")


# ---- Deltas & Events -------------------------------------------------------


def dump_delta(delta: Delta) -> dict[str, Any]:
    if isinstance(delta, TextDelta):
        return {"type": DELTA_TYPE_TEXT_DELTA, "text": delta.text}
    if isinstance(delta, InputJsonDelta):
        return {"type": DELTA_TYPE_INPUT_JSON_DELTA, "partial_json": delta.partial_json}
    if isinstance(delta, ThinkingDelta):
        return {"type": DELTA_TYPE_THINKING_DELTA, "text": delta.text}
    if isinstance(delta, SignatureDelta):
        return {"type": DELTA_TYPE_SIGNATURE_DELTA, "signature": delta.signature}
    if isinstance(delta, CustomToolInputDelta):
        return {"type": DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA, "text": delta.text}
    if isinstance(delta, CitationDelta):
        validate_citation_value(delta.citation.value)
        return {
            "type": DELTA_TYPE_CITATION,
            "citation": {"source": delta.citation.source, "value": delta.citation.value},
        }
    raise CodecError(f"unknown delta type: {type(delta)}")


def load_delta(data: dict[str, Any]) -> Delta:
    kind = data.get("type")
    if kind == DELTA_TYPE_TEXT_DELTA:
        return TextDelta(text=data["text"])
    if kind == DELTA_TYPE_INPUT_JSON_DELTA:
        return InputJsonDelta(partial_json=data["partial_json"])
    if kind == DELTA_TYPE_THINKING_DELTA:
        return ThinkingDelta(text=data["text"])
    if kind == DELTA_TYPE_SIGNATURE_DELTA:
        return SignatureDelta(signature=data["signature"])
    if kind == DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA:
        return CustomToolInputDelta(text=data["text"])
    if kind == DELTA_TYPE_CITATION:
        c = data.get("citation")
        if not isinstance(c, dict) or c.get("source") != "anthropic":
            raise CodecError("unknown citation source")
        val = c.get("value")
        if not isinstance(val, str):
            raise CodecError("citation value must be a string")
        validate_citation_value(val)
        return CitationDelta(citation=TextCitation(source="anthropic", value=val))
    raise CodecError(f"unknown delta discriminant: {kind}")


def dump_event(event: Event) -> dict[str, Any]:
    if isinstance(event, MessageStart):
        return {
            "type": EVENT_TYPE_MESSAGE_START,
            "id": event.id,
            "model": event.model,
        }
    if isinstance(event, ContentBlockStart):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_START,
            "index": event.index,
            "block": dump_block(event.block),
        }
    if isinstance(event, ContentBlockDelta):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
            "index": event.index,
            "delta": dump_delta(event.delta),
        }
    if isinstance(event, ContentBlockStop):
        return {
            "type": EVENT_TYPE_CONTENT_BLOCK_STOP,
            "index": event.index,
        }
    if isinstance(event, MessageDelta):
        out: dict[str, Any] = {
            "type": EVENT_TYPE_MESSAGE_DELTA,
            "stop_reason": event.stop_reason,
            "usage": _dump_usage(event.usage),
        }
        if event.stop_reason == STOP_STOP_SEQUENCE:
            if event.stop_sequence:
                out["stop_sequence"] = event.stop_sequence
        elif event.stop_sequence is not None:
            raise CodecError("stop_sequence is only permitted when stop_reason is stop_sequence")
        return out
    if isinstance(event, MessageDone):
        return {"type": EVENT_TYPE_MESSAGE_DONE}
    raise CodecError(f"unknown event type: {type(event)}")


def load_event(data: dict[str, Any]) -> Event:
    kind = data.get("type")
    if kind == EVENT_TYPE_MESSAGE_START:
        return MessageStart(id=data["id"], model=data["model"])
    if kind == EVENT_TYPE_CONTENT_BLOCK_START:
        return ContentBlockStart(index=int(data["index"]), block=load_block(data["block"]))
    if kind == EVENT_TYPE_CONTENT_BLOCK_DELTA:
        return ContentBlockDelta(index=int(data["index"]), delta=load_delta(data["delta"]))
    if kind == EVENT_TYPE_CONTENT_BLOCK_STOP:
        return ContentBlockStop(index=int(data["index"]))
    if kind == EVENT_TYPE_MESSAGE_DELTA:
        usage_data = data.get("usage", {})
        usage = _load_usage(usage_data)
        return MessageDelta(
            stop_reason=data["stop_reason"],
            usage=usage,
            stop_sequence=data.get("stop_sequence"),
        )
    if kind == EVENT_TYPE_MESSAGE_DONE:
        return MessageDone()
    raise CodecError(f"unknown event discriminant: {kind}")


# ---- Request ---------------------------------------------------------------


def dump_request(req: Request) -> dict[str, Any]:
    out: dict[str, Any] = {
        "specVersion": SPEC_VERSION,
        "model": req.model,
        "messages": [
            {
                "role": m.role,
                "content": [dump_request_block(b) for b in m.content],
            }
            for m in req.messages
        ],
    }
    if req.native:
        out["native"] = [dump_native_annotation(a) for a in req.native]
    if req.system:
        out["system"] = [dump_system_block(b) for b in req.system]
    if req.tools:
        tools_out: list[dict[str, Any]] = []
        for t in req.tools:
            if t.type == "apply_patch":
                t_dict: dict[str, Any] = {"type": "apply_patch"}
            elif t.type == "native":
                if not t.name or not t.source or not t.definition:
                    raise CodecError("native tool requires name, source, and definition")
                t_dict = {"type": "native", "name": t.name, "source": t.source, "definition": t.definition}
            elif t.type == "custom":
                if t.input_schema is not None:
                    raise CodecError("custom tool must not carry input_schema")
                t_dict: dict[str, Any] = {"type": "custom", "name": t.name}
                if t.description is not None:
                    t_dict["description"] = t.description
                if t.format is not None:
                    t_dict["format"] = dump_custom_tool_format(t.format)
            elif t.type == "function":
                if t.format is not None or t.input_schema is None:
                    raise CodecError("function tool requires input_schema and must not carry format")
                t_dict = {"name": t.name, "input_schema": t.input_schema}
                if t.strict is not None:
                    t_dict["strict"] = t.strict
                if t.description:
                    t_dict["description"] = t.description
            else:
                raise CodecError(f"unknown tool type: {t.type!r}")
            if t.native:
                t_dict["native"] = [dump_native_annotation(a) for a in t.native]
            tools_out.append(t_dict)
        out["tools"] = tools_out
    if req.tool_choice:
        tc_dict: dict[str, Any] = {"mode": req.tool_choice.mode}
        if req.tool_choice.mode == TOOL_CHOICE_TOOL:
            if req.tool_choice.tool_type == "apply_patch":
                tc_dict["tool_type"] = "apply_patch"
            else:
                if not req.tool_choice.name or req.tool_choice.tool_type not in ("function", "custom", "native"):
                    raise CodecError("tool choice in mode 'tool' requires name and function/custom/native tool_type")
                tc_dict["name"] = req.tool_choice.name
                if req.tool_choice.tool_type != "function":
                    tc_dict["tool_type"] = req.tool_choice.tool_type
        elif req.tool_choice.tool_type != "function":
            raise CodecError("tool_choice.tool_type is only valid when mode is tool")
        out["tool_choice"] = tc_dict
    if req.params:
        p_dict: dict[str, Any] = {}
        if req.params.temperature is not None:
            p_dict["temperature"] = req.params.temperature
        if req.params.top_p is not None:
            p_dict["top_p"] = req.params.top_p
        if req.params.max_tokens is not None:
            p_dict["max_tokens"] = req.params.max_tokens
        if req.params.stop_sequences is not None:
            p_dict["stop_sequences"] = req.params.stop_sequences
        if req.params.reasoning_effort is not None:
            p_dict["reasoning_effort"] = req.params.reasoning_effort
        if req.params.verbosity is not None:
            if req.params.verbosity not in ("low", "medium", "high"):
                raise CodecError("invalid params.verbosity")
            p_dict["verbosity"] = req.params.verbosity
        if req.params.response_format is not None:
            p_dict["response_format"] = dump_response_format(req.params.response_format)
        if p_dict:
            out["params"] = p_dict
    if req.metadata:
        for k in req.metadata:
            if not k:
                raise CodecError("metadata key cannot be empty")
        out["metadata"] = req.metadata
    return out


def dump_request_json(req: Request) -> str:
    """编码 canonical 请求并保留响应格式 schema 的原始文本。"""
    from oxa._raw_schema import serialize_json_with_schema

    wire = dump_request(req)
    fmt = req.params.response_format if req.params else None
    if isinstance(fmt, ResponseFormatJsonSchema):
        return serialize_json_with_schema(wire, ("params", "response_format", "schema"), fmt.schema, fmt.schema_raw)
    return json.dumps(wire, ensure_ascii=False, allow_nan=False, separators=(",", ":"))


def dump_response_format(rf: ResponseFormat) -> dict[str, Any]:
    if isinstance(rf, ResponseFormatText):
        return {"type": "text"}
    if isinstance(rf, ResponseFormatJsonObject):
        return {"type": "json_object"}
    if isinstance(rf, ResponseFormatJsonSchema):
        res: dict[str, Any] = {
            "type": "json_schema",
            "schema": rf.schema,
        }
        if rf.name:
            res["name"] = rf.name
        if rf.description is not None:
            res["description"] = rf.description
        if rf.strict is not None:
            res["strict"] = rf.strict
        return res
    raise CodecError(f"unknown response format type: {type(rf)}")


def dump_custom_tool_format(fmt: CustomToolFormat) -> dict[str, Any]:
    if isinstance(fmt, CustomToolFormatText):
        return {"type": "text"}
    if isinstance(fmt, CustomToolFormatGrammar):
        if fmt.syntax not in ("lark", "regex") or not isinstance(fmt.definition, str):
            raise CodecError("custom grammar format requires syntax lark/regex and string definition")
        return {"type": "grammar", "syntax": fmt.syntax, "definition": fmt.definition}
    raise CodecError(f"unknown custom tool format: {type(fmt)}")


def load_custom_tool_format(data: dict[str, Any]) -> CustomToolFormat:
    if not isinstance(data, dict):
        raise CodecError("custom tool format must be an object")
    kind = data.get("type")
    if kind == "text":
        return CustomToolFormatText()
    if kind == "grammar":
        syntax, definition = data.get("syntax"), data.get("definition")
        if syntax not in ("lark", "regex") or not isinstance(definition, str):
            raise CodecError("custom grammar format requires syntax lark/regex and string definition")
        return CustomToolFormatGrammar(syntax=syntax, definition=definition)
    raise CodecError(f"unknown custom tool format type: {kind!r}")


def load_response_format(data: dict[str, Any]) -> ResponseFormat:
    kind = data.get("type")
    if kind == "text":
        return ResponseFormatText()
    if kind == "json_object":
        return ResponseFormatJsonObject()
    if kind == "json_schema":
        name = data.get("name", "")
        if "name" in data and (not isinstance(name, str) or not name):
            raise CodecError("response_format.json_schema name must be non-empty when present")
        schema = data.get("schema")
        if not isinstance(schema, dict):
            raise CodecError("response_format.json_schema requires schema object")
        desc = data.get("description")
        if desc is not None and not isinstance(desc, str):
            raise CodecError("response_format.json_schema description must be a string")
        strict = data.get("strict")
        if strict is not None and not isinstance(strict, bool):
            raise CodecError("response_format.json_schema strict must be a boolean")
        return ResponseFormatJsonSchema(
            name=name,
            schema=schema,
            description=desc,
            strict=strict,
        )
    raise CodecError(f"unknown response_format discriminant: {kind}")


def load_system_block(data: dict[str, Any]) -> TextBlock:
    if data.get("type") != BLOCK_TYPE_TEXT:
        raise CodecError("system block must be text")
    if not isinstance(data.get("text"), str):
        raise CodecError("system block text must be a string")
    role = data.get("role", "")
    if role not in ("", "developer"):
        raise CodecError(f"ir: system role {role!r} is not supported")
    native = tuple(load_native_annotation(a) for a in data.get("native", []))
    return TextBlock(text=data["text"], native=native, role=role)


def load_request(data: dict[str, Any] | str) -> Request:
    raw: dict[str, Any] = json.loads(data) if isinstance(data, str) else data
    version = raw.get("specVersion")
    if version not in SUPPORTED_SPEC_VERSIONS:
        raise CodecError(
            f"unsupported specVersion {version!r}, supported versions are {SUPPORTED_SPEC_VERSIONS!r}"
        )

    req_native = tuple(load_native_annotation(a) for a in raw.get("native", []))

    system: list[TextBlock] = []
    if "system" in raw:
        for b in raw["system"]:
            system.append(load_system_block(b))

    messages: list[Message] = []
    for m in raw.get("messages", []):
        messages.append(
            Message(
                role=m["role"],
                content=[load_request_block(b) for b in m["content"]],
            )
        )

    tools: list[Tool] | None = None
    if "tools" in raw:
        tools = []
        for t in raw["tools"]:
            tool_native = tuple(load_native_annotation(a) for a in t.get("native", []))
            tool_type = t.get("type", "function")
            if "strict" in t and (tool_type != "function" or not isinstance(t["strict"], bool)):
                raise CodecError("strict is only allowed as a boolean on function tools")
            if tool_type == "apply_patch":
                if any(k in t for k in ("name", "description", "input_schema", "format", "source", "definition")):
                    raise CodecError("apply_patch declaration must only contain type")
                tools.append(Tool(type="apply_patch", native=tool_native))
            elif tool_type == "native":
                if any(k in t for k in ("description", "input_schema", "format", "native")):
                    raise CodecError("native tool must only contain type, name, source, definition")
                source = t.get("source")
                definition = t.get("definition")
                name = t.get("name")
                if not isinstance(source, str) or not source or not isinstance(definition, str) or not definition or not isinstance(name, str) or not name:
                    raise CodecError("native tool requires non-empty name, source, and definition")
                try:
                    def_parsed = json.loads(definition)
                except Exception as e:
                    raise CodecError("native tool definition must be a valid JSON object") from e
                if not isinstance(def_parsed, dict) or not isinstance(def_parsed.get("type"), str) or not def_parsed["type"] or def_parsed["type"] in ("function", "custom"):
                    raise CodecError("native tool definition requires a native type")
                expected_name = (
                    def_parsed.get("name")
                    if isinstance(def_parsed.get("name"), str) and def_parsed.get("name")
                    else def_parsed.get("server_label")
                    if isinstance(def_parsed.get("server_label"), str) and def_parsed.get("server_label")
                    else def_parsed.get("type")
                )
                if expected_name != name:
                    raise CodecError("native tool definition name must match tool name")
                tools.append(Tool(type="native", name=name, source=source, definition=definition))
            elif tool_type == "custom":
                if "input_schema" in t or "source" in t or "definition" in t:
                    raise CodecError("custom tool must not carry input_schema/source/definition")
                fmt = t.get("format")
                tools.append(Tool(name=t["name"], description=t.get("description"), type="custom", format=load_custom_tool_format(fmt) if fmt is not None else None, native=tool_native))
            elif tool_type == "function":
                if "format" in t or "source" in t or "definition" in t:
                    raise CodecError("function tool must not carry format/source/definition")
                tools.append(
                    Tool(
                        name=t["name"],
                        input_schema=t["input_schema"],
                        description=t.get("description"),
                        native=tool_native,
                        strict=t.get("strict"),
                    )
                )
            else:
                raise CodecError(f"unknown tool type: {tool_type!r}")

    tool_choice: ToolChoice | None = None
    if "tool_choice" in raw:
        tc = raw["tool_choice"]
        mode, tool_type = tc["mode"], tc.get("tool_type", "function")
        if mode == TOOL_CHOICE_TOOL:
            if tool_type == "apply_patch":
                if "name" in tc:
                    raise CodecError("apply_patch choice must not contain name")
                tool_choice = ToolChoice(mode=mode, tool_type="apply_patch")
            else:
                if tool_type not in ("function", "custom", "native") or not tc.get("name"):
                    raise CodecError("tool choice mode 'tool' requires name and function/custom/native tool_type")
                tool_choice = ToolChoice(mode=mode, name=tc.get("name"), tool_type=tool_type)
        elif "tool_type" in tc:
            raise CodecError("tool_choice.tool_type is only valid when mode is tool")
        else:
            tool_choice = ToolChoice(mode=mode)

    params: Params | None = None
    if "params" in raw:
        p = raw["params"]
        effort = p.get("reasoning_effort")
        if effort is not None and effort not in ("minimal", "low", "medium", "high", "xhigh", "max"):
            raise CodecError(f"invalid params.reasoning_effort value {effort!r}")
        if "verbosity" in p and p["verbosity"] not in ("low", "medium", "high"):
            raise CodecError("invalid params.verbosity")
        rf_data = p.get("response_format")
        response_format = load_response_format(rf_data) if rf_data is not None else None
        if isinstance(data, str) and isinstance(response_format, ResponseFormatJsonSchema):
            from dataclasses import replace
            from oxa._wire import raw_field

            response_format = replace(response_format, schema_raw=raw_field(data, "params.response_format.schema"))
        params = Params(
            temperature=p.get("temperature"),
            top_p=p.get("top_p"),
            max_tokens=p.get("max_tokens"),
            stop_sequences=p.get("stop_sequences"),
            reasoning_effort=effort,
            verbosity=p.get("verbosity"),
            response_format=response_format,
        )

    metadata = raw.get("metadata")
    if metadata is not None:
        if not isinstance(metadata, dict):
            raise CodecError("metadata must be an object")
        for k, v in metadata.items():
            if not k:
                raise CodecError("metadata key cannot be empty")
            if not isinstance(v, str):
                raise CodecError(f"metadata.{k} must be a string")

    return Request(
        model=raw["model"],
        messages=messages,
        system=system,
        tools=tools,
        tool_choice=tool_choice,
        params=params,
        metadata=metadata,
        native=req_native,
    )


# ---- Response --------------------------------------------------------------


def dump_response(resp: Response) -> dict[str, Any]:
    out: dict[str, Any] = {
        "specVersion": SPEC_VERSION,
        "id": resp.id,
        "model": resp.model,
        "content": [dump_block(b) for b in resp.content],
        "stop_reason": resp.stop_reason,
        "usage": _dump_usage(resp.usage),
    }
    if resp.stop_reason == STOP_STOP_SEQUENCE:
        if resp.stop_sequence:
            out["stop_sequence"] = resp.stop_sequence
    elif resp.stop_sequence is not None:
        raise CodecError("stop_sequence is only permitted when stop_reason is stop_sequence")
    return out


def load_response(data: dict[str, Any] | str) -> Response:
    raw: dict[str, Any] = json.loads(data) if isinstance(data, str) else data
    version = raw.get("specVersion")
    if version not in SUPPORTED_SPEC_VERSIONS:
        raise CodecError(
            f"unsupported specVersion {version!r}, supported versions are {SUPPORTED_SPEC_VERSIONS!r}"
        )

    content = [load_block(b) for b in raw.get("content", [])]
    usage_data = raw.get("usage", {})
    usage = _load_usage(usage_data)
    return Response(
        id=raw["id"],
        model=raw["model"],
        content=content,
        stop_reason=raw["stop_reason"],
        usage=usage,
        stop_sequence=raw.get("stop_sequence"),
    )


# ---- EventStream -----------------------------------------------------------


def dump_event_stream(stream: EventStream) -> dict[str, Any]:
    return {
        "specVersion": SPEC_VERSION,
        "events": [dump_event(e) for e in stream.events],
    }


def load_event_stream(data: dict[str, Any] | list[dict[str, Any]] | str) -> EventStream:
    parsed: Any = json.loads(data) if isinstance(data, str) else data
    if isinstance(parsed, list):
        # Bare array of events without document envelope
        return EventStream(events=[load_event(e) for e in parsed])
    raw: dict[str, Any] = parsed
    version = raw.get("specVersion")
    if version not in SUPPORTED_SPEC_VERSIONS:
        raise CodecError(
            f"unsupported specVersion {version!r}, supported versions are {SUPPORTED_SPEC_VERSIONS!r}"
        )
    return EventStream(events=[load_event(e) for e in raw.get("events", [])])


# ---- Loss ------------------------------------------------------------------


def dump_loss(loss: Loss) -> dict[str, Any]:
    return loss.to_dict()


def load_loss(data: dict[str, Any]) -> Loss:
    return Loss.from_dict(data)
