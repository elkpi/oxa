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
export const NATIVE_SOURCE_ANTHROPIC = "anthropic";

type HostType = "request" | "system" | "content" | "tool";

export function validateNativeRequest(request: Request): void {
  if (request.native) {
    validateAnnotations(request.native, "request", "request");
  }

  let budgetProjCount = 0;
  let explicitProjCount = 0;
  for (const n of request.native ?? []) {
    if (n.projection?.origin === "budget") {
      budgetProjCount++;
      if (budgetProjCount > 1) {
        throw new OxaError(
          "invalid-input",
          "ir: duplicate budget projection on request",
        );
      }
    } else if (n.projection?.origin === "explicit") {
      explicitProjCount++;
      if (explicitProjCount > 1) {
        throw new OxaError(
          "invalid-input",
          "ir: duplicate explicit projection on request",
        );
      }
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
    try {
      JSON.parse(ann.value);
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
        case NATIVE_KIND_THINKING_BUDGET:
        case NATIVE_KIND_THINKING_DISPLAY:
        case NATIVE_KIND_OUTPUT_EFFORT:
        case NATIVE_KIND_CONTEXT_MANAGEMENT:
          if (host !== "request") {
            throw new OxaError(
              "invalid-input",
              `${path}: anthropic ${ann.kind} is only allowed on Request`,
            );
          }
          break;
        case NATIVE_KIND_CACHE_CONTROL:
          break;
      }
    }
  }
}

function validateBlockNative(blk: Block, host: HostType, path: string): void {
  if (blk.native) {
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
  _target: string,
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
      );
    });
  });

  request.messages.forEach((msg, mIdx) => {
    msg.content.forEach((blk, cIdx) => {
      collectBlockOmissions(losses, blk, `messages[${mIdx}].content[${cIdx}]`);
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
      );
    });
  });

  return losses;
}

function collectBlockOmissions(losses: Loss[], blk: Block, path: string): void {
  blk.native?.forEach((ann, i) => {
    appendOmissionLoss(losses, ann, `${path}.native[${i}]`, false, undefined);
  });
  if (blk.type === "tool_result") {
    (blk as ToolResultBlock).content.forEach((child, k) => {
      collectBlockOmissions(losses, child, `${path}.content[${k}]`);
    });
  } else if (blk.type === "custom_tool_result") {
    const out = (blk as CustomToolResultBlock).output;
    if (Array.isArray(out)) {
      out.forEach((child, k) => {
        collectBlockOmissions(losses, child, `${path}.output.content[${k}]`);
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
): void {
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
    case NATIVE_KIND_THINKING_DISPLAY:
    case NATIVE_KIND_CACHE_CONTROL:
    case NATIVE_KIND_CONTEXT_MANAGEMENT:
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
