function canonicalizeJson(val: JsonValue): JsonValue {
  if (Array.isArray(val)) {
    return val.map(canonicalizeJson);
  }
  if (val !== null && typeof val === "object" && !isJsonNumber(val)) {
    const keys = Object.keys(val).sort();
    const sorted: Record<string, JsonValue> = {};
    for (const k of keys) {
      sorted[k] = canonicalizeJson((val as Record<string, JsonValue>)[k]!);
    }
    return sorted;
  }
  return val;
}
// Canonical spec/15 s3.2 capability-gap detail for Anthropic cache
// breakpoints dropped when crossing to a face without cache_control.
export {
  CACHE_CONTROL_LOSS_DETAIL,
  DEFER_LOADING_LOSS_DETAIL,
  NATIVE_KIND_DEFER_LOADING,
} from "../../ir/native.js";
import {
  CACHE_CONTROL_LOSS_DETAIL,
  DEFER_LOADING_LOSS_DETAIL,
  NATIVE_KIND_DEFER_LOADING,
} from "../../ir/native.js";

export function getToolDeferLoading(tool: Tool): boolean {
  return (
    tool.native?.some(
      (n) =>
        n.source === NATIVE_SOURCE_ANTHROPIC &&
        n.kind === NATIVE_KIND_DEFER_LOADING &&
        n.value === "true",
    ) ?? false
  );
}

export function attachBlockCacheControl(
  wireBlock: Record<string, JsonValue>,
  path: string,
  losses: Loss[],
): readonly NativeAnnotation[] | undefined {
  if ("cache_control" in wireBlock && wireBlock.cache_control !== undefined) {
    const parsed = parseCacheControl(
      wireBlock.cache_control,
      `${path}.cache_control`,
    );
    if (parsed.loss) losses.push(parsed.loss);
    if (parsed.ok && parsed.canonical) {
      return [
        {
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: NATIVE_KIND_CACHE_CONTROL,
          value: parsed.canonical,
          omission: {
            field: "cache_control",
            reason: "unmapped-field" as const,
            detail: CACHE_CONTROL_LOSS_DETAIL,
          },
        },
      ];
    }
  }
  return undefined;
}
import type { Loss } from "../../loss.js";
import type {
  Block,
  NativeAnnotation,
  Request,
  Tool,
  ToolResultBlock,
  CustomToolResultBlock,
} from "../../ir/types.js";
import {
  NATIVE_KIND_CACHE_CONTROL,
  NATIVE_KIND_CONTEXT_MANAGEMENT,
  NATIVE_KIND_OUTPUT_EFFORT,
  NATIVE_KIND_THINKING_BUDGET,
  NATIVE_KIND_THINKING_DISPLAY,
  NATIVE_KIND_THINKING_MODE,
  NATIVE_SOURCE_ANTHROPIC,
} from "../../ir/native.js";
import {
  integer,
  isJsonNumber,
  parseJson,
  stringifyJson,
  type JsonObject,
  type JsonValue,
} from "../../json/index.js";

function parseCacheControl(
  raw: JsonValue | undefined,
  path: string,
): { canonical?: string; loss?: Loss; ok: boolean } {
  if (
    raw === undefined ||
    raw === null ||
    typeof raw !== "object" ||
    Array.isArray(raw)
  ) {
    return { ok: false };
  }
  const obj = raw as Record<string, JsonValue>;
  if (obj.type !== "ephemeral") {
    return {
      loss: {
        path: `${path}.type`,
        field: "type",
        reason: "unmapped-value",
        detail: "cache_control type must be ephemeral",
      },
      ok: false,
    };
  }
  const canonicalMap: Record<string, string> = { type: "ephemeral" };
  if ("ttl" in obj && obj.ttl !== undefined) {
    if (obj.ttl !== "5m" && obj.ttl !== "1h") {
      return {
        loss: {
          path: `${path}.ttl`,
          field: "ttl",
          reason: "unmapped-value",
          detail: "cache_control ttl must be 5m or 1h",
        },
        ok: false,
      };
    }
    canonicalMap.ttl = obj.ttl as string;
  }
  // Canonical order: ttl (if present), type
  const sortedKeys = Object.keys(canonicalMap).sort();
  const sorted: Record<string, string> = {};
  for (const k of sortedKeys) {
    sorted[k] = canonicalMap[k]!;
  }
  return { canonical: JSON.stringify(sorted), ok: true };
}

function parseContextManagement(
  raw: JsonValue | undefined,
  path: string,
): { canonical?: string; losses: Loss[]; ok: boolean } {
  const losses: Loss[] = [];
  if (
    raw === undefined ||
    raw === null ||
    typeof raw !== "object" ||
    Array.isArray(raw)
  ) {
    return { losses, ok: false };
  }
  const obj = raw as Record<string, JsonValue>;
  if (!Array.isArray(obj.edits)) {
    return { losses, ok: false };
  }

  const validEdits: JsonValue[] = [];
  for (let i = 0; i < obj.edits.length; i++) {
    const edit = obj.edits[i];
    const editPath = `${path}.edits[${i}]`;
    if (edit === null || typeof edit !== "object" || Array.isArray(edit)) {
      continue;
    }
    const editObj = edit as Record<string, JsonValue>;
    const editType = editObj.type;
    if (editType === "clear_tool_uses_20250919") {
      for (const k of Object.keys(editObj)) {
        if (
          k !== "type" &&
          k !== "trigger" &&
          k !== "keep" &&
          k !== "clear_at_least" &&
          k !== "exclude_tools" &&
          k !== "clear_tool_inputs"
        ) {
          losses.push({
            path: `${editPath}.${k}`,
            field: k,
            reason: "unmapped-field",
          });
        }
      }
      let valid = true;
      const canonEdit: Record<string, JsonValue> = {
        type: "clear_tool_uses_20250919",
      };
      if (editObj.trigger !== undefined) {
        if (
          typeof editObj.trigger === "object" &&
          editObj.trigger !== null &&
          !Array.isArray(editObj.trigger)
        ) {
          const trig = editObj.trigger as Record<string, JsonValue>;
          for (const k of Object.keys(trig)) {
            if (k !== "type" && k !== "value") {
              losses.push({
                path: `${editPath}.trigger.${k}`,
                field: k,
                reason: "unmapped-field",
              });
            }
          }
          if (trig.type !== "input_tokens") {
            losses.push({
              path: `${editPath}.trigger`,
              field: "trigger",
              reason: "unmapped-value",
            });
            valid = false;
          } else {
            canonEdit.trigger = {
              type: "input_tokens",
              ...(trig.value !== undefined
                ? { value: trig.value as JsonValue }
                : {}),
            };
          }
        } else {
          losses.push({
            path: `${editPath}.trigger`,
            field: "trigger",
            reason: "unmapped-value",
          });
          valid = false;
        }
      }
      if (editObj.keep !== undefined) {
        if (
          typeof editObj.keep === "object" &&
          editObj.keep !== null &&
          !Array.isArray(editObj.keep)
        ) {
          const kp = editObj.keep as Record<string, JsonValue>;
          for (const k of Object.keys(kp)) {
            if (k !== "type" && k !== "value") {
              losses.push({
                path: `${editPath}.keep.${k}`,
                field: k,
                reason: "unmapped-field",
              });
            }
          }
          if (kp.type !== "tool_uses") {
            losses.push({
              path: `${editPath}.keep`,
              field: "keep",
              reason: "unmapped-value",
            });
            valid = false;
          } else {
            canonEdit.keep = {
              type: "tool_uses",
              ...(kp.value !== undefined
                ? { value: kp.value as JsonValue }
                : {}),
            };
          }
        } else {
          losses.push({
            path: `${editPath}.keep`,
            field: "keep",
            reason: "unmapped-value",
          });
          valid = false;
        }
      }
      if (editObj.clear_at_least !== undefined) {
        if (
          typeof editObj.clear_at_least === "object" &&
          editObj.clear_at_least !== null &&
          !Array.isArray(editObj.clear_at_least)
        ) {
          const cal = editObj.clear_at_least as Record<string, JsonValue>;
          for (const k of Object.keys(cal)) {
            if (k !== "type" && k !== "value") {
              losses.push({
                path: `${editPath}.clear_at_least.${k}`,
                field: k,
                reason: "unmapped-field",
              });
            }
          }
          if (cal.type !== "input_tokens") {
            losses.push({
              path: `${editPath}.clear_at_least`,
              field: "clear_at_least",
              reason: "unmapped-value",
            });
            valid = false;
          } else {
            canonEdit.clear_at_least = {
              type: "input_tokens",
              ...(cal.value !== undefined
                ? { value: cal.value as JsonValue }
                : {}),
            };
          }
        } else {
          losses.push({
            path: `${editPath}.clear_at_least`,
            field: "clear_at_least",
            reason: "unmapped-value",
          });
          valid = false;
        }
      }
      if (editObj.exclude_tools !== undefined) {
        canonEdit.exclude_tools = editObj.exclude_tools;
      }
      if (editObj.clear_tool_inputs !== undefined) {
        canonEdit.clear_tool_inputs = editObj.clear_tool_inputs;
      }
      if (valid) {
        validEdits.push(canonEdit);
      }
    } else if (editType === "clear_thinking_20251015") {
      for (const k of Object.keys(editObj)) {
        if (k !== "type" && k !== "keep") {
          losses.push({
            path: `${editPath}.${k}`,
            field: k,
            reason: "unmapped-field",
          });
        }
      }
      let valid = true;
      const canonEdit: Record<string, JsonValue> = {
        type: "clear_thinking_20251015",
      };
      if ("keep" in editObj && editObj.keep !== undefined) {
        if (typeof editObj.keep === "string") {
          if (editObj.keep !== "all") {
            losses.push({
              path: `${editPath}.keep`,
              field: "keep",
              reason: "unmapped-value",
            });
            valid = false;
          } else {
            canonEdit.keep = "all";
          }
        } else if (
          typeof editObj.keep === "object" &&
          editObj.keep !== null &&
          !Array.isArray(editObj.keep)
        ) {
          const kp = editObj.keep as Record<string, JsonValue>;
          for (const k of Object.keys(kp)) {
            if (k !== "type" && k !== "value") {
              losses.push({
                path: `${editPath}.keep.${k}`,
                field: k,
                reason: "unmapped-field",
              });
            }
          }
          if (kp.type !== "thinking_turns") {
            losses.push({
              path: `${editPath}.keep`,
              field: "keep",
              reason: "unmapped-value",
            });
            valid = false;
          } else {
            canonEdit.keep = {
              type: "thinking_turns",
              ...(kp.value !== undefined
                ? { value: kp.value as JsonValue }
                : {}),
            };
          }
        } else {
          losses.push({
            path: `${editPath}.keep`,
            field: "keep",
            reason: "unmapped-value",
          });
          valid = false;
        }
      }
      if (valid) {
        validEdits.push(canonEdit);
      }
    } else {
      losses.push({
        path: editPath,
        field: "type",
        reason: "unsupported-semantic" as const,
        detail: `unsupported context edit type "${String(editType)}"`,
      });
    }
  }

  if (validEdits.length === 0) {
    return { losses, ok: false };
  }
  return {
    canonical: stringifyJson(canonicalizeJson({ edits: validEdits })),
    losses,
    ok: true,
  };
}

export function decodeRequestNative(
  wire: JsonObject,
  request: Request,
  losses: Loss[],
): Request {
  const reqNative: NativeAnnotation[] = [];
  let reasoningEffort = request.params?.reasoning_effort;

  // 1. Thinking
  if (
    wire.thinking &&
    typeof wire.thinking === "object" &&
    !Array.isArray(wire.thinking)
  ) {
    const thinking = wire.thinking as Record<string, JsonValue>;
    const tType = thinking.type;
    if (tType === "adaptive" || tType === "disabled") {
      reqNative.push({
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_THINKING_MODE,
        value: JSON.stringify(tType),
        omission: { field: "type", reason: "unmapped-field" as const },
      });
    } else if (tType === "enabled") {
      const modeAnn: NativeAnnotation = {
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_THINKING_MODE,
        value: '"enabled"',
        ...(wire.output_config &&
        typeof wire.output_config === "object" &&
        !Array.isArray(wire.output_config) &&
        (wire.output_config as Record<string, JsonValue>).effort
          ? { omission: { field: "type", reason: "unmapped-field" as const } }
          : {}),
      };
      reqNative.push(modeAnn);

      let b: bigint | undefined;
      if (typeof thinking.budget_tokens === "bigint") {
        b = thinking.budget_tokens;
      } else if (
        typeof thinking.budget_tokens === "number" &&
        Number.isInteger(thinking.budget_tokens)
      ) {
        b = BigInt(thinking.budget_tokens);
      } else if (isJsonNumber(thinking.budget_tokens)) {
        try {
          b = BigInt(thinking.budget_tokens.token);
        } catch {
          // ignore
        }
      }
      if (b !== undefined && b > 0n) {
        const hasExplicitEffort =
          wire.output_config &&
          typeof wire.output_config === "object" &&
          !Array.isArray(wire.output_config) &&
          Boolean((wire.output_config as Record<string, JsonValue>).effort);

        if (hasExplicitEffort) {
          reqNative.push({
            source: NATIVE_SOURCE_ANTHROPIC,
            kind: NATIVE_KIND_THINKING_BUDGET,
            value: b.toString(),
            omission: {
              field: "budget_tokens",
              reason: "unmapped-field" as const,
            },
          });
        } else {
          const mappedEffort =
            b <= 2048n ? "low" : b <= 8192n ? "medium" : "high";
          reasoningEffort = mappedEffort;
          reqNative.push({
            source: NATIVE_SOURCE_ANTHROPIC,
            kind: NATIVE_KIND_THINKING_BUDGET,
            value: b.toString(),
            omission: {
              field: "budget_tokens",
              reason: "degraded" as const,
              detail: "budget approximated",
            },
            projection: { origin: "budget", effort: mappedEffort },
          });
        }
      }
    } else {
      losses.push({
        path: "thinking.type",
        field: "type",
        reason: "unmapped-value",
        detail: `unknown thinking.type "${String(tType)}"`,
      });
    }

    if ("display" in thinking && thinking.display !== undefined) {
      const d = thinking.display;
      if (d === "summarized" || d === "omitted" || d === "updates") {
        reqNative.push({
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: NATIVE_KIND_THINKING_DISPLAY,
          value: JSON.stringify(d),
          omission: { field: "display", reason: "unmapped-field" as const },
        });
      } else {
        losses.push({
          path: "thinking.display",
          field: "display",
          reason: "unmapped-value",
          detail: `unknown thinking.display "${String(d)}"`,
        });
      }
    }
  }

  // 2. OutputConfig
  if (
    wire.output_config &&
    typeof wire.output_config === "object" &&
    !Array.isArray(wire.output_config)
  ) {
    const oc = wire.output_config as Record<string, JsonValue>;
    if ("effort" in oc && oc.effort !== undefined) {
      const eff = oc.effort;
      if (
        eff === "low" ||
        eff === "medium" ||
        eff === "high" ||
        eff === "xhigh" ||
        eff === "max"
      ) {
        reasoningEffort = eff;
        reqNative.push({
          source: NATIVE_SOURCE_ANTHROPIC,
          kind: NATIVE_KIND_OUTPUT_EFFORT,
          value: JSON.stringify(eff),
          projection: { origin: "explicit", effort: eff },
        });
      } else {
        losses.push({
          path: "output_config.effort",
          field: "effort",
          reason: "unmapped-value",
          detail: `unknown output_config.effort "${String(eff)}"`,
        });
      }
    }
  }

  // 3. Top-level cache_control
  if ("cache_control" in wire && wire.cache_control !== undefined) {
    const parsed = parseCacheControl(wire.cache_control, "cache_control");
    if (parsed.loss) losses.push(parsed.loss);
    if (parsed.ok && parsed.canonical) {
      reqNative.push({
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_CACHE_CONTROL,
        value: parsed.canonical,
        omission: {
          field: "cache_control",
          reason: "unmapped-field" as const,
          detail: CACHE_CONTROL_LOSS_DETAIL,
        },
      });
    }
  }

  // 4. ContextManagement
  if ("context_management" in wire && wire.context_management !== undefined) {
    const parsed = parseContextManagement(
      wire.context_management,
      "context_management",
    );
    losses.push(...parsed.losses);
    if (parsed.ok && parsed.canonical) {
      reqNative.push({
        source: NATIVE_SOURCE_ANTHROPIC,
        kind: NATIVE_KIND_CONTEXT_MANAGEMENT,
        value: parsed.canonical,
        omission: {
          field: "context_management",
          reason: "unsupported-semantic" as const,
        },
      });
    }
  }

  return {
    ...request,
    ...(reqNative.length > 0 ? { native: reqNative } : {}),
    params: {
      ...request.params,
      ...(reasoningEffort !== undefined
        ? { reasoning_effort: reasoningEffort }
        : {}),
    },
  };
}

export function getBlockCacheControl(
  blk: Block | Tool,
): JsonObject | undefined {
  const nat = (blk as any).native as readonly NativeAnnotation[] | undefined;
  const ann = nat?.find(
    (n) =>
      n.source === NATIVE_SOURCE_ANTHROPIC &&
      n.kind === NATIVE_KIND_CACHE_CONTROL,
  );
  if (ann) {
    return parseJson(ann.value) as JsonObject;
  }
  return undefined;
}

export function encodeRequestNative(
  request: Request,
  wire: Record<string, JsonValue>,
  losses: Loss[],
): void {
  let nativeMode: string | undefined;
  let nativeBudget: bigint | undefined;
  let nativeBudgetProj: NativeAnnotation["projection"] | undefined;
  let nativeDisplay: string | undefined;
  let nativeEffort: string | undefined;
  let nativeEffortProj: NativeAnnotation["projection"] | undefined;

  const nativeList = request.native ?? [];
  for (let i = 0; i < nativeList.length; i++) {
    const n = nativeList[i]!;
    if (n.source !== NATIVE_SOURCE_ANTHROPIC) {
      losses.push({
        path: `native[${i}]`,
        field: "source",
        reason: "unsupported-semantic",
        detail: `unsupported native option source "${n.source}"`,
      });
      continue;
    }
    switch (n.kind) {
      case NATIVE_KIND_THINKING_MODE:
        nativeMode = JSON.parse(n.value) as string;
        break;
      case NATIVE_KIND_THINKING_BUDGET:
        nativeBudget = BigInt(JSON.parse(n.value) as number | string);
        nativeBudgetProj = n.projection;
        break;
      case NATIVE_KIND_THINKING_DISPLAY:
        nativeDisplay = JSON.parse(n.value) as string;
        break;
      case NATIVE_KIND_OUTPUT_EFFORT:
        nativeEffort = JSON.parse(n.value) as string;
        nativeEffortProj = n.projection;
        break;
      case NATIVE_KIND_CACHE_CONTROL:
        wire.cache_control = parseJson(n.value) as JsonObject;
        break;
      case NATIVE_KIND_CONTEXT_MANAGEMENT:
        wire.context_management = parseJson(n.value) as JsonObject;
        break;
      default:
        losses.push({
          path: `native[${i}]`,
          field: "kind",
          reason: "unsupported-semantic",
          detail: `unsupported native option kind "${n.kind}"`,
        });
        break;
    }
  }

  // Thinking / Effort synthesis
  const currentEffort = request.params?.reasoning_effort;
  if (
    nativeMode !== undefined ||
    nativeEffort !== undefined ||
    currentEffort !== undefined
  ) {
    if (nativeMode === "disabled") {
      wire.thinking = { type: "disabled" };
      if (nativeEffort !== undefined || nativeEffortProj !== undefined) {
        if (currentEffort !== undefined) {
          wire.output_config = { effort: currentEffort };
        }
      }
    } else if (nativeMode === "adaptive") {
      wire.thinking = {
        type: "adaptive",
        ...(nativeDisplay !== undefined ? { display: nativeDisplay } : {}),
      };
      if (currentEffort !== undefined) {
        wire.output_config = { effort: currentEffort };
      }
    } else if (nativeMode === "enabled") {
      if (nativeBudget !== undefined && nativeBudget > 0n) {
        if (nativeBudgetProj?.origin === "budget") {
          if (currentEffort === nativeBudgetProj.effort) {
            // Unedited: restore exact budget
            wire.thinking = {
              type: "enabled",
              budget_tokens: integer(nativeBudget),
              ...(nativeDisplay !== undefined
                ? { display: nativeDisplay }
                : {}),
            };
          } else if (currentEffort !== undefined) {
            // Edited: reconstruct
            switch (currentEffort) {
              case "minimal":
                wire.thinking = {
                  type: "enabled",
                  budget_tokens: integer(1024n),
                };
                losses.push({
                  path: "params.reasoning_effort",
                  field: "reasoning_effort",
                  reason: "degraded",
                  detail: "budget approximated",
                });
                break;
              case "low":
                wire.thinking = {
                  type: "enabled",
                  budget_tokens: integer(2048n),
                };
                losses.push({
                  path: "params.reasoning_effort",
                  field: "reasoning_effort",
                  reason: "degraded",
                  detail: "budget approximated",
                });
                break;
              case "medium":
                wire.thinking = {
                  type: "enabled",
                  budget_tokens: integer(8192n),
                };
                losses.push({
                  path: "params.reasoning_effort",
                  field: "reasoning_effort",
                  reason: "degraded",
                  detail: "budget approximated",
                });
                break;
              case "high":
                wire.thinking = {
                  type: "enabled",
                  budget_tokens: integer(16384n),
                };
                losses.push({
                  path: "params.reasoning_effort",
                  field: "reasoning_effort",
                  reason: "degraded",
                  detail: "budget approximated",
                });
                break;
              case "xhigh":
              case "max":
                wire.thinking = { type: "adaptive" };
                wire.output_config = { effort: currentEffort };
                losses.push({
                  path: "params.reasoning_effort",
                  field: "reasoning_effort",
                  reason: "degraded",
                  detail:
                    "switched to adaptive thinking mode for extended effort",
                });
                break;
            }
            if (wire.thinking && nativeDisplay !== undefined) {
              (wire.thinking as Record<string, JsonValue>).display =
                nativeDisplay;
            }
          } else {
            // Effort was cleared! Thinking cannot be formed:
            delete wire.thinking;
            losses.push({
              path: "thinking",
              field: "thinking",
              reason: "unsupported-semantic",
              detail:
                "thinking configuration invalidated after clearing reasoning_effort",
            });
          }
        } else {
          // Enabled budget with explicit effort
          wire.thinking = {
            type: "enabled",
            budget_tokens: integer(nativeBudget),
            ...(nativeDisplay !== undefined ? { display: nativeDisplay } : {}),
          };
          if (currentEffort !== undefined) {
            wire.output_config = { effort: currentEffort };
          }
        }
      }
    } else {
      // No nativeMode recorded:
      if (nativeEffort !== undefined || nativeEffortProj !== undefined) {
        if (currentEffort !== undefined) {
          wire.output_config = { effort: currentEffort };
        }
      } else {
        // Standard unannotated IR ReasoningEffort mapping (budget table)
        switch (currentEffort) {
          case "minimal":
            wire.thinking = { type: "enabled", budget_tokens: integer(1024n) };
            losses.push({
              path: "params.reasoning_effort",
              field: "reasoning_effort",
              reason: "degraded",
              detail: "budget approximated",
            });
            break;
          case "low":
            wire.thinking = { type: "enabled", budget_tokens: integer(2048n) };
            losses.push({
              path: "params.reasoning_effort",
              field: "reasoning_effort",
              reason: "degraded",
              detail: "budget approximated",
            });
            break;
          case "medium":
            wire.thinking = { type: "enabled", budget_tokens: integer(8192n) };
            losses.push({
              path: "params.reasoning_effort",
              field: "reasoning_effort",
              reason: "degraded",
              detail: "budget approximated",
            });
            break;
          case "high":
            wire.thinking = { type: "enabled", budget_tokens: integer(16384n) };
            losses.push({
              path: "params.reasoning_effort",
              field: "reasoning_effort",
              reason: "degraded",
              detail: "budget approximated",
            });
            break;
          case "xhigh":
          case "max":
            wire.thinking = { type: "adaptive" };
            wire.output_config = { effort: currentEffort };
            break;
        }
      }
    }
  }
}
