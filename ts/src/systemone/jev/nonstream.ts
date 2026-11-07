import { OxaError } from "../../error.js";
import {
  fromValue,
  integer,
  isJsonArray,
  isJsonNumber,
  parseJson,
  stringifyJson,
  type JsonObject,
  type JsonValue,
} from "../../json/index.js";
import { sourceKeysOf } from "../../json/value.js";
import type { ConversionResult, Loss } from "../../loss.js";
import { mapModel, type ModelMapper } from "../../modelmap.js";
import type {
  Block,
  Message,
  Request,
  Response,
  TextBlock,
} from "../../ir/index.js";

/**
 * SystemOne Jev face (spec/17): the TypeSafe AI `/v1/systemone` structured
 * decision protocol projected onto the IR conversation model. Nonstream only
 * — Jev has no streaming mode.
 */

const INSTRUCTION_TEMPLATE =
  "Answer the structured questions that follow as a single JSON object. Keys MUST be exactly the question IDs given in the user message; values MUST match each question's type: noul answers are numbers in [0,1], choice answers are one of the listed option names, score answers are one of the listed level numbers. Output raw JSON only: no prose, no markdown fences.";

const RESPONSE_FORMAT_NAME = "jev_answers";

const SYNTHESIZED_ID = "jev";

const QUESTION_TYPE_NOUL = "noul";
const QUESTION_TYPE_CHOICE = "choice";
const QUESTION_TYPE_SCORE = "score";
const QUESTION_TYPE_BOX = "box";

/** A question definition for response-encode context (spec/17 §4.1). */
export interface JevQuestion {
  readonly type: string;
  readonly instructions?: string;
  readonly criteria?: JsonObject | readonly JsonValue[];
}

export interface JevOptions {
  readonly modelMapper?: ModelMapper;
  /** The original request's questions; enables exact type recovery. */
  readonly questions?: Readonly<Record<string, JevQuestion>>;
}

function fail(message: string): never {
  throw new OxaError("type-violation", `jev: ${message}`);
}

function isPlainObject(value: JsonValue | undefined): value is JsonObject {
  return (
    value !== undefined &&
    value !== null &&
    typeof value === "object" &&
    !isJsonArray(value) &&
    !isJsonNumber(value)
  );
}

function requireObject(value: JsonValue | undefined, name: string): JsonObject {
  if (!isPlainObject(value)) fail(`${name} must be an object`);
  return value;
}

function sortedKeys(object: JsonObject): readonly string[] {
  return sourceKeysOf(object)
    .slice()
    .sort((a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0));
}

/** Caller-keyed wire objects must keep every key as an own property, so
 * `__proto__` and friends never touch the prototype chain (spec/02 audit
 * discipline; Go's map has this property by construction). */
function makeKeyed<T>(): Record<string, T> {
  return Object.create(null) as Record<string, T>;
}

function hasOwnKey(
  object: Readonly<Record<string, unknown>>,
  key: string,
): boolean {
  return Object.prototype.hasOwnProperty.call(object, key);
}

/** Rebuilds a JSON value with object keys sorted at every depth. */
function sortRebuild(value: JsonValue): JsonValue {
  if (isJsonArray(value)) return value.map(sortRebuild);
  if (isPlainObject(value)) {
    const out: Record<string, JsonValue> = {};
    for (const key of sortedKeys(value)) out[key] = sortRebuild(value[key]!);
    return out;
  }
  return value;
}

/** Serializes a JSON value with keys in ascending lexicographic order at
 * every depth (spec/17 §7.5/§7.6 canon). JavaScript object enumeration
 * reorders integer-like keys numerically, so the walk cannot rely on
 * insertion order; leaf values go through stringifyJson to keep number
 * tokens verbatim. */
function canonicalStringify(value: JsonValue): string {
  if (isJsonArray(value)) return `[${value.map(canonicalStringify).join(",")}]`;
  if (isPlainObject(value)) {
    return `{${sortedKeys(value)
      .map((key) => `${stringifyJson(key)}:${canonicalStringify(value[key]!)}`)
      .join(",")}}`;
  }
  return stringifyJson(value);
}

/** Clones a JSON value without parse-time source-text markers, so
 * stringifyJson renders it compact with key order preserved — the wire-side
 * equivalent of Go's json.Compact on RawMessage carries. */
function detach(value: JsonValue): JsonValue {
  if (isJsonArray(value)) return value.map(detach);
  if (isPlainObject(value)) {
    const out: Record<string, JsonValue> = {};
    for (const key of sourceKeysOf(value)) out[key] = detach(value[key]!);
    return out;
  }
  return value;
}

// --- exact decimal comparison ------------------------------------------------
// Range and integer validation must not round JSON number tokens through
// IEEE 754 doubles: 1.0000000000000001 is greater than 1 and 1.5 is not a
// score level, even though both collapse to the same double. A token is
// represented as ±digits × 10^exp with digits stripped of leading and
// trailing zeros, so magnitude checks work on the digit strings and their
// order of magnitude without ever materializing a power of ten (exponents
// saturate instead of expanding adversarially long powers into memory).

const JSON_NUMBER_PATTERN =
  /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$/;

interface DecimalParts {
  readonly negative: boolean;
  readonly digits: string;
  readonly exp: number;
}

/** Parses the exponent literal with saturation at ±2^62: the sign of the
 * overflow preserves whether the value is astronomically large or small,
 * which is all the range checks need. */
function parseExponentSaturating(literal: string): number {
  const LIMIT = 2 ** 62;
  const negative = literal.startsWith("-");
  const body = literal.replace(/^[+-]/, "");
  let value = 0;
  for (const character of body) {
    const digit = character.charCodeAt(0) - 48;
    if (digit < 0 || digit > 9) return 0;
    if (value > LIMIT / 10) {
      value = LIMIT;
      break;
    }
    value *= 10;
    if (value > LIMIT - digit) {
      value = LIMIT;
      break;
    }
    value += digit;
  }
  return negative ? -value : value;
}

function parseDecimal(token: string): DecimalParts | undefined {
  if (!JSON_NUMBER_PATTERN.test(token)) return undefined;
  const negative = token.startsWith("-");
  let body = negative ? token.slice(1) : token;
  let exponent = 0;
  const eIndex = body.search(/[eE]/);
  if (eIndex >= 0) {
    exponent = parseExponentSaturating(body.slice(eIndex + 1));
    body = body.slice(0, eIndex);
  }
  let intDigits: string;
  let fracDigits: string;
  const dot = body.indexOf(".");
  if (dot >= 0) {
    intDigits = body.slice(0, dot);
    fracDigits = body.slice(dot + 1);
  } else {
    intDigits = body;
    fracDigits = "";
  }
  const exp = exponent - fracDigits.length;
  const digits = (intDigits + fracDigits).replace(/^0+(?=[0-9])/, "");
  if (digits === "0") return { negative, digits: "0", exp: 0 };
  const trimmedRight = digits.replace(/0+$/, "");
  const trailing = digits.length - trimmedRight.length;
  return {
    negative,
    digits: digits.slice(0, digits.length - trailing),
    exp: exp + trailing,
  };
}

function isZeroDecimal(parts: DecimalParts): boolean {
  return parts.digits === "0";
}

/** Reports whether a JSON number token equals exactly 0, accepting any
 * decimal spelling such as `0.0` or `0e0`; false for non-number leaves. */
function decimalEqualsZero(a: JsonValue): boolean {
  if (!isJsonNumber(a)) return false;
  const parts = parseDecimal(a.token);
  return parts !== undefined && isZeroDecimal(parts);
}

/** Reports whether a JSON number token equals exactly 1, accepting any
 * decimal spelling such as `1.0` or `1e0`; false for non-number leaves. */
function decimalEqualsOne(a: JsonValue): boolean {
  if (!isJsonNumber(a)) return false;
  const parts = parseDecimal(a.token);
  if (parts === undefined || isZeroDecimal(parts) || parts.negative)
    return false;
  return parts.exp === 0 && parts.digits === "1";
}

/** Reports whether a JSON number token is exactly within [0,1] in decimal
 * terms (spec/17 §4.1 noul range); false for non-number leaves. */
function decimalWithinUnitInterval(a: JsonValue): boolean {
  if (!isJsonNumber(a)) return false;
  const parts = parseDecimal(a.token);
  if (parts === undefined) return false;
  if (isZeroDecimal(parts)) return true;
  if (parts.negative) return false;
  // A positive nonzero value is within [0,1] exactly when it is 1 or a
  // decimal fraction below 1: digits × 10^exp ≤ 1 with digits ≥ 1.
  if (parts.exp > 0) return false;
  if (parts.exp === 0) return parts.digits === "1";
  // exp < 0: value = digits × 10^exp ≤ 1 ⇔ digits ≤ 10^(-exp). The bound
  // only matters when its digit count can reach digits.length; a longer
  // bound means digits is certainly below it.
  if (-parts.exp + 1 > parts.digits.length) return true;
  const bound = "1" + "0".repeat(-parts.exp);
  if (parts.digits.length < bound.length) return true;
  return parts.digits.length === bound.length && parts.digits <= bound;
}

/** Validates a score value against the discrete 1..N levels exactly: the
 * token must be an integer whose exact value falls in range. */
function scoreWithinLevels(leaf: JsonValue, levels: number): boolean {
  if (!isJsonNumber(leaf)) return false;
  const parts = parseDecimal(leaf.token);
  if (parts === undefined || isZeroDecimal(parts) || parts.negative)
    return false;
  // A fractional value (negative exp after trailing-zero stripping) is not
  // an integer level; a positive exp of 2 or more is at least 100, beyond
  // the 1..10 level bound.
  if (parts.exp < 0 || parts.exp > 2) return false;
  const valueDigits = parts.digits + "0".repeat(parts.exp);
  const levelDigits = String(levels);
  if (valueDigits.length !== levelDigits.length)
    return valueDigits.length < levelDigits.length;
  return valueDigits <= levelDigits;
}

// ---------------------------------------------------------------------------
// decode: Jev wire -> IR
// ---------------------------------------------------------------------------

export function decodeRequest(
  wire: JsonObject,
  options: JevOptions = {},
): ConversionResult<Request> {
  const losses: Loss[] = [];
  let modelName: string | undefined;
  let modelSeen = false;
  let stateValue: JsonValue | undefined;
  let stateSeen = false;
  let questionsValue: JsonObject | undefined;
  const questions = new Map<string, RenderedQuestion>();

  const unknownProperty = (path: string, key: string, what: string): void => {
    losses.push({
      path,
      field: key,
      reason: "unmapped-field",
      detail: `unknown property on the ${what}`,
    });
  };

  for (const key of sourceKeysOf(wire)) {
    const value = wire[key]!;
    switch (key) {
      case "model":
        if (typeof value !== "string") fail("request model must be a string");
        modelName = value;
        modelSeen = true;
        break;
      case "state": {
        stateSeen = true;
        stateValue = value;
        if (value === null) break;
        if (typeof value === "string") break;
        if (isPlainObject(value) || isJsonArray(value)) {
          losses.push({
            path: "state",
            field: "state",
            reason: "degraded",
            detail: "N-JEV-1: state serialized to canonical JSON text",
          });
          break;
        }
        fail("state must be a string, object, or array");
      }
      case "questions": {
        questionsValue = requireObject(value, "request questions");
        if (Object.keys(questionsValue).length === 0)
          fail("request questions must not be empty");
        for (const id of sourceKeysOf(questionsValue)) {
          const question = requireObject(questionsValue[id], `question ${id}`);
          // Determine the question type first: a whole-unit skip (experimental
          // box or unsupported type) suppresses the per-field audit inside the
          // unit, per spec/02 §4.
          const carried = question.type;
          if (typeof carried !== "string")
            fail(`question ${id} type must be a string`);
          if (
            carried === QUESTION_TYPE_BOX ||
            !supportedQuestionType(carried)
          ) {
            losses.push(wholeUnitSkipLoss(id, carried));
            continue;
          }
          let instructions = "";
          let criteria: JsonValue | undefined;
          for (const field of sourceKeysOf(question)) {
            const leaf = question[field]!;
            switch (field) {
              case "type":
                break;
              case "instructions":
                if (typeof leaf !== "string")
                  fail(`question ${id} instructions must be a string`);
                instructions = leaf;
                break;
              case "criteria":
                criteria = leaf;
                break;
              default:
                unknownProperty(`questions.${id}`, field, "Jev question");
            }
          }
          const rendered = renderQuestion(id, carried, instructions, criteria);
          if (rendered.loss !== undefined) {
            losses.push(rendered.loss);
            continue;
          }
          questions.set(id, rendered);
        }
        break;
      }
      case "assets":
        losses.push({
          path: "assets",
          field: "assets",
          reason: "unsupported-semantic",
          detail: "N-JEV-4: experimental image assets are out of scope",
        });
        break;
      default:
        unknownProperty(key, key, "Jev request");
    }
  }

  if (!modelSeen || modelName === undefined || modelName === "")
    fail("request model is required");
  if (!stateSeen || stateValue === null) fail("request state is required");
  if (questionsValue === undefined) fail("request questions must not be empty");
  if (questions.size === 0)
    fail("request questions produced no supported questions");

  // The §7.2 rendering follows ascending lexicographic ID order regardless
  // of the wire's encounter order; the loss walk above stays in source order.
  let questionLines: string[] = [];
  const props = makeKeyed<JsonValue>();
  const required: string[] = [];
  for (const id of [...questions.keys()].sort()) {
    const rendered = questions.get(id)!;
    questionLines = questionLines.concat(rendered.lines);
    props[id] = rendered.property;
    required.push(id);
  }

  const stateText = stateAsText(stateValue!);

  const schema = makeKeyed<JsonValue>();
  schema.additionalProperties = false;
  const sortedProps = makeKeyed<JsonValue>();
  for (const id of Object.keys(props).sort()) sortedProps[id] = props[id]!;
  schema.properties = sortedProps;
  schema.required = required.slice().sort();
  schema.type = "object";

  return {
    value: {
      model: mapModel(options.modelMapper, modelName),
      system: [
        {
          type: "text",
          text: `${stateText}\n\n${INSTRUCTION_TEMPLATE}`,
        },
      ],
      messages: [
        {
          role: "user",
          content: [{ type: "text", text: questionLines.join("\n") }],
        },
      ],
      params: {
        response_format: {
          type: "json_schema",
          name: RESPONSE_FORMAT_NAME,
          schema,
        },
      },
    },
    losses,
  };
}

function stateAsText(value: JsonValue): string {
  if (typeof value === "string") return value;
  return canonicalStringify(value);
}

function wholeUnitSkipLoss(id: string, type: string): Loss {
  return type === QUESTION_TYPE_BOX
    ? {
        path: `questions.${id}`,
        field: "type",
        reason: "unsupported-semantic",
        detail: "N-JEV-5: experimental box questions are out of scope",
      }
    : {
        path: `questions.${id}`,
        field: "type",
        reason: "unsupported-semantic",
        detail: "N-JEV-6: skipped from schema and prompt",
      };
}

interface RenderedQuestion {
  readonly lines: readonly string[];
  readonly property: JsonObject;
  readonly loss?: Loss | undefined;
}

// renderQuestion renders one question into prompt lines plus its synthesized
// schema property. A loss means the question is skipped (experimental or
// unsupported type).
function renderQuestion(
  id: string,
  type: string,
  instructions: string,
  criteria: JsonValue | undefined,
): RenderedQuestion {
  const head = `question ${id} (${type}): ${instructions}`;
  const criteriaLines: string[] = [];
  const property: Record<string, JsonValue> = {};

  const addCriterionLine = (
    name: string,
    description: JsonValue | undefined,
  ) => {
    if (
      description === undefined ||
      description === null ||
      typeof description !== "string"
    ) {
      criteriaLines.push(
        description === undefined || description === null
          ? `- ${name}`
          : `- ${name}: ${canonicalStringify(description)}`,
      );
      return;
    }
    criteriaLines.push(`- ${name}: ${description}`);
  };

  switch (type) {
    case QUESTION_TYPE_NOUL: {
      if (criteria !== undefined && criteria !== null) {
        const crit = requireObject(criteria, `question ${id} noul criteria`);
        criteriaLines.push("criteria:");
        for (const name of ["false", "true"]) {
          const leaf = crit[name];
          if (leaf !== undefined) addCriterionLine(name, leaf);
        }
      }
      property.type = fromValue("number");
      property.minimum = fromValue(0);
      property.maximum = fromValue(1);
      break;
    }
    case QUESTION_TYPE_CHOICE: {
      if (!isPlainObject(criteria))
        fail(`question ${id} choice criteria is required`);
      const crit = criteria as JsonObject;
      if (Object.keys(crit).length === 0)
        fail(`question ${id} choice criteria must not be empty`);
      const names = sortedKeys(crit);
      criteriaLines.push("criteria:");
      for (const name of names) addCriterionLine(name, crit[name]);
      property.type = fromValue("string");
      property.enum = names.map((name) => fromValue(name));
      break;
    }
    case QUESTION_TYPE_SCORE: {
      if (!isJsonArray(criteria) || criteria.length === 0)
        fail(`question ${id} score criteria is required`);
      criteriaLines.push("criteria:");
      const enumValues: JsonValue[] = [];
      criteria.forEach((leaf, index) => {
        enumValues.push(fromValue(index + 1));
        addCriterionLine(`level ${index + 1}`, leaf);
      });
      property.type = fromValue("number");
      property.enum = enumValues;
      break;
    }
  }

  if (instructions !== "") property.description = fromValue(instructions);
  const sorted: Record<string, JsonValue> = {};
  for (const key of Object.keys(property).sort()) sorted[key] = property[key]!;
  return { lines: [head, ...criteriaLines], property: sorted };
}

export function decodeRequestJSON(
  raw: string,
  options: JevOptions = {},
): ConversionResult<Request> {
  const parsed = parseJson(raw);
  return decodeRequest(requireObject(parsed, "request JSON"), options);
}

export function decodeResponse(
  wire: JsonObject,
  options: JevOptions = {},
): ConversionResult<Response> {
  const losses: Loss[] = [];
  let modelName: string | undefined;
  let modelSeen = false;
  let answers: Map<string, JsonObject> | undefined;
  let usage: { input_tokens: bigint; output_tokens: bigint } | undefined;

  const unknownProperty = (path: string, key: string, what: string): void => {
    losses.push({
      path,
      field: key,
      reason: "unmapped-field",
      detail: `unknown property on the ${what}`,
    });
  };

  for (const key of sourceKeysOf(wire)) {
    const value = wire[key]!;
    switch (key) {
      case "model":
        if (typeof value !== "string") fail("response model must be a string");
        modelName = value;
        modelSeen = true;
        break;
      case "answers": {
        const map = requireObject(value, "answers");
        answers = new Map();
        for (const id of sourceKeysOf(map)) {
          const answer = requireObject(map[id], `answer ${id}`);
          const filtered = makeKeyed<JsonValue>();
          for (const field of sourceKeysOf(answer)) {
            const leaf = answer[field]!;
            switch (field) {
              case "type":
                filtered[field] = leaf;
                if (leaf === QUESTION_TYPE_BOX) {
                  losses.push({
                    path: `answers.${id}`,
                    field: "type",
                    reason: "unsupported-semantic",
                    detail:
                      "N-JEV-5: experimental box answers are carried verbatim",
                  });
                }
                break;
              case "noul":
              case "choice":
              case "score":
              case "legend":
              case "probabilities":
              case "confidence":
              case "box":
                filtered[field] = leaf;
                break;
              default:
                losses.push({
                  path: `answers.${id}`,
                  field,
                  reason: "unmapped-field",
                  detail: "unknown property on the Jev answer; dropped",
                });
            }
          }
          const sorted = makeKeyed<JsonValue>();
          for (const field of Object.keys(filtered).sort())
            sorted[field] = detach(filtered[field]!);
          answers.set(id, sorted);
        }
        break;
      }
      case "usage": {
        const map = requireObject(value, "usage");
        let input: bigint | undefined;
        let output: bigint | undefined;
        for (const field of sourceKeysOf(map)) {
          const leaf = map[field]!;
          switch (field) {
            case "input_tokens":
              input = usageInteger(leaf, "input_tokens");
              break;
            case "output_tokens":
              output = usageInteger(leaf, "output_tokens");
              break;
            default:
              unknownProperty("usage", field, "Jev usage");
          }
        }
        usage = { input_tokens: input ?? 0n, output_tokens: output ?? 0n };
        break;
      }
      default:
        unknownProperty(key, key, "Jev response");
    }
  }

  if (!modelSeen || modelName === undefined || modelName === "")
    fail("response model is required");
  if (usage === undefined) usage = { input_tokens: 0n, output_tokens: 0n };
  if (answers === undefined || answers.size === 0)
    fail("response answers must not be empty");

  // §7.5 canon: answer keys and each answer's recognized fields sort
  // ascending, while field values carry verbatim (nested key order and
  // number tokens preserved) — so the text composes layer-by-layer instead
  // of deep-sorting the whole payload.
  const canonText = `{${[...answers.keys()]
    .sort()
    .map((id) => `${stringifyJson(id)}:${stringifyJson(answers.get(id)!)}`)
    .join(",")}}`;

  return {
    value: {
      id: SYNTHESIZED_ID,
      model: mapModel(options.modelMapper, modelName),
      content: [{ type: "text", text: canonText }],
      stop_reason: "end_turn",
      usage,
    },
    losses,
  };
}

function usageInteger(value: JsonValue, name: string): bigint {
  if (!isJsonNumber(value) || !value.isInteger)
    fail(`usage ${name} must be an integer`);
  return BigInt(value.token);
}

export function decodeResponseJSON(
  raw: string,
  options: JevOptions = {},
): ConversionResult<Response> {
  const parsed = parseJson(raw);
  return decodeResponse(requireObject(parsed, "response JSON"), options);
}

// ---------------------------------------------------------------------------
// encode: IR -> Jev wire
// ---------------------------------------------------------------------------

export function encodeRequest(
  request: Request,
  options: JevOptions = {},
): ConversionResult<JsonObject> {
  const losses: Loss[] = [];
  const format = request.params?.response_format;
  if (
    format === undefined ||
    format.type !== "json_schema" ||
    Object.keys(format.schema).length === 0
  )
    fail("EncodeRequest requires a json_schema response format");

  const { questions, losses: schemaLosses } = questionsFromSchema(
    format.schema,
  );
  losses.push(...schemaLosses);

  const { state, losses: stateLosses } = stateFromIR(request, questions);
  losses.push(...stateLosses);
  losses.push(...requestLosses(request));

  const questionsWire = makeKeyed<JsonValue>();
  for (const id of Object.keys(questions).sort())
    questionsWire[id] = questionWire(questions[id]!);

  return {
    value: {
      model: mapModel(options.modelMapper, request.model),
      state,
      questions: questionsWire,
    },
    losses,
  };
}

function questionWire(question: JevQuestion): JsonObject {
  const out: Record<string, JsonValue> = { type: question.type };
  if (question.instructions !== undefined)
    out.instructions = question.instructions;
  if (question.criteria !== undefined) out.criteria = question.criteria;
  return out;
}

function requestLosses(request: Request): Loss[] {
  const losses: Loss[] = [];
  const jev10 = (path: string, field: string): Loss => ({
    path,
    field,
    reason: "unmapped-field",
    detail: "N-JEV-10: one loss per present field",
  });
  const params = request.params;
  if (params?.temperature !== undefined)
    losses.push(jev10("params", "temperature"));
  if (params?.top_p !== undefined) losses.push(jev10("params", "top_p"));
  if (params?.max_tokens !== undefined)
    losses.push(jev10("params", "max_tokens"));
  if (params?.stop_sequences !== undefined)
    losses.push(jev10("params", "stop_sequences"));
  if (params?.reasoning_effort !== undefined)
    losses.push(jev10("params", "reasoning_effort"));
  if (params?.verbosity !== undefined)
    losses.push(jev10("params", "verbosity"));
  if ((request.tools?.length ?? 0) > 0) losses.push(jev10("tools", "tools"));
  if (request.tool_choice !== undefined)
    losses.push(jev10("tool_choice", "tool_choice"));
  for (const key of Object.keys(request.metadata ?? {}).sort())
    losses.push({
      path: "metadata",
      field: key,
      reason: "unmapped-field",
      detail: "N-JEV-10: no Jev equivalent",
    });
  if ((request.native?.length ?? 0) > 0) losses.push(jev10("native", "native"));
  return losses;
}

function stateFromIR(
  request: Request,
  questions: Readonly<Record<string, JevQuestion>>,
): { state: string; losses: Loss[] } {
  const sysParts: string[] = [];
  for (const block of request.system ?? []) {
    if (block.text !== "") sysParts.push(block.text);
  }
  if (sysParts.length === 0) fail("EncodeRequest requires system text");

  const expected = new Map<string, string>();
  for (const [id, question] of Object.entries(questions))
    expected.set(id, question.type);

  const losses: Loss[] = [];
  for (const [index, message] of request.messages.entries()) {
    if (
      message.role === "user" &&
      isDerivedQuestionRendering(message, expected)
    )
      continue;
    losses.push({
      path: `messages[${index}]`,
      field: "content",
      reason: "unsupported-semantic",
      detail:
        "N-JEV-10: not the derived question rendering; Jev has no conversation turns",
    });
  }
  return { state: stripTemplate(sysParts.join("\n\n")), losses };
}

// isDerivedQuestionRendering reports whether the message's content is exactly
// a §7.2 rendering of the expected questions: every block is a text block,
// every line is a question line, a criteria line, or blank, and the question
// (id, type) set matches. Mixed-in non-text blocks have no Jev destination
// and must surface as a loss, never as a silent drop.
function isDerivedQuestionRendering(
  message: Message,
  expected: ReadonlyMap<string, string>,
): boolean {
  const texts: string[] = [];
  for (const block of message.content) {
    if (block.type !== "text") return false;
    texts.push(block.text);
  }
  if (texts.length === 0) return false;
  const found = new Map<string, string>();
  for (const text of texts) {
    for (const line of text.split("\n")) {
      if (line === "" || line === "criteria:" || line.startsWith("- "))
        continue;
      if (line.startsWith("question ")) {
        const parsed = parseQuestionLine(line);
        if (parsed === undefined) return false;
        if (found.has(parsed.id)) return false;
        found.set(parsed.id, parsed.type);
        continue;
      }
      return false;
    }
  }
  if (found.size !== expected.size) return false;
  for (const [id, type] of expected) if (found.get(id) !== type) return false;
  return true;
}

function parseQuestionLine(
  line: string,
): { id: string; type: string } | undefined {
  const rest = line.startsWith("question ")
    ? line.slice("question ".length)
    : undefined;
  if (rest === undefined) return undefined;
  const open = rest.indexOf(" (");
  if (open < 0) return undefined;
  const close = rest.indexOf("): ", open);
  if (close < 0) return undefined;
  return { id: rest.slice(0, open), type: rest.slice(open + 2, close) };
}

function stripTemplate(text: string): string {
  const suffix = `\n\n${INSTRUCTION_TEMPLATE}`;
  return text.endsWith(suffix)
    ? text.slice(0, text.length - suffix.length)
    : text;
}

function questionsFromSchema(schema: JsonObject): {
  questions: Record<string, JevQuestion>;
  losses: Loss[];
} {
  const properties = schema.properties;
  if (!isPlainObject(properties) || Object.keys(properties).length === 0)
    fail("response format schema has no properties");
  const questions = makeKeyed<JevQuestion>();
  const losses: Loss[] = [];
  for (const id of sortedKeys(properties)) {
    const property = requireObject(properties[id], `schema property ${id}`);
    const extraKey = Object.keys(property).find(
      (key) =>
        key !== "type" &&
        key !== "minimum" &&
        key !== "maximum" &&
        key !== "enum" &&
        key !== "description",
    );
    if (extraKey !== undefined) {
      losses.push({
        path: `questions.${id}`,
        field: "type",
        reason: "unsupported-semantic",
        detail: "N-JEV-6: schema constraint beyond the synthesized projection",
      });
      continue;
    }
    const propType = property.type;
    const description =
      typeof property.description === "string"
        ? property.description
        : undefined;
    if (propType === "number") {
      const enumValues = property.enum;
      if (isJsonArray(enumValues)) {
        const levels = scoreLevels(enumValues);
        if (levels === undefined) {
          losses.push({
            path: `questions.${id}`,
            field: "type",
            reason: "unsupported-semantic",
            detail: "N-JEV-6: skipped from schema and prompt",
          });
          continue;
        }
        questions[id] = {
          type: QUESTION_TYPE_SCORE,
          ...(description === undefined ? {} : { instructions: description }),
          criteria: Array.from({ length: levels }, () => ""),
        };
        losses.push({
          path: `questions.${id}`,
          field: "criteria",
          reason: "degraded",
          detail: "N-JEV-9: criteria descriptions live in the prompt text",
        });
        continue;
      }
      const minimumLeaf = property.minimum;
      const maximumLeaf = property.maximum;
      if (
        minimumLeaf !== undefined &&
        maximumLeaf !== undefined &&
        decimalEqualsZero(minimumLeaf) &&
        decimalEqualsOne(maximumLeaf)
      ) {
        questions[id] = {
          type: QUESTION_TYPE_NOUL,
          ...(description === undefined ? {} : { instructions: description }),
        };
        continue;
      }
    }
    if (propType === "string") {
      const names = choiceOptions(property.enum);
      if (names !== undefined) {
        const criteria = makeKeyed<JsonValue>();
        for (const name of names) criteria[name] = fromValue(name);
        questions[id] = {
          type: QUESTION_TYPE_CHOICE,
          ...(description === undefined ? {} : { instructions: description }),
          criteria,
        };
        losses.push({
          path: `questions.${id}`,
          field: "criteria",
          reason: "degraded",
          detail: "N-JEV-9: criteria descriptions live in the prompt text",
        });
        continue;
      }
    }
    losses.push({
      path: `questions.${id}`,
      field: "type",
      reason: "unsupported-semantic",
      detail: "N-JEV-6: skipped from schema and prompt",
    });
  }
  if (Object.keys(questions).length === 0)
    fail("response format schema has no reconstructable questions");
  return { questions, losses };
}

// scoreLevels validates an enum of ascending 1..N integers and returns N.
function scoreLevels(enumValues: readonly JsonValue[]): number | undefined {
  if (enumValues.length === 0) return undefined;
  for (const [index, value] of enumValues.entries()) {
    if (!scoreWithinLevels(value, index + 1)) return undefined;
    const parts = parseDecimal((value as { token: string }).token);
    if (parts === undefined) return undefined;
    const expected = String(index + 1);
    // scoreWithinLevels bounds exp to 0..2, so the shift stays bounded.
    const rendered = parts.digits + "0".repeat(Math.max(parts.exp, 0));
    if (rendered !== expected) return undefined;
  }
  return enumValues.length;
}

function choiceOptions(value: JsonValue | undefined): string[] | undefined {
  if (!isJsonArray(value) || value.length === 0) return undefined;
  const names: string[] = [];
  for (const item of value) {
    if (typeof item !== "string") return undefined;
    names.push(item);
  }
  return names;
}

export function encodeResponse(
  response: Response,
  options: JevOptions = {},
): ConversionResult<JsonObject> {
  const losses: Loss[] = [];
  const texts: string[] = [];
  response.content.forEach((block, index) => {
    if (block.type === "text") {
      texts.push(block.text);
      return;
    }
    losses.push({
      path: `content[${index}]`,
      field: "type",
      reason: "unsupported-semantic",
      detail: "N-JEV-15: non-text blocks are excluded from answers",
    });
  });
  if (texts.length === 0) fail("response has no text content");

  const aggregated = texts.join("");
  const parsed = parseJson(aggregated);
  const answers = requireObject(parsed, "aggregated response text");
  if (Object.keys(answers).length === 0)
    fail("aggregated response text carries no answers");

  const context = options.questions ?? {};
  for (const id of Object.keys(context).sort()) {
    if (!supportedQuestionType(context[id]!.type)) continue;
    if (!hasOwnKey(answers, id))
      fail(`answer ${id} is missing from the response`);
  }

  const out = makeKeyed<JsonValue>();
  for (const id of sortedKeys(answers)) {
    const recovered = recoverAnswer(id, answers[id]!, context);
    if (Object.keys(context).length > 0 && !hasOwnKey(context, id))
      losses.push({
        path: "answers",
        field: id,
        reason: "unmapped-field",
        detail: "answer beyond the requested questions",
      });
    if (recovered.loss !== undefined) losses.push(recovered.loss);
    out[id] = detach(recovered.value);
  }

  if (response.stop_reason !== "end_turn" && response.stop_reason !== "other")
    losses.push({
      path: "stop_reason",
      field: "stop_reason",
      reason: "unmapped-field",
      detail: "N-JEV-16: Jev responses carry no stop reason",
    });
  losses.push(...usageLosses(response.usage));

  const answersWire = makeKeyed<JsonValue>();
  for (const id of Object.keys(out).sort()) answersWire[id] = out[id]!;

  return {
    value: {
      model: mapModel(options.modelMapper, response.model),
      answers: answersWire,
      usage: {
        input_tokens: integer(response.usage.input_tokens),
        output_tokens: integer(response.usage.output_tokens),
      },
    },
    losses,
  };
}

function supportedQuestionType(type: string): boolean {
  return (
    type === QUESTION_TYPE_NOUL ||
    type === QUESTION_TYPE_CHOICE ||
    type === QUESTION_TYPE_SCORE
  );
}

function usageLosses(usage: Response["usage"]): Loss[] {
  const losses: Loss[] = [];
  const add = (field: string): Loss => ({
    path: "usage",
    field,
    reason: "unmapped-field",
    detail: "N-JEV-17: usage granularity has no Jev equivalent",
  });
  if (usage.cache_read_input_tokens !== undefined)
    losses.push(add("cache_read_input_tokens"));
  if (usage.cache_creation_input_tokens !== undefined)
    losses.push(add("cache_creation_input_tokens"));
  if (usage.input_tokens_details !== undefined)
    losses.push(add("input_tokens_details"));
  if (usage.output_tokens_details !== undefined)
    losses.push(add("output_tokens_details"));
  return losses;
}

interface RecoveredAnswer {
  readonly value: JsonValue;
  readonly loss?: Loss | undefined;
}

function recoverAnswer(
  id: string,
  raw: JsonValue,
  context: Readonly<Record<string, JevQuestion>>,
): RecoveredAnswer {
  if (typeof raw === "boolean" || typeof raw === "string" || isJsonNumber(raw))
    return recoverScalarAnswer(id, raw, context);
  if (isPlainObject(raw)) return recoverObjectAnswer(id, raw, context);
  fail(`answer ${id} must be an object or scalar value`);
}

const contradictionLoss = (id: string): Loss => ({
  path: `answers.${id}`,
  field: "type",
  reason: "unmapped-value",
  detail: "N-JEV-12: carried answer contradicts the original question",
});

const degradedType = (id: string): Loss => ({
  path: `answers.${id}`,
  field: "type",
  reason: "degraded",
  detail: "N-JEV-13: noul/score are ambiguous without question context",
});

const outOfRange = (id: string, field: string): Loss => ({
  path: `answers.${id}`,
  field,
  reason: "degraded",
  detail: "N-JEV-14: value kept, not rewritten",
});

/** Rebuilds an answer object with sorted keys at every depth; used only when
 * the encoder must rewrite the object (a synthesized type or a normalized
 * boolean noul). Carried answers stay verbatim. */
function rebuild(value: Record<string, JsonValue>): JsonObject {
  return sortRebuild(value) as JsonObject;
}

function recoverScalarAnswer(
  id: string,
  raw: JsonValue,
  context: Readonly<Record<string, JevQuestion>>,
): RecoveredAnswer {
  const question = hasOwnKey(context, id) ? context[id] : undefined;
  if (question !== undefined) {
    switch (question.type) {
      case QUESTION_TYPE_NOUL: {
        if (typeof raw === "boolean")
          return {
            value: rebuild({
              noul: fromValue(raw ? 1 : 0),
              type: QUESTION_TYPE_NOUL,
            }),
          };
        if (isJsonNumber(raw)) {
          const loss = decimalWithinUnitInterval(raw)
            ? undefined
            : outOfRange(id, "noul");
          return {
            value: rebuild({ noul: raw, type: QUESTION_TYPE_NOUL }),
            loss,
          };
        }
        break;
      }
      case QUESTION_TYPE_CHOICE: {
        if (typeof raw === "string") {
          const names = choiceNamesOfCriteria(question.criteria);
          const loss =
            names !== undefined && names.length > 0 && !names.includes(raw)
              ? outOfRange(id, "choice")
              : undefined;
          return {
            value: rebuild({ choice: raw, type: QUESTION_TYPE_CHOICE }),
            loss,
          };
        }
        break;
      }
      case QUESTION_TYPE_SCORE: {
        if (isJsonNumber(raw)) {
          const levels = criteriaCountOf(question.criteria);
          const loss =
            levels > 0 && !scoreWithinLevels(raw, levels)
              ? outOfRange(id, "score")
              : undefined;
          return {
            value: rebuild({ score: raw, type: QUESTION_TYPE_SCORE }),
            loss,
          };
        }
        break;
      }
    }
    return { value: raw, loss: contradictionLoss(id) };
  }
  if (typeof raw === "boolean")
    return {
      value: rebuild({
        noul: fromValue(raw ? 1 : 0),
        type: QUESTION_TYPE_NOUL,
      }),
    };
  if (typeof raw === "string")
    return {
      value: rebuild({ choice: raw, type: QUESTION_TYPE_CHOICE }),
    };
  if (isJsonNumber(raw))
    return {
      value: rebuild({ score: raw, type: QUESTION_TYPE_SCORE }),
      loss: degradedType(id),
    };
  fail(`answer ${id} must be an object or scalar value`);
}

function recoverObjectAnswer(
  id: string,
  obj: JsonObject,
  context: Readonly<Record<string, JevQuestion>>,
): RecoveredAnswer {
  const carried = typeof obj.type === "string" ? obj.type : undefined;
  if (carried === QUESTION_TYPE_BOX)
    return {
      value: obj,
      loss: {
        path: `answers.${id}`,
        field: "type",
        reason: "unsupported-semantic",
        detail: "N-JEV-5: experimental box answers are carried verbatim",
      },
    };
  const question = hasOwnKey(context, id) ? context[id] : undefined;
  if (
    question !== undefined &&
    carried !== undefined &&
    carried !== question.type
  )
    return { value: obj, loss: contradictionLoss(id) };
  let type = carried;
  if (type === undefined && question !== undefined) type = question.type;
  if (type === undefined) {
    if (obj.noul !== undefined) type = QUESTION_TYPE_NOUL;
    else if (obj.choice !== undefined) type = QUESTION_TYPE_CHOICE;
    else if (obj.score !== undefined) type = QUESTION_TYPE_SCORE;
    else fail(`answer ${id} carries no recognized value`);
  }

  // A typed answer carries verbatim; the encoder rewrites the object only
  // when it must synthesize the type or normalize a boolean noul.
  const rewrite =
    carried === undefined ||
    (type === QUESTION_TYPE_NOUL && typeof obj.noul === "boolean");
  if (!rewrite) {
    return { value: obj, loss: validateAnswer(id, obj, type!, question) };
  }
  const working: Record<string, JsonValue> = { ...obj, type };
  if (type === QUESTION_TYPE_NOUL && typeof working.noul === "boolean")
    working.noul = fromValue(working.noul ? 1 : 0);
  const loss = validateAnswer(id, working, type, question);
  return { value: rebuild(working), loss };
}

// validateAnswer reports the range/option losses of an answer against its
// question context; value-shape failures stay structural.
function validateAnswer(
  id: string,
  holder: Readonly<Record<string, JsonValue>>,
  type: string,
  question: JevQuestion | undefined,
): Loss | undefined {
  switch (type) {
    case QUESTION_TYPE_NOUL: {
      const leaf = holder.noul;
      if (typeof leaf === "boolean") return undefined;
      if (!isJsonNumber(leaf)) fail(`answer ${id} carries no noul value`);
      if (question === undefined || question.type !== QUESTION_TYPE_NOUL)
        return undefined;
      if (!decimalWithinUnitInterval(leaf)) return outOfRange(id, "noul");
      return undefined;
    }
    case QUESTION_TYPE_CHOICE: {
      if (typeof holder.choice !== "string")
        fail(`answer ${id} carries no choice value`);
      if (question === undefined) return undefined;
      const names = choiceNamesOfCriteria(question.criteria);
      if (
        names !== undefined &&
        names.length > 0 &&
        !names.includes(holder.choice)
      )
        return outOfRange(id, "choice");
      return undefined;
    }
    case QUESTION_TYPE_SCORE: {
      if (!isJsonNumber(holder.score))
        fail(`answer ${id} carries no score value`);
      if (question === undefined) return undefined;
      const levels = criteriaCountOf(question.criteria);
      if (levels > 0 && !scoreWithinLevels(holder.score, levels))
        return outOfRange(id, "score");
      return undefined;
    }
  }
  return undefined;
}

function choiceNamesOfCriteria(
  criteria: JevQuestion["criteria"],
): string[] | undefined {
  if (!isPlainObject(criteria)) return undefined;
  return Object.keys(criteria).sort();
}

function criteriaCountOf(criteria: JevQuestion["criteria"]): number {
  return isJsonArray(criteria) ? criteria.length : 0;
}
