import { OxaError } from "../error.js";
import type { Loss } from "../loss.js";
import type {
  Block,
  NativeAnnotation,
  Request,
  ToolResultBlock,
  CustomToolResultBlock,
} from "./types.js";

export const NATIVE_KIND_THINKING_MODE = "thinking_mode";
export const NATIVE_KIND_THINKING_BUDGET = "thinking_budget";
export const NATIVE_KIND_THINKING_DISPLAY = "thinking_display";
export const NATIVE_KIND_OUTPUT_EFFORT = "output_effort";
export const NATIVE_KIND_CACHE_CONTROL = "cache_control";
export const NATIVE_KIND_CONTEXT_MANAGEMENT = "context_management";
export const NATIVE_KIND_DEFER_LOADING = "defer_loading";
export const NATIVE_KIND_DOCUMENT_TITLE = "document_title";
export const NATIVE_KIND_DOCUMENT_CONTEXT = "document_context";
export const NATIVE_KIND_DOCUMENT_CITATIONS = "document_citations";
export const NATIVE_KIND_CONTAINER = "container";
export const NATIVE_KIND_EAGER_INPUT_STREAMING = "eager_input_streaming";
export const NATIVE_KIND_STORE = "store";
export const NATIVE_KIND_STREAM = "stream";
export const NATIVE_KIND_STREAM_OPTIONS = "stream_options";
export const NATIVE_KIND_MAX_COMPLETION_TOKENS = "max_completion_tokens";
export const NATIVE_KIND_TOP_K = "top_k";
export const NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE =
  "disable_parallel_tool_use";
export const NATIVE_SOURCE_ANTHROPIC = "anthropic";
export const NATIVE_SOURCE_OPENAI = "openai";

export const CACHE_CONTROL_LOSS_DETAIL =
  "anthropic-native cache breakpoint; the target face has no cache_control representation";

export const DEFER_LOADING_LOSS_DETAIL =
  "deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search";

export const EAGER_INPUT_STREAMING_LOSS_DETAIL =
  "target does not preserve eager tool input streaming semantics";

export const STREAM_OPTIONS_LOSS_DETAIL =
  "target does not emit the requested final usage chunk";

export interface LossFingerprint {
  path: string;
  field: string;
  reason: string;
}

export function fingerprintLosses(
  losses: readonly Loss[],
  limit = 0,
): LossFingerprint[] {
  const n = limit > 0 && limit < losses.length ? limit : losses.length;
  const out: LossFingerprint[] = [];
  for (let i = 0; i < n; i++) {
    const l = losses[i]!;
    out.push({
      path: l.path,
      field: l.field,
      reason: l.reason,
    });
  }
  return out;
}

export const DOCUMENT_TITLE_LOSS_DETAIL =
  "anthropic-native document title; the target face has no document representation";

export const DOCUMENT_CONTEXT_LOSS_DETAIL =
  "anthropic-native document context; the target face has no document representation";

export const DOCUMENT_CITATIONS_LOSS_DETAIL =
  "anthropic-native document citations; citations alter response semantics and the target face has no document representation";

type HostType = "request" | "system" | "content" | "tool";

export function validateNativeRequest(request: Request): void {
  if (request.native) {
    validateAnnotations(request.native, "request", "request");
  }

  let budgetProjCount = 0;
  let explicitProjCount = 0;
  for (const n of request.native ?? []) {
    if (n.projection) {
      const orig = n.projection.origin;
      if (orig !== "budget" && orig !== "explicit") {
        throw new OxaError(
          "invalid-input",
          `ir: native projection origin "${orig}" invalid`,
        );
      }
      const eff = n.projection.effort;
      if (
        eff !== "minimal" &&
        eff !== "low" &&
        eff !== "medium" &&
        eff !== "high" &&
        eff !== "xhigh" &&
        eff !== "max"
      ) {
        throw new OxaError(
          "invalid-input",
          `ir: native projection effort "${eff}" invalid`,
        );
      }
      if (orig === "budget") {
        if (n.kind !== NATIVE_KIND_THINKING_BUDGET) {
          throw new OxaError(
            "invalid-input",
            "ir: budget projection is only allowed on thinking_budget",
          );
        }
        budgetProjCount++;
        if (budgetProjCount > 1) {
          throw new OxaError(
            "invalid-input",
            "ir: duplicate budget projection on request",
          );
        }
      } else if (orig === "explicit") {
        if (n.kind !== NATIVE_KIND_OUTPUT_EFFORT) {
          throw new OxaError(
            "invalid-input",
            "ir: explicit projection is only allowed on output_effort",
          );
        }
        explicitProjCount++;
        if (explicitProjCount > 1) {
          throw new OxaError(
            "invalid-input",
            "ir: duplicate explicit projection on request",
          );
        }
      }
    }
  }

  if (budgetProjCount > 0 && explicitProjCount > 0) {
    throw new OxaError(
      "invalid-input",
      "ir: conflicting effort projections on request",
    );
  }

  let hasBetweenTools = false;
  let hasBudgetOrDisplay = false;
  let hasDisableParallel = false;
  if (request.native) {
    for (const n of request.native) {
      if (n.source === NATIVE_SOURCE_ANTHROPIC) {
        if (
          n.kind === NATIVE_KIND_THINKING_MODE &&
          n.value === '"between_tools"'
        ) {
          hasBetweenTools = true;
        } else if (
          n.kind === NATIVE_KIND_THINKING_BUDGET ||
          n.kind === NATIVE_KIND_THINKING_DISPLAY
        ) {
          hasBudgetOrDisplay = true;
        } else if (n.kind === NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE) {
          hasDisableParallel = true;
        }
      }
    }
  }
  if (hasBetweenTools && hasBudgetOrDisplay) {
    throw new OxaError(
      "invalid-input",
      "anthropic: cannot specify budget_tokens or display with thinking.type between_tools",
    );
  }
  if (hasDisableParallel) {
    if (!request.tool_choice || request.tool_choice.mode === "none") {
      throw new OxaError(
        "invalid-input",
        "anthropic: cannot specify disable_parallel_tool_use when tool_choice is none or not specified",
      );
    }
  }

  request.system?.forEach((sb, i) => {
    if (sb.native) {
      validateAnnotations(sb.native, "system", `system[${i}]`);
    }
  });

  request.messages.forEach((msg, mIdx) => {
    msg.content.forEach((blk, cIdx) => {
      validateBlockNative(blk, "content", `messages[${mIdx}].content[${cIdx}]`);
    });
  });

  request.tools?.forEach((t, tIdx) => {
    if (t.native) {
      validateAnnotations(t.native, "tool", `tools[${tIdx}]`);
      let hasCacheControl = false;
      let hasDeferLoading = false;
      for (const n of t.native) {
        if (n.source === NATIVE_SOURCE_ANTHROPIC) {
          if (n.kind === NATIVE_KIND_CACHE_CONTROL) {
            hasCacheControl = true;
          } else if (n.kind === NATIVE_KIND_DEFER_LOADING) {
            hasDeferLoading = true;
          }
        }
      }
      if (hasCacheControl && hasDeferLoading) {
        throw new OxaError(
          "invalid-input",
          `ir: tools[${tIdx}]: cannot specify both defer_loading and cache_control`,
        );
      }
    }
  });
}

function validateAnnotations(
  annotations: readonly NativeAnnotation[],
  host: HostType,
  path: string,
): void {
  const seen = new Set<string>();
  for (let i = 0; i < annotations.length; i++) {
    const ann = annotations[i]!;
    if (!ann.source) {
      throw new OxaError(
        "invalid-input",
        `${path}.native[${i}]: source cannot be empty`,
      );
    }
    if (!ann.kind) {
      throw new OxaError(
        "invalid-input",
        `${path}.native[${i}]: kind cannot be empty`,
      );
    }
    let parsedJson: any;
    try {
      parsedJson = JSON.parse(ann.value);
    } catch {
      throw new OxaError(
        "invalid-input",
        `${path}.native[${i}]: value is not valid JSON text`,
      );
    }
    if (ann.omission) {
      if (!ann.omission.field) {
        throw new OxaError(
          "invalid-input",
          `${path}.native[${i}]: omission field cannot be empty`,
        );
      }
      const r = ann.omission.reason;
      if (
        r !== "unmapped-field" &&
        r !== "unmapped-value" &&
        r !== "unsupported-semantic" &&
        r !== "degraded"
      ) {
        throw new OxaError(
          "invalid-input",
          `${path}.native[${i}]: invalid omission reason`,
        );
      }
    }
    const key = `${ann.source}:${ann.kind}`;
    if (seen.has(key)) {
      throw new OxaError(
        "invalid-input",
        `${path}: duplicate native annotation for ${key}`,
      );
    }
    seen.add(key);

    if (ann.source === NATIVE_SOURCE_ANTHROPIC) {
      switch (ann.kind) {
        case NATIVE_KIND_THINKING_MODE:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            typeof parsedJson !== "string" ||
            (parsedJson !== "adaptive" &&
              parsedJson !== "enabled" &&
              parsedJson !== "disabled" &&
              parsedJson !== "between_tools")
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid thinking_mode ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_TOP_K:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            typeof parsedJson !== "number" ||
            !Number.isInteger(parsedJson) ||
            parsedJson < 0 ||
            parsedJson > Number.MAX_SAFE_INTEGER
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid top_k ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (typeof parsedJson !== "boolean") {
            throw new OxaError(
              "invalid-input",
              `invalid disable_parallel_tool_use ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_THINKING_BUDGET:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            (typeof parsedJson !== "number" &&
              typeof parsedJson !== "string") ||
            Number(parsedJson) <= 0 ||
            !Number.isInteger(Number(parsedJson))
          ) {
            throw new OxaError(
              "invalid-input",
              "anthropic thinking_budget must be a positive integer",
            );
          }
          break;
        case NATIVE_KIND_THINKING_DISPLAY:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            typeof parsedJson !== "string" ||
            (parsedJson !== "summarized" &&
              parsedJson !== "omitted" &&
              parsedJson !== "updates")
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid thinking_display ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_OUTPUT_EFFORT:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            typeof parsedJson !== "string" ||
            (parsedJson !== "low" &&
              parsedJson !== "medium" &&
              parsedJson !== "high" &&
              parsedJson !== "xhigh" &&
              parsedJson !== "max")
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid output_effort ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_CONTEXT_MANAGEMENT:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          if (
            parsedJson === null ||
            typeof parsedJson !== "object" ||
            !Array.isArray(parsedJson.edits)
          ) {
            throw new OxaError(
              "invalid-input",
              "context_management requires edits array",
            );
          }
          break;
        case NATIVE_KIND_CACHE_CONTROL:
          if (
            parsedJson === null ||
            typeof parsedJson !== "object" ||
            Array.isArray(parsedJson) ||
            parsedJson.type !== "ephemeral"
          ) {
            throw new OxaError("invalid-input", "invalid cache_control object");
          }
          if (
            "ttl" in parsedJson &&
            parsedJson.ttl !== "5m" &&
            parsedJson.ttl !== "1h"
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid cache_control ttl ${String(parsedJson.ttl)}`,
            );
          }
          break;
        case NATIVE_KIND_DOCUMENT_TITLE:
        case NATIVE_KIND_DOCUMENT_CONTEXT:
          if (host !== "content") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on content blocks`,
            );
          }
          if (typeof parsedJson !== "string") {
            throw new OxaError(
              "invalid-input",
              `anthropic ${ann.kind} value must be a JSON string`,
            );
          }
          break;
        case NATIVE_KIND_DOCUMENT_CITATIONS:
          if (host !== "content") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on content blocks`,
            );
          }
          if (
            typeof parsedJson !== "object" ||
            parsedJson === null ||
            Array.isArray(parsedJson)
          ) {
            throw new OxaError(
              "invalid-input",
              `anthropic ${ann.kind} value must be a JSON object`,
            );
          }
          if (
            parsedJson.enabled !== undefined &&
            typeof parsedJson.enabled !== "boolean"
          ) {
            throw new OxaError(
              "invalid-input",
              `anthropic ${ann.kind} enabled must be a boolean`,
            );
          }
          break;
        case NATIVE_KIND_DEFER_LOADING:
          if (host !== "tool") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Tool`,
            );
          }
          if (parsedJson !== true) {
            throw new OxaError(
              "invalid-input",
              `invalid defer_loading ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_EAGER_INPUT_STREAMING:
          if (host !== "tool") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Tool`,
            );
          }
          if (typeof parsedJson !== "boolean") {
            throw new OxaError(
              "invalid-input",
              `invalid eager_input_streaming ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
      }
    } else if (ann.source === NATIVE_SOURCE_OPENAI) {
      switch (ann.kind) {
        case NATIVE_KIND_MAX_COMPLETION_TOKENS:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai ${ann.kind} is only allowed on Request`,
            );
          }
          if (parsedJson !== true) {
            throw new OxaError(
              "invalid-input",
              `invalid max_completion_tokens ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_STORE:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai ${ann.kind} is only allowed on Request`,
            );
          }
          if (typeof parsedJson !== "boolean" && parsedJson !== null) {
            throw new OxaError(
              "invalid-input",
              `invalid store ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_STREAM:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai ${ann.kind} is only allowed on Request`,
            );
          }
          if (typeof parsedJson !== "boolean" && parsedJson !== null) {
            throw new OxaError(
              "invalid-input",
              `invalid stream ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
        case NATIVE_KIND_STREAM_OPTIONS:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai ${ann.kind} is only allowed on Request`,
            );
          }
          if (parsedJson === null) {
            // explicit null is allowed
          } else if (
            typeof parsedJson !== "object" ||
            Array.isArray(parsedJson)
          ) {
            throw new OxaError(
              "invalid-input",
              `invalid stream_options ${JSON.stringify(parsedJson)}`,
            );
          } else {
            const so = parsedJson as Record<string, unknown>;
            for (const [k, v] of Object.entries(so)) {
              if (k === "include_usage" || k === "include_obfuscation") {
                if (typeof v !== "boolean") {
                  throw new OxaError(
                    "invalid-input",
                    `invalid stream_options ${JSON.stringify(parsedJson)}: ${k} must be a boolean`,
                  );
                }
              }
            }
          }
          break;
        case NATIVE_KIND_CONTAINER:
          if (host !== "tool") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai container is only allowed on Tool`,
            );
          }
          if (
            typeof parsedJson !== "object" ||
            parsedJson === null ||
            (!("path" in parsedJson) && typeof parsedJson !== "string")
          ) {
            throw new OxaError(
              "invalid-input",
              `${path}: openai container must be an object with path array or string`,
            );
          }
          break;
        case NATIVE_KIND_DEFER_LOADING:
          if (host !== "tool") {
            throw new OxaError(
              "invalid-input",
              `${path}: openai defer_loading is only allowed on Tool`,
            );
          }
          if (parsedJson !== true) {
            throw new OxaError(
              "invalid-input",
              `invalid defer_loading ${JSON.stringify(parsedJson)}`,
            );
          }
          break;
      }
    }
  }
}

function validateBlockNative(blk: Block, host: HostType, path: string): void {
  if ("native" in blk && blk.native) {
    validateAnnotations(blk.native, host, path);
  }
  if (blk.type === "tool_result") {
    (blk as ToolResultBlock).content.forEach((child, k) => {
      validateBlockNative(child, host, `${path}.content[${k}]`);
    });
  } else if (blk.type === "custom_tool_result") {
    const out = (blk as CustomToolResultBlock).output;
    if (Array.isArray(out)) {
      out.forEach((child, k) => {
        validateBlockNative(child, host, `${path}.output.content[${k}]`);
      });
    }
  }
}

export function omittedNativeRequestLosses(
  request: Request,
  target: string,
): readonly Loss[] {
  validateNativeRequest(request);

  const losses: Loss[] = [];
  const hasActiveBudgetProj =
    request.native?.some(
      (n) =>
        n.source === NATIVE_SOURCE_ANTHROPIC &&
        n.kind === NATIVE_KIND_THINKING_BUDGET &&
        n.projection?.origin === "budget" &&
        n.projection?.effort === request.params?.reasoning_effort,
    ) ?? false;

  request.native?.forEach((ann, i) => {
    appendOmissionLoss(
      losses,
      ann,
      `native[${i}]`,
      hasActiveBudgetProj,
      request.params?.reasoning_effort,
      target,
    );
  });

  request.system?.forEach((sb, sIdx) => {
    sb.native?.forEach((ann, i) => {
      appendOmissionLoss(
        losses,
        ann,
        `system[${sIdx}].native[${i}]`,
        false,
        undefined,
        target,
      );
    });
  });

  request.messages.forEach((msg, mIdx) => {
    msg.content.forEach((blk, cIdx) => {
      collectBlockOmissions(
        losses,
        blk,
        `messages[${mIdx}].content[${cIdx}]`,
        target,
      );
    });
  });

  request.tools?.forEach((t, tIdx) => {
    t.native?.forEach((ann, i) => {
      appendOmissionLoss(
        losses,
        ann,
        `tools[${tIdx}].native[${i}]`,
        false,
        undefined,
        target,
      );
    });
  });

  return losses;
}

function collectBlockOmissions(
  losses: Loss[],
  blk: Block,
  path: string,
  target: string,
): void {
  if (
    blk.type === "redacted_thinking" ||
    blk.type === "web_search_call" ||
    (blk.type === "document" &&
      target === "chatcompletions" &&
      blk.url !== undefined)
  ) {
    // whole-block drop; annotations not reported a second time
    return;
  }
  blk.native?.forEach((ann, i) => {
    appendOmissionLoss(
      losses,
      ann,
      `${path}.native[${i}]`,
      false,
      undefined,
      target,
    );
  });
  if (blk.type === "tool_result") {
    (blk as ToolResultBlock).content.forEach((child, k) => {
      collectBlockOmissions(losses, child, `${path}.content[${k}]`, target);
    });
  } else if (blk.type === "custom_tool_result") {
    const out = (blk as CustomToolResultBlock).output;
    if (Array.isArray(out)) {
      out.forEach((child, k) => {
        collectBlockOmissions(
          losses,
          child,
          `${path}.output.content[${k}]`,
          target,
        );
      });
    }
  }
}

function appendOmissionLoss(
  losses: Loss[],
  ann: NativeAnnotation,
  path: string,
  hasActiveBudgetProj: boolean,
  currentEffort: string | undefined,
  target: string,
): void {
  if (ann.source === NATIVE_SOURCE_OPENAI) {
    switch (ann.kind) {
      case NATIVE_KIND_MAX_COMPLETION_TOKENS:
      case NATIVE_KIND_STREAM:
        return;
      case NATIVE_KIND_STREAM_OPTIONS: {
        if (target === "chatcompletions") {
          return;
        }
        let hasUsageTrue = false;
        try {
          const parsed = JSON.parse(ann.value);
          if (
            parsed &&
            typeof parsed === "object" &&
            parsed.include_usage === true
          ) {
            hasUsageTrue = true;
          }
        } catch {}
        if (hasUsageTrue) {
          const field = ann.omission?.field || "stream_options";
          const reason = ann.omission?.reason || "degraded";
          const detail = ann.omission?.detail || STREAM_OPTIONS_LOSS_DETAIL;
          losses.push({
            path,
            field,
            reason,
            ...(detail ? { detail } : {}),
          });
        }
        return;
      }
      case NATIVE_KIND_STORE: {
        if (target === "chatcompletions" || target === "responses") {
          return;
        }
        let isTrue = false;
        try {
          isTrue = JSON.parse(ann.value) === true;
        } catch {}
        if (isTrue) {
          const field = ann.omission?.field || "store";
          const reason = ann.omission?.reason || "unsupported-semantic";
          const detail =
            ann.omission?.detail || "target does not support output retention";
          losses.push({
            path,
            field,
            reason,
            ...(detail ? { detail } : {}),
          });
        }
        return;
      }
      default:
        if (target === "responses") {
          return;
        }
        if (target === "anthropic" && ann.kind === NATIVE_KIND_DEFER_LOADING) {
          return;
        }
        if (ann.omission) {
          losses.push({
            path,
            field: ann.omission.field,
            reason: ann.omission.reason,
            ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
          });
        }
        return;
    }
  }
  if (ann.source !== NATIVE_SOURCE_ANTHROPIC) {
    losses.push({
      path,
      field: "source",
      reason: "unsupported-semantic",
      detail: `unsupported native option source "${ann.source}"`,
    });
    return;
  }

  switch (ann.kind) {
    case NATIVE_KIND_OUTPUT_EFFORT:
      return;
    case NATIVE_KIND_THINKING_MODE:
      if (hasActiveBudgetProj) {
        return;
      }
      if (ann.omission) {
        losses.push({
          path,
          field: ann.omission.field,
          reason: ann.omission.reason,
          ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
        });
      } else {
        losses.push({
          path,
          field: "type",
          reason: "unmapped-field",
        });
      }
      return;
    case NATIVE_KIND_THINKING_BUDGET:
      if (ann.projection?.origin === "budget") {
        if (ann.projection.effort === currentEffort) {
          if (ann.omission) {
            losses.push({
              path,
              field: ann.omission.field,
              reason: ann.omission.reason,
              ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
            });
          }
        }
        return;
      }
      if (ann.omission) {
        losses.push({
          path,
          field: ann.omission.field,
          reason: ann.omission.reason,
          ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
        });
      }
      return;
    case NATIVE_KIND_CACHE_CONTROL: {
      const field = ann.omission?.field || "cache_control";
      const reason = ann.omission?.reason || "unmapped-field";
      const detail = ann.omission?.detail || CACHE_CONTROL_LOSS_DETAIL;
      losses.push({
        path,
        field,
        reason,
        detail,
      });
      return;
    }
    case NATIVE_KIND_DEFER_LOADING: {
      if (target === "responses" || target === "anthropic") {
        return;
      }
      const field = ann.omission?.field || "defer_loading";
      const reason = ann.omission?.reason || "degraded";
      const detail = ann.omission?.detail || DEFER_LOADING_LOSS_DETAIL;
      losses.push({
        path,
        field,
        reason,
        detail,
      });
      return;
    }
    case NATIVE_KIND_EAGER_INPUT_STREAMING: {
      if (target === "anthropic") {
        return;
      }
      let isTrue = false;
      try {
        isTrue = JSON.parse(ann.value) === true;
      } catch {}
      if (isTrue) {
        const field = ann.omission?.field || "eager_input_streaming";
        const reason = ann.omission?.reason || "degraded";
        const detail =
          ann.omission?.detail || EAGER_INPUT_STREAMING_LOSS_DETAIL;
        losses.push({
          path,
          field,
          reason,
          detail,
        });
      }
      return;
    }
    case NATIVE_KIND_TOP_K: {
      const field = ann.omission?.field || "top_k";
      const reason = ann.omission?.reason || "unmapped-field";
      losses.push({
        path,
        field,
        reason,
        ...(ann.omission?.detail ? { detail: ann.omission.detail } : {}),
      });
      return;
    }
    case NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE: {
      const field = ann.omission?.field || "disable_parallel_tool_use";
      const reason = ann.omission?.reason || "unmapped-field";
      losses.push({
        path,
        field,
        reason,
        ...(ann.omission?.detail ? { detail: ann.omission.detail } : {}),
      });
      return;
    }
    case NATIVE_KIND_THINKING_DISPLAY:
    case NATIVE_KIND_CONTEXT_MANAGEMENT:
    case NATIVE_KIND_DOCUMENT_TITLE:
    case NATIVE_KIND_DOCUMENT_CONTEXT:
    case NATIVE_KIND_DOCUMENT_CITATIONS:
      if (ann.omission) {
        losses.push({
          path,
          field: ann.omission.field,
          reason: ann.omission.reason,
          ...(ann.omission.detail ? { detail: ann.omission.detail } : {}),
        });
      }
      return;
    default:
      losses.push({
        path,
        field: "kind",
        reason: "unsupported-semantic",
        detail: `unsupported native option kind "${ann.kind}"`,
      });
  }
}
