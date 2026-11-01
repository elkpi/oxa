"""Native request options preservation and omission loss calculation (spec/15)."""

from __future__ import annotations

import enum
import json
from dataclasses import dataclass
from typing import Any, Sequence

from oxa.ir.constants import (
    BLOCK_TYPE_DOCUMENT,
    BLOCK_TYPE_REDACTED_THINKING,
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    ROLE_USER,
    ReasoningEffort,
)
from oxa.ir.loss import Loss

NATIVE_SOURCE_ANTHROPIC = "anthropic"
NATIVE_SOURCE_OPENAI = "openai"
NATIVE_KIND_THINKING_MODE = "thinking_mode"
NATIVE_KIND_THINKING_BUDGET = "thinking_budget"
NATIVE_KIND_THINKING_DISPLAY = "thinking_display"
NATIVE_KIND_OUTPUT_EFFORT = "output_effort"
NATIVE_KIND_CACHE_CONTROL = "cache_control"
NATIVE_KIND_CONTEXT_MANAGEMENT = "context_management"
NATIVE_KIND_DEFER_LOADING = "defer_loading"
NATIVE_KIND_DOCUMENT_TITLE = "document_title"
NATIVE_KIND_DOCUMENT_CONTEXT = "document_context"
NATIVE_KIND_DOCUMENT_CITATIONS = "document_citations"
NATIVE_KIND_CONTAINER = "container"
NATIVE_KIND_EAGER_INPUT_STREAMING = "eager_input_streaming"
NATIVE_KIND_STORE = "store"
NATIVE_KIND_STREAM = "stream"
NATIVE_KIND_STREAM_OPTIONS = "stream_options"
NATIVE_KIND_MAX_COMPLETION_TOKENS = "max_completion_tokens"
NATIVE_KIND_TOP_K = "top_k"
NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE = "disable_parallel_tool_use"
NATIVE_KIND_PROMPT_CACHE_KEY = "prompt_cache_key"
NATIVE_KIND_PROMPT_CACHE_OPTIONS = "prompt_cache_options"
NATIVE_KIND_PROMPT_CACHE_BREAKPOINT = "prompt_cache_breakpoint"

DEFER_LOADING_LOSS_DETAIL = (
    "deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search"
)
EAGER_INPUT_STREAMING_LOSS_DETAIL = "target does not preserve eager tool input streaming semantics"
STREAM_OPTIONS_LOSS_DETAIL = "target does not emit the requested final usage chunk"
PROMPT_CACHE_KEY_LOSS_DETAIL = (
    "Anthropic Messages has no prompt_cache_key parameter; prompt cache key is dropped"
)
PROMPT_CACHE_OPTIONS_LOSS_DETAIL = (
    "Anthropic Messages has no prompt_cache_options parameter; prompt cache options are dropped"
)
PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL = (
    "Anthropic Messages has no prompt_cache_breakpoint parameter; prompt cache breakpoint is dropped"
)

CACHE_CONTROL_LOSS_DETAIL = "anthropic-native cache breakpoint; the target face has no cache_control representation"
DOCUMENT_TITLE_LOSS_DETAIL = (
    "anthropic-native document title; the target face has no document representation"
)
DOCUMENT_CONTEXT_LOSS_DETAIL = (
    "anthropic-native document context; the target face has no document representation"
)
DOCUMENT_CITATIONS_LOSS_DETAIL = (
    "anthropic-native document citations; citations alter response semantics and the target face has no document representation"
)


@dataclass(frozen=True)
class LossFingerprint:
    path: str
    field: str
    reason: str


def fingerprint_losses(losses: Sequence[Loss], limit: int = 0) -> list[LossFingerprint]:
    n = limit if 0 < limit < len(losses) else len(losses)
    return [
        LossFingerprint(path=l.path, field=l.field, reason=l.reason)
        for l in losses[:n]
    ]


class CodecError(ValueError):
    """Raised when an IR document violates structural schema requirements."""


@dataclass(frozen=True, slots=True)
class NativeOmission:
    field: str
    reason: str
    detail: str | None = None


@dataclass(frozen=True, slots=True)
class NativeProjection:
    origin: str  # "budget" | "explicit"
    effort: ReasoningEffort


@dataclass(frozen=True, slots=True)
class NativeAnnotation:
    source: str
    kind: str
    value: str  # opaque JSON text
    omission: NativeOmission | None = None
    projection: NativeProjection | None = None


class HostType(enum.Enum):
    REQUEST = "request"
    SYSTEM = "system"
    CONTENT = "content"
    TOOL = "tool"


def validate_native_request(req: Any) -> None:
    """Validates native options constraints across the entire request hierarchy."""
    validate_annotations(getattr(req, "native", ()), HostType.REQUEST, "request")

    budget_proj = 0
    explicit_proj = 0
    for n in getattr(req, "native", ()):
        if n.projection is not None:
            orig = n.projection.origin
            if orig not in ("budget", "explicit"):
                raise CodecError(f'ir: native projection origin "{orig}" invalid')
            eff = n.projection.effort
            if eff not in ("minimal", "low", "medium", "high", "xhigh", "max"):
                raise CodecError(f'ir: native projection effort "{eff}" invalid')
            if orig == "budget":
                if n.kind != NATIVE_KIND_THINKING_BUDGET:
                    raise CodecError("ir: budget projection is only allowed on thinking_budget")
                budget_proj += 1
                if budget_proj > 1:
                    raise CodecError("ir: duplicate budget projection on request")
            elif orig == "explicit":
                if n.kind != NATIVE_KIND_OUTPUT_EFFORT:
                    raise CodecError("ir: explicit projection is only allowed on output_effort")
                explicit_proj += 1
                if explicit_proj > 1:
                    raise CodecError("ir: duplicate explicit projection on request")

    if budget_proj > 0 and explicit_proj > 0:
        raise CodecError("ir: conflicting effort projections on request")

    has_between_tools = False
    has_budget_or_display = False
    has_disable_parallel = False
    for n in getattr(req, "native", ()) or ():
        if n.source == NATIVE_SOURCE_ANTHROPIC:
            if n.kind == NATIVE_KIND_THINKING_MODE and n.value == '"between_tools"':
                has_between_tools = True
            elif n.kind in (NATIVE_KIND_THINKING_BUDGET, NATIVE_KIND_THINKING_DISPLAY):
                has_budget_or_display = True
            elif n.kind == NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE:
                has_disable_parallel = True

    if has_between_tools and has_budget_or_display:
        raise CodecError("anthropic: cannot specify budget_tokens or display with thinking.type between_tools")
    if has_disable_parallel:
        tc = getattr(req, "tool_choice", None)
        if tc is None or tc.mode == "none":
            raise CodecError("anthropic: cannot specify disable_parallel_tool_use when tool_choice is none or not specified")

    for i, sb in enumerate(getattr(req, "system", ()) or ()):
        validate_annotations(getattr(sb, "native", ()), HostType.SYSTEM, f"system[{i}]")

    for m_idx, msg in enumerate(getattr(req, "messages", ()) or ()):
        for c_idx, blk in enumerate(getattr(msg, "content", ()) or ()):
            validate_block_native(blk, HostType.CONTENT, f"messages[{m_idx}].content[{c_idx}]")

    for t_idx, tool in enumerate(getattr(req, "tools", ()) or ()):
        validate_annotations(getattr(tool, "native", ()), HostType.TOOL, f"tools[{t_idx}]")
        has_cache_control = False
        has_defer_loading = False
        for n in getattr(tool, "native", ()) or ():
            if n.source == NATIVE_SOURCE_ANTHROPIC:
                if n.kind == NATIVE_KIND_CACHE_CONTROL:
                    has_cache_control = True
                elif n.kind == NATIVE_KIND_DEFER_LOADING:
                    has_defer_loading = True
        if has_cache_control and has_defer_loading:
            raise CodecError(f"ir: tools[{t_idx}]: cannot specify both defer_loading and cache_control")


def validate_annotations(
    annotations: Sequence[NativeAnnotation],
    host: HostType,
    path: str,
) -> None:
    seen: set[str] = set()
    for i, ann in enumerate(annotations):
        if not ann.source:
            raise CodecError(f"{path}.native[{i}]: source cannot be empty")
        if not ann.kind:
            raise CodecError(f"{path}.native[{i}]: kind cannot be empty")
        try:
            parsed_json = json.loads(ann.value)
        except Exception:
            raise CodecError(f"{path}.native[{i}]: value is not valid JSON text")
        if ann.omission is not None:
            if not ann.omission.field:
                raise CodecError(f"{path}.native[{i}]: omission field cannot be empty")
            if ann.omission.reason not in (
                "unmapped-field",
                "unmapped-value",
                "unsupported-semantic",
                "degraded",
            ):
                raise CodecError(f"{path}.native[{i}]: invalid omission reason")

        key = f"{ann.source}:{ann.kind}"
        if key in seen:
            raise CodecError(f"{path}: duplicate native annotation for {ann.source}:{ann.kind}")
        seen.add(key)

        if ann.source == NATIVE_SOURCE_ANTHROPIC:
            if ann.kind == NATIVE_KIND_THINKING_MODE:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if parsed_json not in ("adaptive", "enabled", "disabled", "between_tools"):
                    raise CodecError(f"invalid thinking_mode {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_TOP_K:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if not (isinstance(parsed_json, int) and not isinstance(parsed_json, bool) and 0 <= parsed_json <= 9007199254740991):
                    raise CodecError(f"invalid top_k {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if not isinstance(parsed_json, bool):
                    raise CodecError(f"invalid disable_parallel_tool_use {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_THINKING_BUDGET:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if not (isinstance(parsed_json, int) and parsed_json > 0):
                    raise CodecError("anthropic thinking_budget must be a positive integer")
            elif ann.kind == NATIVE_KIND_THINKING_DISPLAY:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if parsed_json not in ("summarized", "omitted", "updates"):
                    raise CodecError(f"invalid thinking_display {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_OUTPUT_EFFORT:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if parsed_json not in ("low", "medium", "high", "xhigh", "max"):
                    raise CodecError(f"invalid output_effort {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_CONTEXT_MANAGEMENT:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Request")
                if not isinstance(parsed_json, dict) or not isinstance(parsed_json.get("edits"), list):
                    raise CodecError("context_management requires edits array")
            elif ann.kind == NATIVE_KIND_CACHE_CONTROL:
                if not isinstance(parsed_json, dict) or parsed_json.get("type") != "ephemeral":
                    raise CodecError("invalid cache_control object")
                if "ttl" in parsed_json and parsed_json["ttl"] not in ("5m", "1h"):
                    raise CodecError(f'invalid cache_control ttl {parsed_json["ttl"]!r}')
            elif ann.kind == NATIVE_KIND_DEFER_LOADING:
                if host != HostType.TOOL:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Tool")
                if parsed_json is not True:
                    raise CodecError(f"invalid defer_loading {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_EAGER_INPUT_STREAMING:
                if host != HostType.TOOL:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on Tool")
                if not isinstance(parsed_json, bool):
                    raise CodecError(f"invalid eager_input_streaming {parsed_json!r}")
            elif ann.kind in (NATIVE_KIND_DOCUMENT_TITLE, NATIVE_KIND_DOCUMENT_CONTEXT):
                if host != HostType.CONTENT:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on content blocks")
                if not isinstance(parsed_json, str):
                    raise CodecError(f"anthropic {ann.kind} value must be a JSON string")
            elif ann.kind == NATIVE_KIND_DOCUMENT_CITATIONS:
                if host != HostType.CONTENT:
                    raise CodecError(f"{path}: anthropic {ann.kind} is only allowed on content blocks")
                if not isinstance(parsed_json, dict):
                    raise CodecError(f"anthropic {ann.kind} value must be a JSON object")
                if "enabled" in parsed_json and not isinstance(parsed_json["enabled"], bool):
                    raise CodecError(f"anthropic {ann.kind} enabled must be a boolean")
        elif ann.source == NATIVE_SOURCE_OPENAI:
            if ann.kind == NATIVE_KIND_MAX_COMPLETION_TOKENS:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if parsed_json is not True:
                    raise CodecError(f"invalid max_completion_tokens {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_STORE:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if not isinstance(parsed_json, bool) and parsed_json is not None:
                    raise CodecError(f"invalid store {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_STREAM:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if not isinstance(parsed_json, bool) and parsed_json is not None:
                    raise CodecError(f"invalid stream {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_STREAM_OPTIONS:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if parsed_json is not None:
                    if not isinstance(parsed_json, dict):
                        raise CodecError(f"invalid stream_options {parsed_json!r}")
                    for k, v in parsed_json.items():
                        if k in ("include_usage", "include_obfuscation") and not isinstance(v, bool):
                            raise CodecError(f"invalid stream_options {parsed_json!r}: {k} must be a boolean")
            elif ann.kind == NATIVE_KIND_CONTAINER:
                if host != HostType.TOOL:
                    raise CodecError(f"{path}: openai container is only allowed on Tool")
                if not isinstance(parsed_json, (dict, str)):
                    raise CodecError(f"{path}: openai container must be an object with path array or string")
            elif ann.kind == NATIVE_KIND_DEFER_LOADING:
                if host != HostType.TOOL:
                    raise CodecError(f"{path}: openai defer_loading is only allowed on Tool")
                if parsed_json is not True:
                    raise CodecError(f"invalid defer_loading {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_PROMPT_CACHE_KEY:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if not isinstance(parsed_json, str):
                    raise CodecError(f"invalid prompt_cache_key {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_PROMPT_CACHE_OPTIONS:
                if host != HostType.REQUEST:
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on Request")
                if parsed_json is not None and not isinstance(parsed_json, dict):
                    raise CodecError(f"invalid prompt_cache_options {parsed_json!r}")
            elif ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT:
                if host not in (HostType.CONTENT, HostType.SYSTEM):
                    raise CodecError(f"{path}: openai {ann.kind} is only allowed on content or system blocks")
                if not isinstance(parsed_json, dict):
                    raise CodecError(f"invalid prompt_cache_breakpoint {parsed_json!r}")


_BREAKPOINT_ALLOWED_BLOCK_TYPES = ("text", "image", "document")


def validate_block_native(blk: Any, host: HostType, path: str) -> None:
    native = getattr(blk, "native", ())
    validate_annotations(native, host, path)
    blk_type = getattr(blk, "type", None)
    if blk_type is not None and blk_type not in _BREAKPOINT_ALLOWED_BLOCK_TYPES:
        for i, ann in enumerate(native):
            if (
                ann.source == NATIVE_SOURCE_OPENAI
                and ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT
            ):
                raise CodecError(
                    f"ir: {path}.native[{i}]: openai prompt_cache_breakpoint is only allowed on text, image, or document blocks"
                )

    # Check child blocks for ToolResultBlock and CustomToolResultBlock
    content = getattr(blk, "content", None)
    if isinstance(content, list):
        for k, child in enumerate(content):
            validate_block_native(child, host, f"{path}.content[{k}]")

    output = getattr(blk, "output", None)
    if isinstance(output, list):
        for k, child in enumerate(output):
            validate_block_native(child, host, f"{path}.output.content[{k}]")


def omitted_native_request_losses(req: Any, target: str) -> list[Loss]:
    """Computes losses for native request annotations that the target face cannot represent."""
    validate_native_request(req)

    losses: list[Loss] = []
    current_effort = req.params.reasoning_effort if req.params else None

    has_active_budget_proj = any(
        n.source == NATIVE_SOURCE_ANTHROPIC
        and n.kind == NATIVE_KIND_THINKING_BUDGET
        and n.projection is not None
        and n.projection.origin == "budget"
        and n.projection.effort == current_effort
        for n in getattr(req, "native", ())
    )

    for i, ann in enumerate(getattr(req, "native", ())):
        _append_omission_loss(
            losses,
            ann,
            f"native[{i}]",
            has_active_budget_proj,
            current_effort,
            target,
        )

    for s_idx, sb in enumerate(getattr(req, "system", ()) or ()):
        for i, ann in enumerate(getattr(sb, "native", ())):
            _append_omission_loss(
                losses,
                ann,
                f"system[{s_idx}].native[{i}]",
                False,
                None,
                target,
            )

    for m_idx, msg in enumerate(getattr(req, "messages", ()) or ()):
        role = getattr(msg, "role", "")
        for c_idx, blk in enumerate(getattr(msg, "content", ()) or ()):
            if getattr(blk, "type", "") == BLOCK_TYPE_DOCUMENT and role != ROLE_USER:
                continue
            _collect_block_omissions(
                losses,
                blk,
                f"messages[{m_idx}].content[{c_idx}]",
                target,
            )

    for t_idx, tool in enumerate(getattr(req, "tools", ()) or ()):
        for i, ann in enumerate(getattr(tool, "native", ())):
            _append_omission_loss(
                losses,
                ann,
                f"tools[{t_idx}].native[{i}]",
                False,
                None,
                target,
            )

    return losses


def _collect_block_omissions(losses: list[Loss], blk: Any, path: str, target: str) -> None:
    blk_type = getattr(blk, "type", "")
    if blk_type == BLOCK_TYPE_REDACTED_THINKING or (
        blk_type == BLOCK_TYPE_DOCUMENT
        and target == "chatcompletions"
        and (
            getattr(blk, "url", None)
            or (getattr(blk, "media_type", None) and getattr(blk, "media_type", None) != "application/pdf")
        )
    ):
        # whole-block drop; annotations are part of it and are not reported a second time.
        return
    for i, ann in enumerate(getattr(blk, "native", ())):
        _append_omission_loss(losses, ann, f"{path}.native[{i}]", False, None, target)

    content = getattr(blk, "content", None)
    if isinstance(content, list):
        for k, child in enumerate(content):
            if getattr(child, "type", "") == BLOCK_TYPE_DOCUMENT:
                continue
            _collect_block_omissions(losses, child, f"{path}.content[{k}]", target)

    output = getattr(blk, "output", None)
    if isinstance(output, list):
        for k, child in enumerate(output):
            if getattr(child, "type", "") == BLOCK_TYPE_DOCUMENT:
                continue
            _collect_block_omissions(losses, child, f"{path}.output.content[{k}]", target)


def _append_omission_loss(
    losses: list[Loss],
    ann: NativeAnnotation,
    path: str,
    has_active_budget_proj: bool,
    current_effort: ReasoningEffort | None,
    target: str,
) -> None:
    if ann.source == NATIVE_SOURCE_OPENAI:
        if ann.kind in (NATIVE_KIND_MAX_COMPLETION_TOKENS, NATIVE_KIND_STREAM):
            return
        if ann.kind == NATIVE_KIND_STREAM_OPTIONS:
            if target == "chatcompletions":
                return
            has_usage_true = False
            try:
                so = json.loads(ann.value)
                if isinstance(so, dict) and so.get("include_usage") is True:
                    has_usage_true = True
            except Exception:
                pass
            if has_usage_true:
                field = ann.omission.field if ann.omission and ann.omission.field else "stream_options"
                reason = ann.omission.reason if ann.omission and ann.omission.reason else "degraded"
                detail = ann.omission.detail if ann.omission and ann.omission.detail else STREAM_OPTIONS_LOSS_DETAIL
                losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
            return
        if ann.kind == NATIVE_KIND_STORE:
            if target in ("chatcompletions", "responses"):
                return
            is_true = False
            try:
                is_true = json.loads(ann.value) is True
            except Exception:
                pass
            if is_true:
                field = ann.omission.field if ann.omission and ann.omission.field else "store"
                reason = ann.omission.reason if ann.omission and ann.omission.reason else "unsupported-semantic"
                detail = ann.omission.detail if ann.omission and ann.omission.detail else "target does not support output retention"
                losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
            return
        if ann.kind == NATIVE_KIND_PROMPT_CACHE_KEY:
            if target in ("chatcompletions", "responses"):
                return
            field = ann.omission.field if ann.omission and ann.omission.field else "prompt_cache_key"
            reason = ann.omission.reason if ann.omission and ann.omission.reason else "unmapped-field"
            detail = ann.omission.detail if ann.omission and ann.omission.detail else PROMPT_CACHE_KEY_LOSS_DETAIL
            losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
            return
        if ann.kind == NATIVE_KIND_PROMPT_CACHE_OPTIONS:
            if target in ("chatcompletions", "responses"):
                return
            field = ann.omission.field if ann.omission and ann.omission.field else "prompt_cache_options"
            reason = ann.omission.reason if ann.omission and ann.omission.reason else "unmapped-field"
            detail = ann.omission.detail if ann.omission and ann.omission.detail else PROMPT_CACHE_OPTIONS_LOSS_DETAIL
            losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
            return
        if ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT:
            if target in ("chatcompletions", "responses"):
                return
            field = ann.omission.field if ann.omission and ann.omission.field else "prompt_cache_breakpoint"
            reason = ann.omission.reason if ann.omission and ann.omission.reason else "unmapped-field"
            detail = ann.omission.detail if ann.omission and ann.omission.detail else PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL
            losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
            return
        if target == "responses":
            return
        if target == "anthropic" and ann.kind == NATIVE_KIND_DEFER_LOADING:
            return
        if ann.omission is not None:
            losses.append(
                Loss(
                    path=path,
                    field=ann.omission.field,
                    reason=ann.omission.reason,
                    detail=ann.omission.detail or "",
                )
            )
        return
    if ann.source != NATIVE_SOURCE_ANTHROPIC:
        losses.append(
            Loss(
                path=path,
                field="source",
                reason="unsupported-semantic",
                detail=f'unsupported native option source "{ann.source}"',
            )
        )
        return

    if ann.kind == NATIVE_KIND_OUTPUT_EFFORT:
        return

    if ann.kind == NATIVE_KIND_THINKING_MODE:
        if has_active_budget_proj:
            return
        if ann.omission is not None:
            losses.append(
                Loss(
                    path=path,
                    field=ann.omission.field,
                    reason=ann.omission.reason,
                    detail=ann.omission.detail or "",
                )
            )
        else:
            losses.append(
                Loss(
                    path=path,
                    field="type",
                    reason="unmapped-field",
                )
            )
        return

    if ann.kind == NATIVE_KIND_THINKING_BUDGET:
        if ann.projection is not None and ann.projection.origin == "budget":
            if ann.projection.effort == current_effort and ann.omission is not None:
                losses.append(
                    Loss(
                        path=path,
                        field=ann.omission.field,
                        reason=ann.omission.reason,
                        detail=ann.omission.detail or "",
                    )
                )
            return
        if ann.omission is not None:
            losses.append(
                Loss(
                    path=path,
                    field=ann.omission.field,
                    reason=ann.omission.reason,
                    detail=ann.omission.detail or "",
                )
            )
        return

    if ann.kind == NATIVE_KIND_CACHE_CONTROL:
        field = "cache_control"
        reason = LOSS_UNMAPPED_FIELD
        detail = CACHE_CONTROL_LOSS_DETAIL
        if ann.omission is not None:
            if ann.omission.field:
                field = ann.omission.field
            if ann.omission.reason:
                reason = ann.omission.reason
            if ann.omission.detail:
                detail = ann.omission.detail
        losses.append(
            Loss(
                path=path,
                field=field,
                reason=reason,
                detail=detail,
            )
        )
        return

    if ann.kind == NATIVE_KIND_DEFER_LOADING:
        if target in ("responses", "anthropic"):
            return
        field = "defer_loading"
        reason = LOSS_DEGRADED
        detail = DEFER_LOADING_LOSS_DETAIL
        if ann.omission is not None:
            if ann.omission.field:
                field = ann.omission.field
            if ann.omission.reason:
                reason = ann.omission.reason
            if ann.omission.detail:
                detail = ann.omission.detail
        losses.append(
            Loss(
                path=path,
                field=field,
                reason=reason,
                detail=detail,
            )
        )
        return

    if ann.kind == NATIVE_KIND_EAGER_INPUT_STREAMING:
        if target == "anthropic":
            return
        is_true = False
        try:
            is_true = json.loads(ann.value) is True
        except Exception:
            pass
        if is_true:
            field = "eager_input_streaming"
            reason = LOSS_DEGRADED
            detail = EAGER_INPUT_STREAMING_LOSS_DETAIL
            if ann.omission is not None:
                if ann.omission.field:
                    field = ann.omission.field
                if ann.omission.reason:
                    reason = ann.omission.reason
                if ann.omission.detail:
                    detail = ann.omission.detail
            losses.append(
                Loss(
                    path=path,
                    field=field,
                    reason=reason,
                    detail=detail,
                )
            )
        return

    if ann.kind == NATIVE_KIND_TOP_K:
        field = "top_k"
        reason = LOSS_UNMAPPED_FIELD
        detail = ""
        if ann.omission is not None:
            if ann.omission.field:
                field = ann.omission.field
            if ann.omission.reason:
                reason = ann.omission.reason
            if ann.omission.detail:
                detail = ann.omission.detail
        losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
        return

    if ann.kind == NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE:
        field = "disable_parallel_tool_use"
        reason = LOSS_UNMAPPED_FIELD
        detail = ""
        if ann.omission is not None:
            if ann.omission.field:
                field = ann.omission.field
            if ann.omission.reason:
                reason = ann.omission.reason
            if ann.omission.detail:
                detail = ann.omission.detail
        losses.append(Loss(path=path, field=field, reason=reason, detail=detail))
        return

    if ann.kind in (
        NATIVE_KIND_THINKING_DISPLAY,
        NATIVE_KIND_CONTEXT_MANAGEMENT,
        NATIVE_KIND_DOCUMENT_TITLE,
        NATIVE_KIND_DOCUMENT_CONTEXT,
        NATIVE_KIND_DOCUMENT_CITATIONS,
    ):
        if ann.omission is not None:
            losses.append(
                Loss(
                    path=path,
                    field=ann.omission.field,
                    reason=ann.omission.reason,
                    detail=ann.omission.detail or "",
                )
            )
        return

    losses.append(
        Loss(
            path=path,
            field="kind",
            reason="unsupported-semantic",
            detail=f'unsupported native option kind "{ann.kind}"',
        )
    )
