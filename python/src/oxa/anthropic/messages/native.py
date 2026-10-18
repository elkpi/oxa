"""Anthropic Messages native options extraction and restoration (spec/15)."""

from __future__ import annotations

import dataclasses
import json
from typing import Any

from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    Loss,
    ReasoningEffort,
    Request,
)
from oxa.ir.native import (
    NATIVE_KIND_CACHE_CONTROL,
    NATIVE_KIND_CONTEXT_MANAGEMENT,
    NATIVE_KIND_OUTPUT_EFFORT,
    NATIVE_KIND_THINKING_BUDGET,
    NATIVE_KIND_THINKING_DISPLAY,
    NATIVE_KIND_THINKING_MODE,
    NATIVE_SOURCE_ANTHROPIC,
    NativeAnnotation,
    NativeOmission,
    NativeProjection,
)


def parse_cache_control(cc: Any, path: str) -> tuple[str | None, Loss | None]:
    if not isinstance(cc, dict):
        return None, None
    kind = cc.get("type")
    if kind != "ephemeral":
        return (
            None,
            Loss(
                path=f"{path}.type",
                field="type",
                reason=LOSS_UNMAPPED_VALUE,
                detail=f'unknown cache_control.type "{kind}"',
            ),
        )

    ttl = cc.get("ttl")
    if ttl is not None:
        if ttl not in ("5m", "1h"):
            return (
                None,
                Loss(
                    path=f"{path}.ttl",
                    field="ttl",
                    reason=LOSS_UNMAPPED_VALUE,
                    detail=f'unknown cache_control.ttl "{ttl}"',
                ),
            )
        return json.dumps({"ttl": ttl, "type": "ephemeral"}, separators=(",", ":"), sort_keys=True), None

    return json.dumps({"type": "ephemeral"}, separators=(",", ":"), sort_keys=True), None


def parse_context_management(cm: Any, path: str) -> tuple[str | None, list[Loss]]:
    if not isinstance(cm, dict):
        return None, []
    edits = cm.get("edits")
    if not isinstance(edits, list) or not edits:
        return None, []

    losses: list[Loss] = []
    valid_edits: list[dict[str, Any]] = []

    for i, edit in enumerate(edits):
        if not isinstance(edit, dict):
            continue
        p = f"{path}.edits[{i}]"
        kind = edit.get("type")

        if kind == "clear_thinking_20251015":
            valid = True
            if "keep" in edit:
                keep = edit["keep"]
                if isinstance(keep, str):
                    if keep != "all":
                        losses.append(
                            Loss(
                                path=f"{p}.keep",
                                field="keep",
                                reason=LOSS_UNMAPPED_VALUE,
                            )
                        )
                        valid = False
                elif isinstance(keep, dict):
                    if keep.get("type") != "thinking_turns":
                        losses.append(
                            Loss(
                                path=f"{p}.keep",
                                field="keep",
                                reason=LOSS_UNMAPPED_VALUE,
                            )
                        )
                        valid = False
                else:
                    losses.append(
                        Loss(
                            path=f"{p}.keep",
                            field="keep",
                            reason=LOSS_UNMAPPED_VALUE,
                        )
                    )
                    valid = False
            if valid:
                valid_edits.append(edit)

        elif kind == "clear_tool_uses_20250919":
            valid = True
            if "trigger" in edit:
                trigger = edit["trigger"]
                if isinstance(trigger, dict):
                    if trigger.get("type") != "input_tokens":
                        losses.append(
                            Loss(
                                path=f"{p}.trigger",
                                field="trigger",
                                reason=LOSS_UNMAPPED_VALUE,
                            )
                        )
                        valid = False
            if "keep" in edit:
                keep = edit["keep"]
                if isinstance(keep, dict):
                    if keep.get("type") != "tool_uses":
                        losses.append(
                            Loss(
                                path=f"{p}.keep",
                                field="keep",
                                reason=LOSS_UNMAPPED_VALUE,
                            )
                        )
                        valid = False
            if valid:
                valid_edits.append(edit)

        else:
            losses.append(
                Loss(
                    path=p,
                    field="type",
                    reason=LOSS_UNSUPPORTED_SEMANTIC,
                    detail=f'unsupported context edit type "{kind}"',
                )
            )

    if not valid_edits:
        return None, losses

    return json.dumps({"edits": valid_edits}, separators=(",", ":"), sort_keys=True), losses


def decode_request_native(wire: dict[str, Any], req: Request) -> tuple[Request, list[Loss]]:
    losses: list[Loss] = []
    req_native: list[NativeAnnotation] = list(req.native)

    # 1. Thinking
    thinking = wire.get("thinking")
    if isinstance(thinking, dict):
        kind = thinking.get("type")
        if kind in ("adaptive", "disabled"):
            req_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value=json.dumps(kind),
                    omission=NativeOmission(field="type", reason=LOSS_UNMAPPED_FIELD),
                )
            )
        elif kind == "enabled":
            has_explicit_effort = (
                isinstance(wire.get("output_config"), dict)
                and wire["output_config"].get("effort") is not None
            )
            omission = (
                NativeOmission(field="type", reason=LOSS_UNMAPPED_FIELD)
                if has_explicit_effort
                else None
            )
            req_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value=json.dumps("enabled"),
                    omission=omission,
                )
            )
            budget = thinking.get("budget_tokens")
            if isinstance(budget, int) and budget > 0:
                if has_explicit_effort:
                    req_native.append(
                        NativeAnnotation(
                            source=NATIVE_SOURCE_ANTHROPIC,
                            kind=NATIVE_KIND_THINKING_BUDGET,
                            value=str(budget),
                            omission=NativeOmission(field="budget_tokens", reason=LOSS_UNMAPPED_FIELD),
                        )
                    )
                else:
                    derived: ReasoningEffort = (
                        "low" if budget <= 2048 else ("medium" if budget <= 8192 else "high")
                    )
                    if req.params is not None:
                        req = dataclasses.replace(
                            req, params=dataclasses.replace(req.params, reasoning_effort=derived)
                        )
                    req_native.append(
                        NativeAnnotation(
                            source=NATIVE_SOURCE_ANTHROPIC,
                            kind=NATIVE_KIND_THINKING_BUDGET,
                            value=str(budget),
                            omission=NativeOmission(
                                field="budget_tokens",
                                reason=LOSS_DEGRADED,
                                detail="budget approximated",
                            ),
                            projection=NativeProjection(origin="budget", effort=derived),
                        )
                    )
        else:
            losses.append(
                Loss(
                    path="thinking.type",
                    field="type",
                    reason=LOSS_UNMAPPED_VALUE,
                    detail=f'unknown thinking.type "{kind}"',
                )
            )

        display = thinking.get("display")
        if display is not None:
            if display in ("summarized", "omitted", "updates"):
                req_native.append(
                    NativeAnnotation(
                        source=NATIVE_SOURCE_ANTHROPIC,
                        kind=NATIVE_KIND_THINKING_DISPLAY,
                        value=json.dumps(display),
                        omission=NativeOmission(field="display", reason=LOSS_UNMAPPED_FIELD),
                    )
                )
            else:
                losses.append(
                    Loss(
                        path="thinking.display",
                        field="display",
                        reason=LOSS_UNMAPPED_VALUE,
                        detail=f'unknown thinking.display "{display}"',
                    )
                )

    # 2. OutputConfig
    output_config = wire.get("output_config")
    if isinstance(output_config, dict):
        effort = output_config.get("effort")
        if effort is not None:
            if effort in ("low", "medium", "high", "xhigh", "max"):
                if req.params is not None:
                    req = dataclasses.replace(
                        req, params=dataclasses.replace(req.params, reasoning_effort=effort)
                    )
                req_native.append(
                    NativeAnnotation(
                        source=NATIVE_SOURCE_ANTHROPIC,
                        kind=NATIVE_KIND_OUTPUT_EFFORT,
                        value=json.dumps(effort),
                        projection=NativeProjection(origin="explicit", effort=effort),
                    )
                )
            else:
                losses.append(
                    Loss(
                        path="output_config.effort",
                        field="effort",
                        reason=LOSS_UNMAPPED_VALUE,
                        detail=f'unknown output_config.effort "{effort}"',
                    )
                )

    # 3. Top-level cache_control
    if "cache_control" in wire:
        canon, l = parse_cache_control(wire["cache_control"], "cache_control")
        if l is not None:
            losses.append(l)
        if canon is not None:
            req_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CACHE_CONTROL,
                    value=canon,
                    omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD),
                )
            )

    # 4. ContextManagement
    if "context_management" in wire:
        canon, cm_losses = parse_context_management(wire["context_management"], "context_management")
        losses.extend(cm_losses)
        if canon is not None:
            req_native.append(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CONTEXT_MANAGEMENT,
                    value=canon,
                    omission=NativeOmission(
                        field="context_management", reason=LOSS_UNSUPPORTED_SEMANTIC
                    ),
                )
            )

    # 5. System blocks cache_control
    if isinstance(wire.get("system"), list):
        new_system = list(req.system)
        for i, sb_wire in enumerate(wire["system"]):
            if isinstance(sb_wire, dict) and "cache_control" in sb_wire and i < len(new_system):
                canon, l = parse_cache_control(sb_wire["cache_control"], f"system[{i}].cache_control")
                if l is not None:
                    losses.append(l)
                if canon is not None:
                    ann = NativeAnnotation(
                        source=NATIVE_SOURCE_ANTHROPIC,
                        kind=NATIVE_KIND_CACHE_CONTROL,
                        value=canon,
                        omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD),
                    )
                    new_system[i] = dataclasses.replace(
                        new_system[i], native=new_system[i].native + (ann,)
                    )
        req = dataclasses.replace(req, system=new_system)

    # 6. Tools cache_control
    if isinstance(wire.get("tools"), list) and req.tools is not None:
        new_tools = list(req.tools)
        for i, t_wire in enumerate(wire["tools"]):
            if isinstance(t_wire, dict) and "cache_control" in t_wire and i < len(new_tools):
                canon, l = parse_cache_control(t_wire["cache_control"], f"tools[{i}].cache_control")
                if l is not None:
                    losses.append(l)
                if canon is not None:
                    ann = NativeAnnotation(
                        source=NATIVE_SOURCE_ANTHROPIC,
                        kind=NATIVE_KIND_CACHE_CONTROL,
                        value=canon,
                        omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD),
                    )
                    new_tools[i] = dataclasses.replace(
                        new_tools[i], native=new_tools[i].native + (ann,)
                    )
        req = dataclasses.replace(req, tools=new_tools)

    req = dataclasses.replace(req, native=tuple(req_native))
    return req, losses


def get_block_cache_control(native: tuple[NativeAnnotation, ...]) -> dict[str, Any] | None:
    for n in native:
        if n.source == NATIVE_SOURCE_ANTHROPIC and n.kind == NATIVE_KIND_CACHE_CONTROL:
            try:
                return json.loads(n.value)
            except Exception:
                return None
    return None


def encode_request_native(req: Request, wire: dict[str, Any]) -> list[Loss]:
    losses: list[Loss] = []

    native_mode: str | None = None
    native_budget: int | None = None
    native_budget_proj: NativeProjection | None = None
    native_display: str | None = None
    native_effort: str | None = None

    for n in req.native:
        if n.source != NATIVE_SOURCE_ANTHROPIC:
            continue
        if n.kind == NATIVE_KIND_THINKING_MODE:
            try:
                native_mode = json.loads(n.value)
            except Exception:
                pass
        elif n.kind == NATIVE_KIND_THINKING_BUDGET:
            try:
                native_budget = int(n.value)
                native_budget_proj = n.projection
            except Exception:
                pass
        elif n.kind == NATIVE_KIND_THINKING_DISPLAY:
            try:
                native_display = json.loads(n.value)
            except Exception:
                pass
        elif n.kind == NATIVE_KIND_OUTPUT_EFFORT:
            try:
                native_effort = json.loads(n.value)
            except Exception:
                pass
        elif n.kind == NATIVE_KIND_CACHE_CONTROL:
            try:
                wire["cache_control"] = json.loads(n.value)
            except Exception:
                pass
        elif n.kind == NATIVE_KIND_CONTEXT_MANAGEMENT:
            try:
                wire["context_management"] = json.loads(n.value)
            except Exception:
                pass

    current_effort = req.params.reasoning_effort if req.params else None

    if native_mode is not None or native_effort is not None or current_effort is not None:
        if native_mode == "disabled":
            wire["thinking"] = {"type": "disabled"}
        elif native_mode == "adaptive":
            t_wire: dict[str, Any] = {"type": "adaptive"}
            if native_display is not None:
                t_wire["display"] = native_display
            wire["thinking"] = t_wire
            if current_effort is not None:
                wire["output_config"] = {"effort": current_effort}
        elif native_mode == "enabled":
            if native_budget is not None:
                if native_budget_proj is not None and native_budget_proj.origin == "budget":
                    if native_budget_proj.effort == current_effort:
                        t_wire = {"type": "enabled", "budget_tokens": native_budget}
                        if native_display is not None:
                            t_wire["display"] = native_display
                        wire["thinking"] = t_wire
                    elif current_effort is not None:
                        budgets: dict[str, int] = {
                            "minimal": 1024,
                            "low": 2048,
                            "medium": 8192,
                            "high": 16384,
                        }
                        if current_effort in budgets:
                            t_wire = {
                                "type": "enabled",
                                "budget_tokens": budgets[current_effort],
                            }
                            if native_display is not None:
                                t_wire["display"] = native_display
                            wire["thinking"] = t_wire
                            losses.append(
                                Loss(
                                    path="params.reasoning_effort",
                                    field="reasoning_effort",
                                    reason=LOSS_DEGRADED,
                                    detail="budget approximated",
                                )
                            )
                        else:  # xhigh / max
                            t_wire = {"type": "adaptive"}
                            if native_display is not None:
                                t_wire["display"] = native_display
                            wire["thinking"] = t_wire
                            wire["output_config"] = {"effort": current_effort}
                            losses.append(
                                Loss(
                                    path="params.reasoning_effort",
                                    field="reasoning_effort",
                                    reason=LOSS_DEGRADED,
                                    detail="switched to adaptive thinking mode for extended effort",
                                )
                            )
                else:
                    t_wire = {"type": "enabled", "budget_tokens": native_budget}
                    if native_display is not None:
                        t_wire["display"] = native_display
                    wire["thinking"] = t_wire
                    if current_effort is not None:
                        wire["output_config"] = {"effort": current_effort}
        elif current_effort is not None:
            budgets = {
                "minimal": 1024,
                "low": 2048,
                "medium": 8192,
                "high": 16384,
            }
            if current_effort in budgets:
                wire["thinking"] = {
                    "type": "enabled",
                    "budget_tokens": budgets[current_effort],
                }
                losses.append(
                    Loss(
                        path="params.reasoning_effort",
                        field="reasoning_effort",
                        reason=LOSS_DEGRADED,
                        detail="budget approximated",
                    )
                )
            else:
                wire["thinking"] = {"type": "adaptive"}
                wire["output_config"] = {"effort": current_effort}

    # System cache_control
    if "system" in wire and isinstance(wire["system"], list):
        for i, sb in enumerate(req.system):
            cc = get_block_cache_control(sb.native)
            if cc is not None and i < len(wire["system"]) and isinstance(wire["system"][i], dict):
                wire["system"][i]["cache_control"] = cc

    # Tools cache_control
    if "tools" in wire and isinstance(wire["tools"], list) and req.tools is not None:
        for i, t in enumerate(req.tools):
            cc = get_block_cache_control(t.native)
            if cc is not None and i < len(wire["tools"]) and isinstance(wire["tools"][i], dict):
                wire["tools"][i]["cache_control"] = cc

    return losses
