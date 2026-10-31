import { OxaError } from "../../error.js";
import {
  inspectWebSearchCall,
  validateWebSearchExtra,
} from "../../ir/web-search.js";
import {
  assertSourceMatches,
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  jsonText,
  parseJson,
  sourceTextOf,
  stringifyJson,
  type JsonObject,
  type JsonValue,
} from "../../json/index.js";
import type { ConversionResult, Loss, LossReason } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import {
  parseUsageInteger,
  validateRequest,
  omittedNativeRequestLosses,
  DEFER_LOADING_LOSS_DETAIL,
} from "../../ir/index.js";
import { auditWire, mergeWireLosses } from "../../json/wire-audit.js";
import { RESPONSES_REQUEST, RESPONSES_RESPONSE } from "./audit.js";
import type {
  ApplyPatchOperation,
  Block,
  ImageBlock,
  Message,
  ReasoningEffort,
  Request,
  Response,
  ResponseFormat,
  Tool,
  ToolChoice,
  ToolResultBlock,
  WebSearchCallBlock,
} from "../../ir/index.js";

function decodeWebSearchCall(
  item: JsonObject,
  path: string,
  losses: Loss[],
): WebSearchCallBlock {
  const id = nonEmptyString(item.id, `${path}.id`);
  const status = nonEmptyString(item.status, `${path}.status`);
  const { action, unknown } = inspectWebSearchCall(
    id,
    status,
    item.action,
    path,
  );
  for (const field of unknown)
    losses.push(loss(field.path, field.field, "unmapped-field"));
  const extra: Record<string, JsonValue> = Object.create(null) as Record<
    string,
    JsonValue
  >;
  for (const [key, value] of Object.entries(item)) {
    if (!["type", "id", "status", "action"].includes(key)) extra[key] = value;
  }
  return {
    type: "web_search_call",
    id,
    status,
    action,
    ...(Object.keys(extra).length ? { extra } : {}),
  };
}

export interface NonstreamOptions {
  readonly modelMapper?: ModelMapper;
}

export function decodeRequest(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const audit = auditWire(wire, RESPONSES_REQUEST);
  const result = decodeRequestObject(wire, options);
  return { value: result.value, losses: mergeWireLosses(result.losses, audit) };
}

export function decodeRequestJSON(
  raw: string,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  return decodeRequest(objectOrThrow(parseJson(raw), "request"), options);
}

function decodeRequestObject(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Request> {
  const losses: Loss[] = [];
  let effortValue: ReasoningEffort | undefined;
  let formatValue: ResponseFormat | undefined;
  let verbosityValue: "low" | "medium" | "high" | undefined;
  if (wire.text !== undefined && wire.text !== null) {
    const text = object(wire.text, "text");
    if (text.verbosity !== undefined && text.verbosity !== null) {
      const verbosity = string(text.verbosity, "text.verbosity");
      if (verbosity === "low" || verbosity === "medium" || verbosity === "high")
        verbosityValue = verbosity;
      else losses.push(loss("text.verbosity", "verbosity", "unmapped-value"));
    }
    if (text.format !== undefined)
      formatValue = decodeTextFormat(text.format, losses);
  }
  if (wire.reasoning !== undefined) {
    const reasoning = object(wire.reasoning, "reasoning");
    if (reasoning.effort !== undefined && reasoning.effort !== null) {
      const effort = string(reasoning.effort, "reasoning.effort");
      if (
        effort === "minimal" ||
        effort === "low" ||
        effort === "medium" ||
        effort === "high" ||
        effort === "xhigh" ||
        effort === "max"
      )
        effortValue = effort;
      else losses.push(loss("reasoning.effort", "effort", "unmapped-value"));
    }
    if (reasoning.summary !== undefined)
      losses.push(loss("reasoning.summary", "summary", "unmapped-field"));
  }
  if (wire.parallel_tool_calls !== undefined)
    losses.push(
      loss("parallel_tool_calls", "parallel_tool_calls", "unmapped-field"),
    );

  const system: {
    readonly type: "text";
    readonly text: string;
    readonly role?: "developer";
  }[] = [];
  if (wire.instructions !== undefined)
    system.push({
      type: "text",
      text: string(wire.instructions, "instructions"),
    });
  const seenNames = new Set<string>();
  const tools: Tool[] = [];
  for (const [index, entry] of optionalArray(wire.tools, "tools").entries()) {
    const decoded = decodeResponsesToolItem(
      entry,
      [],
      `tools[${index}]`,
      seenNames,
      losses,
    );
    tools.push(...decoded);
  }
  const toolChoice = decodeToolChoice(wire.tool_choice, losses);
  const messages: Message[] = [];
  if (typeof wire.input === "string") {
    messages.push({
      role: "user",
      content: [{ type: "text", text: wire.input }],
    });
  } else {
    const items = array(wire.input, "input");
    const {
      decodedCalls,
      decodedResults,
      dropped,
      losses: pairLosses,
    } = precheckApplyPatchPairs(items);
    losses.push(...pairLosses);

    for (let index = 0; index < items.length; ) {
      const item = object(items[index], `input[${index}]`);
      const type =
        item.type === undefined
          ? "message"
          : string(item.type, `input[${index}].type`);
      if (
        type === "function_call" ||
        type === "custom_tool_call" ||
        type === "apply_patch_call" ||
        type === "reasoning" ||
        type === "web_search_call" ||
        (type === "message" && item.role === "assistant")
      ) {
        const thinking: Block[] = [];
        const texts: Block[] = [];
        const calls: Block[] = [];
        while (index < items.length) {
          const current = object(items[index], `input[${index}]`);
          const currentType =
            current.type === undefined
              ? "message"
              : string(current.type, `input[${index}].type`);
          if (currentType === "reasoning") {
            const summary =
              current.summary === undefined
                ? []
                : array(current.summary, `input[${index}].summary`);
            for (
              let partIndex = 0;
              partIndex < summary.length;
              partIndex += 1
            ) {
              const partPath = `input[${index}].summary[${partIndex}]`;
              const part = object(summary[partIndex], partPath);
              if (part.type !== "output_text" && part.type !== "summary_text") {
                losses.push(loss(partPath, "type", "unsupported-semantic"));
                continue;
              }
              if (
                part.annotations !== undefined &&
                array(part.annotations, `${partPath}.annotations`).length > 0
              )
                losses.push(
                  loss(
                    `${partPath}.annotations`,
                    "annotations",
                    "unmapped-field",
                  ),
                );
              const text = string(part.text, `${partPath}.text`);
              if (text !== "")
                thinking.push({ type: "thinking", thinking: text });
            }
            index += 1;
            continue;
          }
          if (currentType === "web_search_call") {
            calls.push(decodeWebSearchCall(current, `input[${index}]`, losses));
            index += 1;
            continue;
          }
          if (currentType === "apply_patch_call") {
            if (dropped.has(index)) {
              index += 1;
              continue;
            }
            const block = decodedCalls.get(index);
            if (block !== undefined) {
              calls.push(block);
              index += 1;
              continue;
            }
            fail(`input[${index}]: unpaired apply_patch_call (INV-3)`);
          }
          if (currentType === "function_call") {
            const rawName = nonEmptyString(
              current.name,
              `input[${index}].name`,
            );
            const ns =
              typeof current.namespace === "string" ? current.namespace : "";
            const finalName =
              ns !== "" && !rawName.startsWith(ns + ".")
                ? `${ns}.${rawName}`
                : rawName;
            calls.push({
              type: "tool_use",
              id: nonEmptyString(current.call_id, `input[${index}].call_id`),
              name: finalName,
              input: jsonText(
                string(current.arguments, `input[${index}].arguments`),
              ),
            });
            index += 1;
            continue;
          }
          if (currentType === "custom_tool_call") {
            const rawName = nonEmptyString(
              current.name,
              `input[${index}].name`,
            );
            const ns =
              typeof current.namespace === "string" ? current.namespace : "";
            const finalName =
              ns !== "" && !rawName.startsWith(ns + ".")
                ? `${ns}.${rawName}`
                : rawName;
            calls.push({
              type: "custom_tool_use",
              id: nonEmptyString(current.call_id, `input[${index}].call_id`),
              name: finalName,
              input: string(current.input, `input[${index}].input`),
            });
            index += 1;
            continue;
          }
          if (currentType !== "message" || current.role !== "assistant") break;
          const decoded = decodeContent(
            current.content,
            `input[${index}].content`,
            losses,
          );
          texts.push(
            ...(decoded.length === 0
              ? [{ type: "text" as const, text: "" }]
              : decoded),
          );
          index += 1;
        }
        const content = [...thinking, ...texts, ...calls];
        if (content.length > 0) {
          messages.push({
            role: "assistant",
            content,
          });
        }
        continue;
      }
      if (
        type === "function_call_output" ||
        type === "custom_tool_call_output" ||
        type === "apply_patch_call_output"
      ) {
        const content: Block[] = [];
        while (index < items.length) {
          const current = object(items[index], `input[${index}]`);
          if (
            current.type !== "function_call_output" &&
            current.type !== "custom_tool_call_output" &&
            current.type !== "apply_patch_call_output"
          )
            break;
          if (current.type === "apply_patch_call_output") {
            if (dropped.has(index)) {
              index += 1;
              continue;
            }
            const block = decodedResults.get(index);
            if (block !== undefined) {
              content.push(block);
              index += 1;
              continue;
            }
            fail(`input[${index}]: unpaired apply_patch_call_output (INV-3)`);
          }
          const callId = nonEmptyString(
            current.call_id,
            `input[${index}].call_id`,
          );
          if (current.type === "custom_tool_call_output")
            content.push({
              type: "custom_tool_result",
              tool_use_id: callId,
              output: decodeCustomOutput(
                current.output,
                `input[${index}].output`,
                losses,
              ),
            });
          else
            content.push({
              type: "tool_result",
              tool_use_id: callId,
              content: [
                {
                  type: "text",
                  text: string(current.output, `input[${index}].output`),
                },
              ],
            });
          index += 1;
        }
        if (content.length > 0) {
          messages.push({ role: "user", content });
        }
        continue;
      }
      if (type === "message") {
        const role = string(item.role, `input[${index}].role`);
        const content = decodeContent(
          item.content,
          `input[${index}].content`,
          losses,
        );
        if (role === "system" || role === "developer") {
          for (let child = 0; child < content.length; child += 1) {
            const block = content[child]!;
            if (block.type === "text")
              system.push(
                role === "developer"
                  ? { ...block, role: "developer" as const }
                  : block,
              );
            else
              losses.push(
                loss(
                  `input[${index}].content[${child}]`,
                  "content",
                  "unsupported-semantic",
                ),
              );
          }
        } else if (role === "user")
          messages.push({
            role,
            content:
              content.length === 0 ? [{ type: "text", text: "" }] : content,
          });
        else fail(`input[${index}]: unknown role ${role}`);
        index += 1;
        continue;
      }
      if (type === "additional_tools") {
        const itemTools = array(item.tools, `input[${index}].tools`);
        if (itemTools.length === 0) {
          fail(
            `input[${index}]: additional_tools requires non-empty tools array`,
          );
        }
        for (const [tIdx, tEntry] of itemTools.entries()) {
          const decoded = decodeResponsesToolItem(
            tEntry,
            [],
            `input[${index}].tools[${tIdx}]`,
            seenNames,
            losses,
          );
          tools.push(...decoded);
        }
        index += 1;
        continue;
      }
      losses.push(loss(`input[${index}]`, "type", "unsupported-semantic"));
      index += 1;
    }
  }
  if (messages.length === 0 && system.length > 0) {
    messages.push({ role: "user", content: [{ type: "text", text: "" }] });
    losses.push({
      path: "input",
      field: "input",
      reason: "degraded",
      detail:
        "An empty user message was added because the IR requires at least one conversation message.",
    });
  }
  const params = {
    ...(wire.temperature === undefined
      ? {}
      : { temperature: number(wire.temperature, "temperature") }),
    ...(wire.top_p === undefined ? {} : { top_p: number(wire.top_p, "top_p") }),
    ...(wire.max_output_tokens === undefined
      ? {}
      : {
          max_tokens: whole(wire.max_output_tokens, "max_output_tokens"),
        }),
    ...(effortValue === undefined ? {} : { reasoning_effort: effortValue }),
    ...(verbosityValue === undefined ? {} : { verbosity: verbosityValue }),
    ...(formatValue === undefined ? {} : { response_format: formatValue }),
  };
  const request: Request = {
    model: mapModel(options.modelMapper, string(wire.model, "model")),
    ...(system.length === 0 ? {} : { system }),
    messages,
    ...(tools.length === 0 ? {} : { tools }),
    ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
    ...(Object.keys(params).length === 0 ? {} : { params }),
    ...stringMetadata(wire.metadata, "responses"),
  };
  validateRequest(request);
  return { value: request, losses };
}

function precheckApplyPatchPairs(items: readonly JsonValue[]): {
  decodedCalls: Map<number, Block>;
  decodedResults: Map<number, Block>;
  dropped: Set<number>;
  losses: Loss[];
} {
  const decodedCalls = new Map<number, Block>();
  const decodedResults = new Map<number, Block>();
  const dropped = new Set<number>();
  const losses: Loss[] = [];

  let i = 0;
  while (i < items.length) {
    const item = items[i];
    if (
      typeof item !== "object" ||
      item === null ||
      isJsonArray(item) ||
      isJsonNumber(item)
    ) {
      i++;
      continue;
    }
    if (item.type === "apply_patch_call_output") {
      fail(
        `input[${i}]: apply_patch tool result without preceding tool calls (INV-3)`,
      );
    }
    const type = item.type === undefined ? "message" : item.type;
    const isAssistant =
      (type === "message" && item.role === "assistant") ||
      type === "function_call" ||
      type === "custom_tool_call" ||
      type === "apply_patch_call" ||
      type === "reasoning" ||
      type === "web_search_call";

    if (!isAssistant) {
      i++;
      continue;
    }

    const calls: { callId: string; kind: string; index: number }[] = [];
    const seenCallIds = new Set<string>();
    let aStart = i;
    while (aStart < items.length) {
      const it = items[aStart];
      if (
        typeof it !== "object" ||
        it === null ||
        isJsonArray(it) ||
        isJsonNumber(it)
      )
        break;
      const t = it.type === undefined ? "message" : it.type;
      let kind = "";
      if (t === "function_call") {
        kind = "function";
      } else if (t === "custom_tool_call") {
        kind = "custom";
      } else if (t === "apply_patch_call") {
        kind = "apply_patch";
      } else if (
        t === "reasoning" ||
        t === "web_search_call" ||
        (t === "message" && it.role === "assistant")
      ) {
        // assistant content
      } else {
        break;
      }
      if (kind !== "") {
        const callId = string(it.call_id, `input[${aStart}].call_id`);
        if (kind === "apply_patch" && callId === "") {
          fail(
            `input[${aStart}]: apply_patch tool call requires nonempty call_id (INV-3)`,
          );
        }
        if (seenCallIds.has(callId)) {
          fail(
            `input[${aStart}]: duplicate call_id "${callId}" in turn (INV-3)`,
          );
        }
        seenCallIds.add(callId);
        calls.push({ callId, kind, index: aStart });
      }
      aStart++;
    }

    const results: { callId: string; kind: string; index: number }[] = [];
    let oStart = aStart;
    while (oStart < items.length) {
      const it = items[oStart];
      if (
        typeof it !== "object" ||
        it === null ||
        isJsonArray(it) ||
        isJsonNumber(it)
      )
        break;
      const t = it.type;
      let matchedKind = "";
      if (t === "function_call_output") {
        matchedKind = "function";
      } else if (t === "custom_tool_call_output") {
        matchedKind = "custom";
      } else if (t === "apply_patch_call_output") {
        matchedKind = "apply_patch";
      } else {
        break;
      }
      const callId = string(it.call_id, `input[${oStart}].call_id`);
      if (matchedKind === "apply_patch" && callId === "") {
        fail(
          `input[${oStart}]: apply_patch tool result requires nonempty call_id (INV-3)`,
        );
      }
      results.push({ callId, kind: matchedKind, index: oStart });
      oStart++;
    }

    const hasApplyPatch =
      calls.some((c) => c.kind === "apply_patch") ||
      results.some((r) => r.kind === "apply_patch");

    if (hasApplyPatch) {
      if (calls.length !== results.length) {
        fail(
          `tool calls count (${calls.length}) and results count (${results.length}) mismatch (INV-3)`,
        );
      }
      for (let k = 0; k < calls.length; k++) {
        if (
          calls[k]!.callId !== results[k]!.callId ||
          calls[k]!.kind !== results[k]!.kind
        ) {
          fail("tool result identity/type/order mismatch (INV-3)");
        }
      }
      for (let k = 0; k < calls.length; k++) {
        if (calls[k]!.kind !== "apply_patch") continue;
        const cIdx = calls[k]!.index;
        const oIdx = results[k]!.index;
        const cItem = object(items[cIdx], `input[${cIdx}]`);
        const oItem = object(items[oIdx], `input[${oIdx}]`);

        const callOpResult = decodeApplyPatchOperationWire(
          cItem.operation,
          `input[${cIdx}]`,
        );
        const callStatusResult = decodeApplyPatchCallStatusWire(
          cItem.status,
          `input[${cIdx}]`,
        );
        const resStatusResult = decodeApplyPatchResultStatusWire(
          oItem.status,
          `input[${oIdx}]`,
        );
        const resOutputResult = decodeApplyPatchResultOutputWire(
          oItem.output,
          `input[${oIdx}]`,
        );

        const hasCallLoss =
          callOpResult.loss !== undefined ||
          callStatusResult.loss !== undefined;
        const hasResLoss = resStatusResult.loss !== undefined;

        if (hasCallLoss || hasResLoss) {
          dropped.add(cIdx);
          dropped.add(oIdx);
          if (hasCallLoss) {
            if (callOpResult.loss) losses.push(callOpResult.loss);
            if (callStatusResult.loss) losses.push(callStatusResult.loss);
          } else {
            losses.push(loss(`input[${cIdx}]`, "type", "unsupported-semantic"));
          }
          if (hasResLoss) {
            if (resStatusResult.loss) losses.push(resStatusResult.loss);
          } else {
            losses.push(loss(`input[${oIdx}]`, "type", "unsupported-semantic"));
          }
        } else {
          decodedCalls.set(cIdx, {
            type: "apply_patch_tool_use",
            id: string(cItem.call_id, `input[${cIdx}].call_id`),
            status: callStatusResult.status!,
            operation: callOpResult.operation!,
          });
          decodedResults.set(oIdx, {
            type: "apply_patch_tool_result",
            tool_use_id: string(oItem.call_id, `input[${oIdx}].call_id`),
            status: resStatusResult.status!,
            ...(resOutputResult.output !== undefined
              ? { output: resOutputResult.output }
              : {}),
          });
        }
      }
    }

    i = oStart;
  }

  return { decodedCalls, decodedResults, dropped, losses };
}

function decodeApplyPatchOperationWire(
  wire: JsonValue | undefined,
  path: string,
): { operation?: ApplyPatchOperation; loss?: Loss } {
  if (
    wire === undefined ||
    wire === null ||
    typeof wire !== "object" ||
    isJsonArray(wire) ||
    isJsonNumber(wire)
  ) {
    fail(`${path}.operation is required`);
  }
  const type = string(wire.type, `${path}.operation.type`);
  switch (type) {
    case "create_file":
    case "update_file":
      return {
        operation: {
          type,
          path: string(wire.path, `${path}.operation.path`),
          diff: string(wire.diff, `${path}.operation.diff`),
        },
      };
    case "delete_file":
      return {
        operation: {
          type,
          path: string(wire.path, `${path}.operation.path`),
        },
      };
    default:
      return { loss: loss(path, "operation", "unsupported-semantic") };
  }
}

function decodeApplyPatchCallStatusWire(
  status: JsonValue | undefined,
  path: string,
): { status?: "in_progress" | "completed"; loss?: Loss } {
  if (typeof status !== "string" || status === "") {
    fail(`${path}.status is required`);
  }
  if (status === "in_progress" || status === "completed") {
    return { status };
  }
  return { loss: loss(`${path}.status`, "status", "unmapped-value") };
}

function decodeApplyPatchResultStatusWire(
  status: JsonValue | undefined,
  path: string,
): { status?: "completed" | "failed"; loss?: Loss } {
  if (typeof status !== "string" || status === "") {
    fail(`${path}.status is required`);
  }
  if (status === "completed" || status === "failed") {
    return { status };
  }
  return { loss: loss(`${path}.status`, "status", "unmapped-value") };
}

function decodeApplyPatchResultOutputWire(
  output: JsonValue | undefined,
  path: string,
): { output?: string } {
  if (output === undefined || output === null) {
    return {};
  }
  if (typeof output === "string") {
    return { output };
  }
  fail(`${path}.output must be a string`);
}

function encodeApplyPatchOperation(op: ApplyPatchOperation): JsonObject {
  switch (op.type) {
    case "create_file":
    case "update_file":
      return { type: op.type, path: op.path, diff: op.diff };
    case "delete_file":
      return { type: op.type, path: op.path };
    default:
      fail("unknown apply_patch operation");
  }
}

export function decodeResponse(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const audit = auditWire(wire, RESPONSES_RESPONSE);
  const result = decodeResponseObject(wire, options);
  return { value: result.value, losses: mergeWireLosses(result.losses, audit) };
}

export function decodeResponseJSON(
  raw: string,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  return decodeResponse(objectOrThrow(parseJson(raw), "response"), options);
}

function decodeResponseObject(
  wire: JsonObject,
  options: NonstreamOptions = {},
): ConversionResult<Response> {
  const losses: Loss[] = [];
  const texts: Block[] = [];
  const calls: Block[] = [];
  for (let index = 0; index < array(wire.output, "output").length; index += 1) {
    const item = object(
      array(wire.output, "output")[index],
      `output[${index}]`,
    );
    if (item.type === "message") {
      const content = array(item.content, `output[${index}].content`);
      for (let child = 0; child < content.length; child += 1) {
        const part = object(
          content[child],
          `output[${index}].content[${child}]`,
        );
        if (part.type !== "output_text") {
          losses.push(
            loss(
              `output[${index}].content[${child}]`,
              "type",
              "unsupported-semantic",
            ),
          );
          continue;
        }
        if (
          part.annotations !== undefined &&
          array(
            part.annotations,
            `output[${index}].content[${child}].annotations`,
          ).length > 0
        )
          losses.push(
            loss(
              `output[${index}].content[${child}].annotations`,
              "annotations",
              "unmapped-field",
            ),
          );
        texts.push({
          type: "text",
          text: string(part.text, `output[${index}].content[${child}].text`),
        });
      }
    } else if (item.type === "reasoning") {
      const summary = item.summary;
      const parts =
        summary === undefined || summary === null
          ? []
          : array(summary, `output[${index}].summary`);
      for (let partIndex = 0; partIndex < parts.length; partIndex += 1) {
        const partPath = `output[${index}].summary[${partIndex}]`;
        const part = object(parts[partIndex], partPath);
        if (part.type !== "output_text" && part.type !== "summary_text") {
          losses.push(loss(partPath, "type", "unsupported-semantic"));
          continue;
        }
        if (
          part.annotations !== undefined &&
          array(part.annotations, `${partPath}.annotations`).length > 0
        )
          losses.push(
            loss(`${partPath}.annotations`, "annotations", "unmapped-field"),
          );
        texts.push({
          type: "thinking",
          thinking: string(part.text, `${partPath}.text`),
        });
      }
      if (parts.length === 0)
        losses.push({
          path: `output[${index}]`,
          field: "type",
          reason: "unsupported-semantic",
          detail:
            "Responses reasoning output item with empty summary carries no convertible content",
        });
      if (item.encrypted_content !== undefined)
        losses.push(
          loss(
            `output[${index}].encrypted_content`,
            "encrypted_content",
            "unmapped-field",
          ),
        );
    } else if (item.type === "apply_patch_call") {
      nonEmptyString(item.id, `output[${index}].id`);
      const statusResult = decodeApplyPatchCallStatusWire(
        item.status,
        `output[${index}]`,
      );
      if (statusResult.loss) {
        losses.push(statusResult.loss);
        continue;
      }
      const opResult = decodeApplyPatchOperationWire(
        item.operation,
        `output[${index}]`,
      );
      if (opResult.loss) {
        losses.push(opResult.loss);
        continue;
      }
      calls.push({
        type: "apply_patch_tool_use",
        id: nonEmptyString(item.call_id, `output[${index}].call_id`),
        status: statusResult.status!,
        operation: opResult.operation!,
      });
    } else if (item.type === "apply_patch_call_output") {
      nonEmptyString(item.id, `output[${index}].id`);
      const statusResult = decodeApplyPatchResultStatusWire(
        item.status,
        `output[${index}]`,
      );
      if (statusResult.loss) {
        losses.push(statusResult.loss);
        continue;
      }
      const outResult = decodeApplyPatchResultOutputWire(
        item.output,
        `output[${index}]`,
      );
      calls.push({
        type: "apply_patch_tool_result",
        tool_use_id: nonEmptyString(item.call_id, `output[${index}].call_id`),
        status: statusResult.status!,
        ...(outResult.output !== undefined ? { output: outResult.output } : {}),
      });
    } else if (item.type === "web_search_call") {
      calls.push(decodeWebSearchCall(item, `output[${index}]`, losses));
    } else if (item.type === "function_call") {
      const rawName = nonEmptyString(item.name, `output[${index}].name`);
      const ns =
        typeof item.namespace === "string" && item.namespace
          ? item.namespace
          : "";
      const finalName =
        ns && !rawName.startsWith(ns + ".") ? `${ns}.${rawName}` : rawName;
      calls.push({
        type: "tool_use",
        id: nonEmptyString(item.call_id, `output[${index}].call_id`),
        name: finalName,
        input: jsonText(string(item.arguments, `output[${index}].arguments`)),
      });
    } else if (item.type === "custom_tool_call") {
      const rawName = nonEmptyString(item.name, `output[${index}].name`);
      const ns =
        typeof item.namespace === "string" && item.namespace
          ? item.namespace
          : "";
      const finalName =
        ns && !rawName.startsWith(ns + ".") ? `${ns}.${rawName}` : rawName;
      calls.push({
        type: "custom_tool_use",
        id: nonEmptyString(item.call_id, `output[${index}].call_id`),
        name: finalName,
        input: string(item.input, `output[${index}].input`),
      });
    } else if (item.type === "custom_tool_call_output") {
      calls.push({
        type: "custom_tool_result",
        tool_use_id: nonEmptyString(item.call_id, `output[${index}].call_id`),
        output: decodeCustomOutput(
          item.output,
          `output[${index}].output`,
          losses,
        ),
      });
    } else {
      losses.push(loss(`output[${index}]`, "type", "unsupported-semantic"));
    }
  }
  const status = string(wire.status, "status");
  let stopReason: Response["stop_reason"];
  if (wire.error !== undefined && wire.error !== null) {
    stopReason = "other";
    losses.push(loss("error", "error", "unsupported-semantic"));
  } else if (status === "completed") {
    stopReason = calls.some(
      (block) =>
        block.type === "tool_use" ||
        block.type === "custom_tool_use" ||
        block.type === "apply_patch_tool_use",
    )
      ? "tool_use"
      : "end_turn";
  } else if (status === "incomplete") {
    const details =
      wire.incomplete_details === undefined || wire.incomplete_details === null
        ? undefined
        : object(wire.incomplete_details, "incomplete_details");
    if (details?.reason === "max_output_tokens") stopReason = "max_tokens";
    else {
      stopReason = "other";
      losses.push(
        loss("incomplete_details.reason", "reason", "unmapped-value"),
      );
    }
  } else if (status === "failed") {
    stopReason = "other";
    losses.push(loss("error", "error", "unsupported-semantic"));
  } else fail(`status ${status} has no IR equivalent`);
  const usage =
    wire.usage === undefined || wire.usage === null
      ? { input_tokens: 0n, output_tokens: 0n }
      : (() => {
          const value = object(wire.usage, "usage");
          const inputDetails = value.input_token_details;
          const outputDetails = value.output_token_details;
          return {
            input_tokens: whole(value.input_tokens, "usage.input_tokens"),
            output_tokens: whole(value.output_tokens, "usage.output_tokens"),
            ...(inputDetails === undefined || inputDetails === null
              ? {}
              : {
                  input_tokens_details: {
                    cached_tokens: parseUsageInteger(
                      whole(
                        object(inputDetails, "usage.input_token_details")
                          .cached_tokens,
                        "usage.input_token_details.cached_tokens",
                      ),
                      "usage.input_token_details.cached_tokens",
                    ),
                  },
                }),
            ...(outputDetails === undefined || outputDetails === null
              ? {}
              : {
                  output_tokens_details: {
                    reasoning_tokens: parseUsageInteger(
                      whole(
                        object(outputDetails, "usage.output_token_details")
                          .reasoning_tokens,
                        "usage.output_token_details.reasoning_tokens",
                      ),
                      "usage.output_token_details.reasoning_tokens",
                    ),
                  },
                }),
          };
        })();
  return {
    value: {
      id: string(wire.id, "id"),
      model: mapModel(options.modelMapper, string(wire.model, "model")),
      content: [...texts, ...calls],
      stop_reason: stopReason,
      usage,
    },
    losses,
  };
}

export function encodeRequest(
  request: Request,
  options: NonstreamOptions = {},
): ConversionResult<JsonObject> {
  validateRequest(request);
  const losses: Loss[] = [];
  const items: JsonValue[] = [];
  let requestOrdinal = 0;
  for (let index = 0; index < request.messages.length; index += 1) {
    const message = request.messages[index]!;
    if (message.role === "assistant") {
      let text = "";
      let hasText = false;
      const thinking: string[] = [];
      const calls: JsonValue[] = [];
      for (let child = 0; child < message.content.length; child += 1) {
        const block = message.content[child]!;
        if (block.type === "text") {
          text += block.text;
          hasText = true;
          if (block.citations && block.citations.length > 0) {
            losses.push({
              path: `messages[${index}].content[${child}].citations`,
              field: "citations",
              reason: "unsupported-semantic",
              detail:
                "anthropic text citations; the target face cannot preserve source locations and replay metadata",
            });
          }
        } else if (block.type === "redacted_thinking") {
          losses.push({
            path: `messages[${index}].content[${child}]`,
            field: "type",
            reason: "unsupported-semantic",
            detail:
              "anthropic encrypted thinking replay state; the target face cannot preserve this block",
          });
        } else if (block.type === "thinking") {
          thinking.push(block.thinking);
          if (block.signature !== undefined)
            losses.push(
              loss(
                `messages[${index}].content[${child}].signature`,
                "signature",
                "unmapped-field",
              ),
            );
        } else if (block.type === "apply_patch_tool_use") {
          calls.push({
            type: "apply_patch_call",
            call_id: block.id,
            status: block.status,
            operation: encodeApplyPatchOperation(block.operation),
          });
        } else if (block.type === "web_search_call") {
          inspectWebSearchCall(block.id, block.status, block.action);
          validateWebSearchExtra(block.extra);
          calls.push({
            ...block.extra,
            type: "web_search_call",
            id: block.id,
            status: block.status,
            action: block.action,
          });
        } else if (block.type === "tool_use") {
          const dotIdx = block.name.indexOf(".");
          calls.push({
            type: "function_call",
            call_id: block.id,
            name: dotIdx > 0 ? block.name.slice(dotIdx + 1) : block.name,
            ...(dotIdx > 0 ? { namespace: block.name.slice(0, dotIdx) } : {}),
            arguments: block.input,
          });
        } else if (block.type === "custom_tool_use") {
          const dotIdx = block.name.indexOf(".");
          calls.push({
            type: "custom_tool_call",
            call_id: block.id,
            name: dotIdx > 0 ? block.name.slice(dotIdx + 1) : block.name,
            ...(dotIdx > 0 ? { namespace: block.name.slice(0, dotIdx) } : {}),
            input: block.input,
          });
        } else
          losses.push(
            loss(
              `messages[${index}].content[${child}]`,
              "content",
              "unsupported-semantic",
            ),
          );
      }
      if (thinking.length > 0) {
        requestOrdinal += 1;
        items.push({
          type: "reasoning",
          id: `rs_abc${String(123 + 333 * (requestOrdinal - 1)).padStart(3, "0")}`,
          summary: thinking.map((entry) => ({
            type: "output_text",
            text: entry,
            annotations: [],
          })),
        });
      }
      if (hasText) items.push({ role: "assistant", content: text });
      items.push(...calls);
      continue;
    }
    encodeUserMessage(items, message, index, losses);
  }
  const systemPlan = encodeSystemPlan(request.system);
  if (systemPlan.items.length > 0) items.unshift(...systemPlan.items);
  const shorthand =
    (request.system === undefined || request.system.length === 0) &&
    items.length === 1 &&
    object(items[0], "input[0]").role === "user" &&
    typeof object(items[0], "input[0]").content === "string"
      ? object(items[0], "input[0]").content
      : undefined;
  const tools = request.tools
    ? encodeResponsesGroupedTools(request.tools, losses)
    : [];
  const allToolsDropped =
    (request.tools?.length ?? 0) > 0 && tools.length === 0;
  const choiceName =
    request.tool_choice?.mode === "tool" && "name" in request.tool_choice
      ? request.tool_choice.name
      : undefined;
  const nativeNamedChoiceDropped =
    choiceName !== undefined &&
    ((request.tool_choice?.mode === "tool" &&
      request.tool_choice.tool_type === "native") ||
      request.tools?.some(
        (t) => t.type === "native" && t.name === choiceName,
      )) &&
    !request.tools?.some(
      (t) =>
        t.type === "native" && t.name === choiceName && t.source === "openai",
    );
  const toolChoice =
    request.tool_choice?.mode === "any" && allToolsDropped
      ? (losses.push(
          loss("tool_choice", "tool_choice", "unsupported-semantic"),
        ),
        undefined)
      : nativeNamedChoiceDropped
        ? (losses.push({
            path: "tool_choice",
            field: "tool_choice",
            reason: "unsupported-semantic",
            detail: `native tool choice "${choiceName}" cannot be represented by the target protocol`,
          }),
          undefined)
        : encodeToolChoice(request.tool_choice, losses);
  const params = request.params;
  const format = params?.response_format;
  const unnamedSchema = format?.type === "json_schema" && !format.name;
  if (unnamedSchema)
    losses.push(loss("params.response_format", "name", "unsupported-semantic"));
  if (params?.stop_sequences !== undefined && params.stop_sequences.length > 0)
    losses.push(
      loss("params.stop_sequences", "stop_sequences", "unmapped-field"),
    );
  const omissionLosses = omittedNativeRequestLosses(request, "responses");
  losses.push(...omissionLosses);
  const storeAnn = request.native?.find(
    (n) => n.source === "openai" && n.kind === "store",
  );
  let storeVal: boolean | null | undefined = undefined;
  if (storeAnn) {
    try {
      const parsed = JSON.parse(storeAnn.value);
      if (typeof parsed === "boolean" || parsed === null) {
        storeVal = parsed;
      }
    } catch {}
  }
  return {
    value: {
      model: mapModel(options.modelMapper, request.model),
      input: shorthand ?? items,
      ...(systemPlan.instructions === undefined
        ? {}
        : { instructions: systemPlan.instructions }),
      ...(storeVal !== undefined ? { store: storeVal } : {}),
      ...(params?.temperature === undefined
        ? {}
        : { temperature: fromValue(params.temperature) }),
      ...(params?.top_p === undefined
        ? {}
        : { top_p: fromValue(params.top_p) }),
      ...(params?.max_tokens === undefined
        ? {}
        : { max_output_tokens: integer(params.max_tokens) }),
      ...(params?.reasoning_effort === undefined
        ? {}
        : { reasoning: { effort: params.reasoning_effort } }),
      ...(params?.verbosity === undefined &&
      (format === undefined || unnamedSchema)
        ? {}
        : {
            text: {
              ...(params?.verbosity === undefined
                ? {}
                : { verbosity: params.verbosity }),
              ...(format === undefined || unnamedSchema
                ? {}
                : { format: encodeTextFormat(format) }),
            },
          }),
      ...(tools.length === 0 ? {} : { tools }),
      ...(toolChoice === undefined ? {} : { tool_choice: toolChoice }),
      ...(request.metadata === undefined ? {} : { metadata: request.metadata }),
    },
    losses,
  };
}

export function encodeResponse(
  response: Response,
  options: NonstreamOptions = {},
): ConversionResult<JsonObject> {
  const losses: Loss[] = [];
  let text = "";
  let hasText = false;
  const thinking: string[] = [];
  const calls: JsonValue[] = [];
  let apcIndex = 0;
  let apcoIndex = 0;
  for (let index = 0; index < response.content.length; index += 1) {
    const block = response.content[index]!;
    if (block.type === "text") {
      text += block.text;
      hasText = true;
      if (block.citations && block.citations.length > 0) {
        losses.push({
          path: `content[${index}].citations`,
          field: "citations",
          reason: "unsupported-semantic",
          detail:
            "anthropic text citations; the target face cannot preserve source locations and replay metadata",
        });
      }
    } else if (block.type === "redacted_thinking") {
      losses.push({
        path: `content[${index}]`,
        field: "type",
        reason: "unsupported-semantic",
        detail:
          "anthropic encrypted thinking replay state; the target face cannot preserve this block",
      });
    } else if (block.type === "thinking") {
      thinking.push(block.thinking);
      if (block.signature !== undefined)
        losses.push(
          loss(`content[${index}].signature`, "signature", "unmapped-field"),
        );
    } else if (block.type === "apply_patch_tool_use") {
      calls.push({
        type: "apply_patch_call",
        id: `apc_${apcIndex}`,
        status: block.status,
        call_id: block.id,
        operation: encodeApplyPatchOperation(block.operation),
      });
      apcIndex += 1;
    } else if (block.type === "apply_patch_tool_result") {
      calls.push({
        type: "apply_patch_call_output",
        id: `apco_${apcoIndex}`,
        status: block.status,
        call_id: block.tool_use_id,
        ...(block.output !== undefined ? { output: block.output } : {}),
      });
      apcoIndex += 1;
    } else if (block.type === "web_search_call") {
      inspectWebSearchCall(block.id, block.status, block.action);
      validateWebSearchExtra(block.extra);
      calls.push({
        ...block.extra,
        type: "web_search_call",
        id: block.id,
        status: block.status,
        action: block.action,
      });
    } else if (block.type === "tool_use") {
      const dotIdx = block.name.indexOf(".");
      calls.push({
        type: "function_call",
        id: "fc_abc123",
        status: "completed",
        call_id: block.id,
        name: dotIdx > 0 ? block.name.slice(dotIdx + 1) : block.name,
        ...(dotIdx > 0 ? { namespace: block.name.slice(0, dotIdx) } : {}),
        arguments: block.input,
      });
    } else if (block.type === "custom_tool_use") {
      const dotIdx = block.name.indexOf(".");
      calls.push({
        type: "custom_tool_call",
        id: "ctc_abc123",
        call_id: block.id,
        name: dotIdx > 0 ? block.name.slice(dotIdx + 1) : block.name,
        ...(dotIdx > 0 ? { namespace: block.name.slice(0, dotIdx) } : {}),
        input: block.input,
      });
    } else if (block.type === "custom_tool_result") {
      calls.push({
        type: "custom_tool_call_output",
        id: "ctco_abc123",
        call_id: block.tool_use_id,
        output: encodeCustomOutput(
          block.output,
          `content[${index}].output`,
          losses,
        ),
      });
    } else
      losses.push(loss(`content[${index}]`, "content", "unsupported-semantic"));
  }
  const output: JsonValue[] = [];
  if (thinking.length > 0)
    output.push({
      type: "reasoning",
      id: "rs_abc123",
      summary: thinking.map((entry) => ({
        type: "output_text",
        text: entry,
        annotations: [],
      })),
    });
  if (hasText || response.content.length === 0)
    output.push({
      type: "message",
      id: "msg_abc123",
      status: "completed",
      role: "assistant",
      content: [{ type: "output_text", text, annotations: [] }],
    });
  output.push(...calls);
  let status = "completed";
  let incompleteDetails: JsonObject | undefined;
  let error: JsonObject | undefined;
  switch (response.stop_reason) {
    case "end_turn":
    case "tool_use":
      break;
    case "max_tokens":
      status = "incomplete";
      incompleteDetails = { reason: "max_output_tokens" };
      break;
    case "stop_sequence":
      losses.push(loss("", "stop_sequence", "unmapped-value"));
      break;
    case "refusal":
      status = "failed";
      error = { code: "refusal", message: "" };
      break;
    default:
      fail(`stop reason ${response.stop_reason} is unsupported`);
  }
  return {
    value: {
      id: response.id,
      object: "response",
      status,
      ...(error === undefined ? {} : { error }),
      ...(incompleteDetails === undefined
        ? {}
        : { incomplete_details: incompleteDetails }),
      model: mapModel(options.modelMapper, response.model),
      output,
      usage: {
        input_tokens: integer(response.usage.input_tokens),
        output_tokens: integer(response.usage.output_tokens),
        total_tokens: integer(
          response.usage.input_tokens + response.usage.output_tokens,
        ),
        ...(response.usage.input_tokens_details === undefined
          ? {}
          : {
              input_token_details: {
                cached_tokens: integer(
                  response.usage.input_tokens_details.cached_tokens,
                ),
              },
            }),
        ...(response.usage.output_tokens_details === undefined
          ? {}
          : {
              output_token_details: {
                reasoning_tokens: integer(
                  response.usage.output_tokens_details.reasoning_tokens,
                ),
              },
            }),
      },
    },
    losses,
  };
}

function encodeUserMessage(
  items: JsonValue[],
  message: Message,
  messageIndex: number,
  losses: Loss[],
): void {
  const normal: Block[] = [];
  const results: {
    readonly block:
      | ToolResultBlock
      | Extract<Block, { type: "custom_tool_result" }>
      | Extract<Block, { type: "apply_patch_tool_result" }>;
    readonly index: number;
  }[] = [];
  let firstNormal = -1;
  let lastResult = -1;
  for (let index = 0; index < message.content.length; index += 1) {
    const block = message.content[index]!;
    if (
      block.type === "tool_result" ||
      block.type === "custom_tool_result" ||
      block.type === "apply_patch_tool_result"
    ) {
      results.push({ block, index });
      lastResult = index;
    } else {
      if (firstNormal < 0) firstNormal = index;
      normal.push(block);
    }
  }
  if (firstNormal >= 0 && firstNormal < lastResult)
    losses.push(loss(`messages[${messageIndex}]`, "ordering", "degraded"));
  for (const result of results) {
    if (result.block.type === "apply_patch_tool_result") {
      items.push({
        type: "apply_patch_call_output",
        call_id: result.block.tool_use_id,
        status: result.block.status,
        ...(result.block.output !== undefined
          ? { output: result.block.output }
          : {}),
      });
      continue;
    }
    if (result.block.type === "custom_tool_result") {
      items.push({
        type: "custom_tool_call_output",
        call_id: result.block.tool_use_id,
        output: encodeCustomOutput(
          result.block.output,
          `messages[${messageIndex}].content[${result.index}].output`,
          losses,
        ),
      });
      continue;
    }
    let output = "";
    for (let index = 0; index < result.block.content.length; index += 1) {
      const block = result.block.content[index]!;
      if (block.type === "text") output += block.text;
      else
        losses.push(
          loss(
            `messages[${messageIndex}].content[${result.index}].content[${index}]`,
            "content",
            "unsupported-semantic",
          ),
        );
    }
    if (result.block.is_error === true)
      losses.push(
        loss(
          `messages[${messageIndex}].content[${result.index}].is_error`,
          "is_error",
          "unmapped-field",
        ),
      );
    items.push({
      type: "function_call_output",
      call_id: result.block.tool_use_id,
      output,
    });
  }
  if (normal.length > 0 || results.length === 0)
    items.push({
      role: "user",
      content: encodeUserContent(
        normal,
        `messages[${messageIndex}].content`,
        losses,
      ),
    });
}

function decodeCustomToolFormat(
  value: JsonValue,
  losses: Loss[],
  path: string,
): import("../../ir/index.js").CustomToolFormat | undefined {
  const format = object(value, path);
  if (format.type === "text") return { type: "text" };
  if (format.type !== "grammar") {
    losses.push(loss(`${path}.type`, "type", "unsupported-semantic"));
    return undefined;
  }
  const syntax = string(format.syntax, `${path}.syntax`);
  if (syntax !== "lark" && syntax !== "regex") {
    losses.push(loss(`${path}.syntax`, "syntax", "unsupported-semantic"));
    return undefined;
  }
  return {
    type: "grammar",
    syntax,
    definition: string(format.definition, `${path}.definition`),
  };
}
function encodeCustomToolFormat(
  format: import("../../ir/index.js").CustomToolFormat,
): JsonObject {
  return format.type === "text"
    ? { type: "text" }
    : { type: "grammar", syntax: format.syntax, definition: format.definition };
}
function decodeCustomOutput(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): string | import("../../ir/index.js").CustomToolResultOutput {
  if (typeof value === "string") return value;
  const parts = array(value, path);
  const output: (Extract<Block, { type: "text" }> | ImageBlock)[] = [];
  for (let i = 0; i < parts.length; i++) {
    const part = object(parts[i], `${path}[${i}]`);
    if (part.type === "input_text") {
      output.push({
        type: "text",
        text: string(part.text, `${path}[${i}].text`),
      });
      continue;
    }
    if (part.type === "input_image") {
      const raw = part.image_url;
      const url =
        typeof raw === "string"
          ? raw
          : string(
              object(raw, `${path}[${i}].image_url`).url,
              `${path}[${i}].image_url.url`,
            );
      const image = decodeImage(url);
      if (image === undefined)
        losses.push(
          loss(`${path}[${i}].image_url`, "image_url", "unsupported-semantic"),
        );
      else output.push(image);
      if (part.detail !== undefined)
        losses.push(loss(`${path}[${i}].detail`, "detail", "unmapped-field"));
      continue;
    }
    losses.push({
      path: `${path}[${i}]`,
      field: "type",
      reason: "unsupported-semantic",
      detail: "custom tool output content is not supported",
    });
  }
  return output;
}
function encodeCustomOutput(
  value: string | import("../../ir/index.js").CustomToolResultOutput,
  path: string,
  losses: Loss[],
): JsonValue {
  if (typeof value === "string") return value;
  return value.flatMap<JsonValue>((part, i) => {
    if (part.type === "text") return [{ type: "input_text", text: part.text }];
    const image = encodeImage(part);
    if (image === undefined) {
      losses.push(loss(`${path}[${i}]`, "image", "unsupported-semantic"));
      return [];
    }
    return [{ type: "input_image", image_url: image }];
  });
}

function decodeContent(
  value: JsonValue | undefined,
  path: string,
  losses: Loss[],
): Block[] {
  if (value === undefined || value === null)
    return [{ type: "text", text: "" }];
  if (typeof value === "string") return [{ type: "text", text: value }];
  const blocks: Block[] = [];
  const parts = array(value, path);
  for (let index = 0; index < parts.length; index += 1) {
    const part = object(parts[index], `${path}[${index}]`);
    if (part.type === "input_text" || part.type === "output_text")
      blocks.push({
        type: "text",
        text:
          part.text === undefined
            ? ""
            : string(part.text, `${path}[${index}].text`),
      });
    else if (part.type === "input_image") {
      const image = decodeImage(
        string(part.image_url, `${path}[${index}].image_url`),
      );
      if (image === undefined)
        losses.push(
          loss(
            `${path}[${index}].image_url`,
            "image_url",
            "unsupported-semantic",
          ),
        );
      else blocks.push(image);
    } else
      losses.push(loss(`${path}[${index}]`, "type", "unsupported-semantic"));
  }
  return blocks;
}

function encodeUserContent(
  blocks: readonly Block[],
  path: string,
  losses: Loss[],
): JsonValue {
  let text = "";
  let textBlocks = 0;
  let otherBlocks = 0;
  const parts: JsonValue[] = [];
  for (let index = 0; index < blocks.length; index += 1) {
    const block = blocks[index]!;
    if (block.type === "text") {
      text += block.text;
      textBlocks += 1;
      parts.push({ type: "input_text", text: block.text });
      if (block.citations && block.citations.length > 0) {
        losses.push({
          path: `${path}[${index}].citations`,
          field: "citations",
          reason: "unsupported-semantic",
          detail:
            "anthropic text citations; the target face cannot preserve source locations and replay metadata",
        });
      }
    } else if (block.type === "redacted_thinking") {
      otherBlocks += 1;
      losses.push({
        path: `${path}[${index}]`,
        field: "type",
        reason: "unsupported-semantic",
        detail:
          "anthropic encrypted thinking replay state; the target face cannot preserve this block",
      });
    } else if (block.type === "image") {
      const image = encodeImage(block);
      if (image === undefined)
        losses.push(loss(`${path}[${index}]`, "image", "unsupported-semantic"));
      else {
        otherBlocks += 1;
        parts.push({ type: "input_image", image_url: image });
      }
    } else {
      otherBlocks += 1;
      losses.push(loss(`${path}[${index}]`, "content", "unsupported-semantic"));
    }
  }
  return textBlocks <= 1 && otherBlocks === 0 ? text : parts;
}

function decodeImage(value: string): ImageBlock | undefined {
  if (value.startsWith("https:")) {
    try {
      const url = new URL(value);
      if (url.protocol === "https:" && url.host !== "")
        return { type: "image", url: value };
    } catch {}
    return undefined;
  }
  if (!value.startsWith("data:")) return undefined;
  const comma = value.indexOf(",");
  if (comma < 0) return undefined;
  const metadata = value.slice(5, comma);
  if (!metadata.endsWith(";base64")) return undefined;
  const mediaType = metadata.slice(0, -7);
  if (!mediaType.toLowerCase().startsWith("image/") || mediaType === "image/")
    return undefined;
  return {
    type: "image",
    media_type: mediaType,
    data: value.slice(comma + 1),
  };
}

function encodeImage(image: ImageBlock): string | undefined {
  if (image.data !== undefined && image.url !== undefined) return undefined;
  if (image.data !== undefined) {
    if (
      image.media_type === undefined ||
      !image.media_type.toLowerCase().startsWith("image/") ||
      image.media_type === "image/"
    )
      return undefined;
    return `data:${image.media_type};base64,${image.data}`;
  }
  if (image.url !== undefined) {
    try {
      const url = new URL(image.url);
      if (url.protocol === "https:" && url.host !== "") return image.url;
    } catch {}
  }
  return undefined;
}

function decodeToolChoice(
  value: JsonValue | undefined,
  losses: Loss[],
): ToolChoice | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string") {
    if (value === "auto" || value === "none") return { mode: value };
    if (value === "required") return { mode: "any" };
  } else if (
    typeof value === "object" &&
    !isJsonArray(value) &&
    !isJsonNumber(value)
  ) {
    if (value.type === "apply_patch") {
      return { mode: "tool", tool_type: "apply_patch" };
    }
    const ns = typeof value.namespace === "string" ? value.namespace : "";
    let name = typeof value.name === "string" ? value.name : "";
    if (ns !== "" && !name.startsWith(ns + ".")) {
      name = `${ns}.${name}`;
    }
    if ((value.type === "function" || value.type === "custom") && name !== "")
      return {
        mode: "tool",
        name,
        ...(value.type === "custom" ? { tool_type: "custom" } : {}),
      };
  }
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

function encodeToolChoice(
  value: ToolChoice | undefined,
  losses: Loss[],
): JsonValue | undefined {
  if (value === undefined) return undefined;
  if (value.mode === "auto" || value.mode === "none") return value.mode;
  if (value.mode === "any") return "required";
  if (value.mode === "tool") {
    if (value.tool_type === "apply_patch") return { type: "apply_patch" };
    if (value.name !== "") {
      const dotIdx = value.name.indexOf(".");
      const kind = value.tool_type === "custom" ? "custom" : "function";
      if (dotIdx > 0) {
        return {
          type: kind,
          namespace: value.name.slice(0, dotIdx),
          name: value.name.slice(dotIdx + 1),
        };
      }
      return {
        type: kind,
        name: value.name,
      };
    }
  }
  losses.push(loss("tool_choice", "tool_choice", "unsupported-semantic"));
  return undefined;
}

interface ContainerEntry {
  readonly type: string;
  readonly name: string;
  readonly description?: string;
}

function isKnownOpenAINativeType(type: string): boolean {
  switch (type) {
    case "web_search":
    case "file_search":
    case "code_interpreter":
    case "computer":
    case "computer_use_preview":
    case "mcp":
    case "tool_search":
    case "image_generation":
    case "local_shell":
    case "shell":
    case "programmatic_tool_calling":
      return true;
  }
  if (
    type.startsWith("web_search_") ||
    type.startsWith("file_search_") ||
    type.startsWith("computer_")
  ) {
    return true;
  }
  return false;
}

function decodeResponsesToolItem(
  entry: JsonValue,
  pathStack: readonly ContainerEntry[],
  path: string,
  seenNames: Set<string>,
  losses: Loss[],
): Tool[] {
  const tool = object(entry, path);
  const type = typeof tool.type === "string" ? tool.type : "function";

  if (type === "namespace" || type === "toolbox") {
    const name = string(tool.name, `${path}.name`);
    if (
      !name ||
      name.startsWith(".") ||
      name.endsWith(".") ||
      name.includes("..")
    ) {
      fail(`${path}: invalid ${type} name "${name}"`);
    }
    const tools = array(tool.tools, `${path}.tools`);
    if (tools.length === 0) {
      fail(`${path}: ${type} requires non-empty tools array`);
    }
    const desc =
      tool.description === undefined
        ? undefined
        : string(tool.description, `${path}.description`);
    const newStack = [
      ...pathStack,
      {
        type,
        name,
        ...(desc !== undefined ? { description: desc } : {}),
      },
    ];
    return tools.flatMap((child, idx) =>
      decodeResponsesToolItem(
        child,
        newStack,
        `${path}.tools[${idx}]`,
        seenNames,
        losses,
      ),
    );
  }

  const prefix = pathStack.map((e) => e.name).join(".");

  if (type === "apply_patch") {
    return [{ type: "apply_patch" as const }];
  }

  if (type === "custom") {
    const rawName = nonEmptyString(tool.name, `${path}.name`);
    const finalName =
      prefix && !rawName.startsWith(prefix + ".")
        ? `${prefix}.${rawName}`
        : rawName;
    if (seenNames.has(finalName)) {
      fail(`responses: duplicate tool name "${finalName}"`);
    }
    seenNames.add(finalName);

    const description =
      tool.description === undefined
        ? undefined
        : string(tool.description, `${path}.description`);
    const format =
      tool.format === undefined
        ? undefined
        : decodeCustomToolFormat(tool.format, losses, `${path}.format`);
    if (tool.format !== undefined && format === undefined) return [];

    const nativeAnnotations: import("../../ir/index.js").NativeAnnotation[] =
      [];
    if (pathStack.length > 0) {
      nativeAnnotations.push({
        source: "openai",
        kind: "container",
        value: JSON.stringify({ path: pathStack }),
        omission: {
          field: "type",
          reason: "degraded",
          detail:
            "tool container grouping was flattened into dotted tool names",
        },
      });
    }
    if (tool.defer_loading !== undefined) {
      if (typeof tool.defer_loading !== "boolean") {
        fail(`${path}.defer_loading must be a boolean`);
      }
      if (tool.defer_loading === true) {
        nativeAnnotations.push({
          source: "openai",
          kind: "defer_loading",
          value: "true",
          omission: {
            field: "defer_loading",
            reason: "degraded",
            detail: DEFER_LOADING_LOSS_DETAIL,
          },
        });
      }
    }

    return [
      {
        type: "custom" as const,
        name: finalName,
        ...(description === undefined ? {} : { description }),
        ...(format === undefined ? {} : { format }),
        ...(nativeAnnotations.length > 0 ? { native: nativeAnnotations } : {}),
      },
    ];
  }

  if (type === "function" || type === "") {
    const rawName = nonEmptyString(tool.name, `${path}.name`);
    const finalName =
      prefix && !rawName.startsWith(prefix + ".")
        ? `${prefix}.${rawName}`
        : rawName;
    if (seenNames.has(finalName)) {
      fail(`responses: duplicate tool name "${finalName}"`);
    }
    seenNames.add(finalName);

    const description =
      tool.description === undefined
        ? undefined
        : string(tool.description, `${path}.description`);
    const strict =
      tool.strict === undefined || tool.strict === null
        ? undefined
        : boolean(tool.strict, `${path}.strict`);

    const nativeAnnotations: import("../../ir/index.js").NativeAnnotation[] =
      [];
    if (pathStack.length > 0) {
      nativeAnnotations.push({
        source: "openai",
        kind: "container",
        value: JSON.stringify({ path: pathStack }),
        omission: {
          field: "type",
          reason: "degraded",
          detail:
            "tool container grouping was flattened into dotted tool names",
        },
      });
    }
    if (tool.defer_loading !== undefined) {
      if (typeof tool.defer_loading !== "boolean") {
        fail(`${path}.defer_loading must be a boolean`);
      }
      if (tool.defer_loading === true) {
        nativeAnnotations.push({
          source: "openai",
          kind: "defer_loading",
          value: "true",
          omission: {
            field: "defer_loading",
            reason: "degraded",
            detail: DEFER_LOADING_LOSS_DETAIL,
          },
        });
      }
    }

    return [
      {
        name: finalName,
        ...(description === undefined ? {} : { description }),
        input_schema: object(tool.parameters, `${path}.parameters`),
        ...(strict === undefined ? {} : { strict }),
        ...(nativeAnnotations.length > 0 ? { native: nativeAnnotations } : {}),
      },
    ];
  }

  // Native tool declaration or unsupported
  const hasName = typeof tool.name === "string" && tool.name.length > 0;
  const hasServerLabel =
    typeof tool.server_label === "string" && tool.server_label.length > 0;
  const isNative = hasName || hasServerLabel || isKnownOpenAINativeType(type);
  if (!isNative) {
    losses.push({
      path,
      field: "type",
      reason: "unsupported-semantic",
      detail: `Responses tool type "${type}" has no IR equivalent`,
    });
    return [];
  }

  const toolName = hasName
    ? (tool.name as string)
    : hasServerLabel
      ? (tool.server_label as string)
      : type;
  if (seenNames.has(toolName)) {
    fail(`responses: duplicate tool name "${toolName}"`);
  }
  seenNames.add(toolName);

  const rawSource = sourceTextOf(tool as JsonObject);
  const defRaw =
    rawSource !== undefined ? rawSource : stringifyJson(tool as JsonValue);
  const definition = compactJson(defRaw);

  return [
    {
      type: "native" as const,
      name: toolName,
      source: "openai",
      definition,
    },
  ];
}

function compactJson(raw: string): string {
  let out = "";
  let inString = false;
  let escape = false;
  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i]!;
    if (inString) {
      out += ch;
      if (escape) {
        escape = false;
      } else if (ch === "\\") {
        escape = true;
      } else if (ch === '"') {
        inString = false;
      }
    } else {
      if (ch === '"') {
        inString = true;
        out += ch;
      } else if (ch !== " " && ch !== "\t" && ch !== "\n" && ch !== "\r") {
        out += ch;
      }
    }
  }
  return out;
}

function parseContainerPath(tool: Tool): ContainerEntry[] | undefined {
  if (!tool.native) return undefined;
  for (const ann of tool.native) {
    if (ann.source === "openai" && ann.kind === "container") {
      try {
        const parsed = JSON.parse(ann.value);
        if (parsed && Array.isArray(parsed.path) && parsed.path.length > 0) {
          return parsed.path;
        }
      } catch {}
      const single = ann.value.replace(/^"|"$/g, "");
      if (single) {
        const rawName = tool.name ?? "";
        const dotIdx = rawName.indexOf(".");
        const name = dotIdx > 0 ? rawName.slice(0, dotIdx) : rawName;
        return [{ type: single, name }];
      }
    }
  }
  return undefined;
}

function buildNestedContainer(
  pathStack: readonly ContainerEntry[],
  tools: readonly Tool[],
): JsonValue {
  const curr = pathStack[0]!;
  if (pathStack.length === 1) {
    const childTools: JsonValue[] = [];
    for (const subTool of tools) {
      const rawName = subTool.name ?? "";
      const lastDot = rawName.lastIndexOf(".");
      const strippedName = lastDot >= 0 ? rawName.slice(lastDot + 1) : rawName;
      const hasDefer = subTool.native?.some(
        (ann) => ann.kind === "defer_loading" && ann.value === "true",
      );
      if (subTool.type === "custom") {
        childTools.push({
          type: "custom",
          name: strippedName,
          ...(subTool.description !== undefined
            ? { description: subTool.description }
            : {}),
          ...(subTool.format !== undefined
            ? { format: encodeCustomToolFormat(subTool.format) }
            : {}),
          ...(hasDefer ? { defer_loading: true } : {}),
        });
      } else {
        childTools.push({
          type: "function",
          name: strippedName,
          ...(subTool.description !== undefined
            ? { description: subTool.description }
            : {}),
          parameters: (subTool as any).input_schema as JsonObject,
          ...((subTool as any).strict === undefined
            ? {}
            : { strict: (subTool as any).strict }),
          ...(hasDefer ? { defer_loading: true } : {}),
        });
      }
    }
    return {
      type: curr.type,
      name: curr.name,
      ...(curr.description !== undefined
        ? { description: curr.description }
        : {}),
      tools: childTools,
    };
  }

  // Multi-level
  const childTools: JsonValue[] = [];
  let k = 0;
  while (k < tools.length) {
    const subPath = parseContainerPath(tools[k]!);
    if (!subPath || subPath.length < 2) {
      k++;
      continue;
    }
    const nextName = subPath[1]!.name;
    let m = k;
    while (m < tools.length) {
      const p = parseContainerPath(tools[m]!);
      if (!p || p.length < 2 || p[1]!.name !== nextName) break;
      m++;
    }
    childTools.push(
      buildNestedContainer(pathStack.slice(1), tools.slice(k, m)),
    );
    k = m;
  }
  return {
    type: curr.type,
    name: curr.name,
    ...(curr.description !== undefined
      ? { description: curr.description }
      : {}),
    tools: childTools,
  };
}

function encodeResponsesGroupedTools(
  tools: readonly Tool[],
  losses: Loss[],
): JsonValue[] {
  const out: JsonValue[] = [];
  let i = 0;
  while (i < tools.length) {
    const tool = tools[i]!;
    if (tool.type === "native") {
      if (tool.source === "openai") {
        out.push(parseJson(tool.definition));
      } else {
        losses.push({
          path: `tools[${i}]`,
          field: "type",
          reason: "unsupported-semantic",
          detail: `native tool "${tool.name}" from "${tool.source}" cannot be represented by the target protocol`,
        });
      }
      i++;
      continue;
    }

    if (tool.type === "apply_patch") {
      out.push({ type: "apply_patch" });
      i++;
      continue;
    }

    const containerPath = parseContainerPath(tool);
    const dotIdx = tool.name ? tool.name.indexOf(".") : -1;
    if (containerPath && containerPath.length > 0) {
      const topName = containerPath[0]!.name;
      let j = i;
      while (j < tools.length) {
        const nextTool = tools[j]!;
        if (nextTool.type === "native" || nextTool.type === "apply_patch")
          break;
        const nextPath = parseContainerPath(nextTool);
        if (!nextPath || nextPath.length === 0 || nextPath[0]!.name !== topName)
          break;
        j++;
      }
      out.push(buildNestedContainer(containerPath, tools.slice(i, j)));
      i = j;
      continue;
    } else if (dotIdx > 0 && tool.name) {
      const prefix = tool.name.slice(0, dotIdx);
      let j = i;
      while (j < tools.length) {
        const nextTool = tools[j]!;
        if (nextTool.type === "native" || nextTool.type === "apply_patch")
          break;
        if (!nextTool.name || !nextTool.name.startsWith(prefix + ".")) break;
        j++;
      }
      out.push(
        buildNestedContainer(
          [{ type: "namespace", name: prefix }],
          tools.slice(i, j),
        ),
      );
      i = j;
      continue;
    }

    const hasDefer = tool.native?.some(
      (ann) => ann.kind === "defer_loading" && ann.value === "true",
    );
    if (tool.type === "custom") {
      out.push({
        type: "custom",
        name: tool.name,
        ...(tool.description !== undefined
          ? { description: tool.description }
          : {}),
        ...(tool.format !== undefined
          ? { format: encodeCustomToolFormat(tool.format) }
          : {}),
        ...(hasDefer ? { defer_loading: true } : {}),
      });
    } else {
      out.push({
        type: "function",
        name: tool.name,
        ...(tool.description !== undefined
          ? { description: tool.description }
          : {}),
        parameters: (tool as any).input_schema as JsonObject,
        ...((tool as any).strict === undefined
          ? {}
          : { strict: (tool as any).strict }),
        ...(hasDefer ? { defer_loading: true } : {}),
      });
    }
    i++;
  }
  return out;
}

function encodeSystemPlan(system: Request["system"]): {
  instructions?: string;
  items: JsonObject[];
} {
  if (system === undefined || system.length === 0) return { items: [] };
  if (!system.some((block) => block.role === "developer"))
    return {
      instructions: system.map((block) => block.text).join(""),
      items: [],
    };
  let index = 0;
  let instructions: string | undefined;
  if (system[0]!.role !== "developer") {
    const text: string[] = [];
    while (index < system.length && system[index]!.role !== "developer") {
      text.push(system[index]!.text);
      index += 1;
    }
    instructions = text.join("");
  }
  const items: JsonObject[] = [];
  while (index < system.length) {
    const block = system[index]!;
    items.push({
      role: block.role === "developer" ? "developer" : "system",
      content: block.text,
    });
    index += 1;
  }
  return instructions === undefined ? { items } : { instructions, items };
}

function stringMetadata(
  value: JsonValue | undefined,
  face: string,
): { metadata: Record<string, string> } | Record<string, never> {
  if (value === undefined || value === null) return {};
  if (typeof value !== "object" || Array.isArray(value))
    fail(`${face}: metadata must be an object`);
  const metadata: Record<string, string> = {};
  for (const [key, child] of Object.entries(value)) {
    if (typeof child !== "string")
      fail(`${face}: metadata.${key} must be a string`);
    metadata[key] = child;
  }
  return Object.keys(metadata).length === 0 ? {} : { metadata };
}

function loss(path: string, field: string, reason: LossReason): Loss {
  return { path, field, reason };
}
function optionalArray(
  value: JsonValue | undefined,
  name: string,
): readonly JsonValue[] {
  return value === undefined || value === null ? [] : array(value, name);
}
function array(
  value: JsonValue | undefined,
  name: string,
): readonly JsonValue[] {
  if (!isJsonArray(value)) fail(`${name} must be an array`);
  return value;
}
function object(value: JsonValue | undefined, name: string): JsonObject {
  if (
    value === undefined ||
    value === null ||
    typeof value !== "object" ||
    isJsonArray(value) ||
    isJsonNumber(value)
  )
    fail(`${name} must be an object`);
  return value;
}
function objectOrThrow(value: JsonValue, name: string): JsonObject {
  return object(value, name);
}
function string(value: JsonValue | undefined, name: string): string {
  if (typeof value !== "string") fail(`${name} must be a string`);
  return value;
}
function number(value: JsonValue | undefined, name: string): number {
  if (!isJsonNumber(value)) fail(`${name} must be a number`);
  const result = Number(value.token);
  if (!Number.isFinite(result)) fail(`${name} must be finite`);
  return result;
}
function whole(value: JsonValue | undefined, name: string): bigint {
  if (!isJsonNumber(value) || !value.isInteger)
    fail(`${name} must be an integer`);
  return BigInt(value.token);
}
function fail(message: string): never {
  throw new OxaError("type-violation", `responses: ${message}`);
}

function boolean(value: JsonValue | undefined, name: string): boolean {
  if (typeof value !== "boolean") fail(`${name} must be a boolean`);
  return value;
}

function nonEmptyString(value: JsonValue | undefined, name: string): string {
  const text = string(value, name);
  if (text.length === 0) fail(`${name} must not be empty`);
  return text;
}

function decodeTextFormat(
  value: JsonValue | undefined,
  losses: Loss[],
): ResponseFormat | undefined {
  if (value === undefined || value === null) return undefined;
  const wire = object(value, "text.format");
  const type = string(wire.type, "text.format.type");
  switch (type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema": {
      const name = nonEmptyString(wire.name, "text.format.name");
      const schema = object(wire.schema, "text.format.schema");
      const description =
        wire.description === undefined
          ? undefined
          : string(wire.description, "text.format.description");
      const strict =
        wire.strict === undefined
          ? undefined
          : boolean(wire.strict, "text.format.strict");
      return {
        type: "json_schema",
        name,
        ...(description === undefined ? {} : { description }),
        schema,
        ...(strict === undefined ? {} : { strict }),
      };
    }
    default:
      losses.push(loss("text.format.type", "type", "unmapped-value"));
      return undefined;
  }
}

function encodeTextFormat(format: ResponseFormat): JsonObject {
  switch (format.type) {
    case "text":
      return { type: "text" };
    case "json_object":
      return { type: "json_object" };
    case "json_schema":
      assertSourceMatches(format.schema);
      return {
        type: "json_schema",
        name: format.name!,
        ...(format.description === undefined
          ? {}
          : { description: format.description }),
        schema: format.schema,
        ...(format.strict === undefined ? {} : { strict: format.strict }),
      };
  }
}
