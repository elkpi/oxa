import assert from "node:assert/strict";
import test from "node:test";

import * as jev from "../src/systemone/jev/index.js";
import { fromValue, stringifyJson } from "../src/json/index.js";
import type { Request, Response, TextBlock } from "../src/ir/index.js";

const INSTRUCTION_SUFFIX =
  "\n\nAnswer the structured questions that follow as a single JSON object. Keys MUST be exactly the question IDs given in the user message; values MUST match each question's type: noul answers are numbers in [0,1], choice answers are one of the listed option names, score answers are one of the listed level numbers. Output raw JSON only: no prose, no markdown fences.";

const context = {
  urgent: { type: "noul" },
  mood: { type: "choice", criteria: { good: "g", bad: "b" } },
  level: { type: "score", criteria: ["low", "high"] },
};

function responseWith(text: string): Response {
  return {
    id: "r",
    model: "m",
    content: [{ type: "text", text }] as TextBlock[],
    stop_reason: "end_turn",
    usage: { input_tokens: 1n, output_tokens: 2n },
  };
}

test("scalar answers recover their types from question context", () => {
  const result = jev.encodeResponse(
    responseWith(`{"urgent": 0.95, "mood": "surprise", "level": 1.5}`),
    { questions: context },
  );
  const degraded = result.losses.filter((l) => l.reason === "degraded");
  assert.equal(degraded.length, 2);
  assert.ok(degraded.every((l) => l.path.startsWith("answers.")));
  assert.deepEqual(JSON.parse(stringifyJson(result.value.answers!)), {
    level: { score: 1.5, type: "score" },
    mood: { choice: "surprise", type: "choice" },
    urgent: { noul: 0.95, type: "noul" },
  });
});

test("missing context answers and empty responses are structural errors", () => {
  assert.throws(
    () =>
      jev.encodeResponse(responseWith(`{"mood": "good"}`), {
        questions: context,
      }),
    /is missing from the response/,
  );
  assert.throws(
    () => jev.encodeResponse(responseWith(`null`)),
    /must be an object/,
  );
  assert.throws(
    () => jev.encodeResponse(responseWith(`{}`)),
    /carries no answers/,
  );
});

test("unknown answer properties are dropped with one loss", () => {
  const decoded = jev.decodeResponseJSON(
    `{"model":"m","answers":{"q":{"type":"noul","noul":1,"ext":"x"}},"usage":{"input_tokens":1,"output_tokens":2}}`,
  );
  const decodedText = (decoded.value.content[0] as TextBlock).text;
  assert.ok(!decodedText.includes("ext"));
  assert.equal(decoded.losses.length, 1);
  assert.equal(decoded.losses[0]!.field, "ext");
});

test("losses follow source encounter order in raw requests", () => {
  const raw = `{"z_ext": 1, "model": "m", "state": {}, "a_ext": 2, "questions": {"q": {"type": "noul"}}, "assets": {}}`;
  const result = jev.decodeRequestJSON(raw);
  assert.deepEqual(
    result.losses.map((l) => l.field),
    ["z_ext", "state", "a_ext", "assets"],
  );
});

test("scalar state shapes are structural errors", () => {
  assert.throws(
    () =>
      jev.decodeRequestJSON(
        `{"model":"m","state":true,"questions":{"q":{"type":"noul"}}}`,
      ),
    /must be a string, object, or array/,
  );
});

test("non-derived user content reports a loss on encode", () => {
  const request = {
    model: "m",
    system: [
      { type: "text" as const, text: `state text${INSTRUCTION_SUFFIX}` },
    ],
    messages: [
      {
        role: "user" as const,
        content: [{ type: "text" as const, text: "question q (noul): " }],
      },
      {
        role: "user" as const,
        content: [{ type: "text" as const, text: "CUSTOMER INPUT" }],
      },
    ],
    params: {
      response_format: {
        type: "json_schema" as const,
        name: "jev_answers",
        schema: {
          type: "object",
          properties: fromValue({
            q: { type: "number", minimum: 0, maximum: 1 },
          }),
          required: ["q"],
        },
      },
    },
  };
  const result = jev.encodeRequest(request as unknown as Request);
  const state = result.value.state as string;
  assert.ok(!state.includes("CUSTOMER INPUT"));
  assert.ok(!state.includes("question q"));
  assert.equal(result.losses.length, 1);
  assert.equal(result.losses[0]!.path, "messages[1]");
});

test("schema properties beyond the synthesized projection are skipped", () => {
  const request = {
    model: "m",
    system: [
      { type: "text" as const, text: `state text${INSTRUCTION_SUFFIX}` },
    ],
    messages: [],
    params: {
      response_format: {
        type: "json_schema" as const,
        name: "jev_answers",
        schema: {
          type: "object",
          properties: fromValue({
            ok: { type: "number", minimum: 0, maximum: 1 },
            weird: {
              type: "number",
              minimum: 0,
              maximum: 1,
              multipleOf: 0.5,
            },
          }),
          required: ["ok"],
        },
      },
    },
  };
  const result = jev.encodeRequest(request as unknown as Request);
  const questions = JSON.parse(
    JSON.stringify(result.value.questions),
  ) as Record<string, unknown>;
  assert.deepEqual(Object.keys(questions).sort(), ["ok"]);
  assert.equal(result.losses.length, 1);
  assert.equal(result.losses[0]!.path, "questions.weird");
});

test("the full client-side round trip reconstructs questions and answers", () => {
  const decoded = jev.decodeRequestJSON(`{
    "model": "jev-latest",
    "state": "The customer said the delivery was late.",
    "questions": {
      "sentiment": {"type": "choice", "instructions": "What is the overall sentiment?", "criteria": {"positive": "happy", "negative": "unhappy", "mixed": "both"}},
      "is_urgent": {"type": "noul", "instructions": "Does this convey urgency?"}
    }
  }`);
  const wire = jev.encodeRequest(decoded.value);
  assert.equal(wire.value.state, "The customer said the delivery was late.");
  const questionIds = JSON.parse(
    JSON.stringify(wire.value.questions),
  ) as Record<string, unknown>;
  assert.deepEqual(Object.keys(questionIds).sort(), ["is_urgent", "sentiment"]);
  const degraded = wire.losses.filter((l) => l.reason === "degraded");
  assert.equal(degraded.length, 1);

  const answers = jev.encodeResponse(
    {
      id: "r",
      model: "jev-latest",
      content: [
        {
          type: "text",
          text: `{"is_urgent": {"type": "noul", "noul": 0}, "sentiment": {"type": "choice", "choice": "mixed"}}`,
        },
      ],
      stop_reason: "end_turn",
      usage: { input_tokens: 10n, output_tokens: 5n },
    },
    {
      questions: JSON.parse(JSON.stringify(wire.value.questions)) as Record<
        string,
        jev.JevQuestion
      >,
    },
  );
  assert.equal(answers.losses.length, 0);
  assert.deepEqual(JSON.parse(stringifyJson(answers.value.answers!)), {
    is_urgent: { noul: 0, type: "noul" },
    sentiment: { choice: "mixed", type: "choice" },
  });
});

test("prototype-like question and answer IDs stay own properties", () => {
  const decoded = jev.decodeRequestJSON(
    `{"model":"m","state":"s","questions":{"__proto__":{"type":"noul","instructions":"q"}}}`,
  );
  const userMessage = decoded.value.messages![0]!;
  const userText = (userMessage.content[0] as TextBlock).text;
  assert.equal(userText, "question __proto__ (noul): q");
  const answered = jev.decodeResponseJSON(
    `{"model":"m","answers":{"__proto__":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":2}}`,
  );
  const answeredText = (answered.value.content[0] as TextBlock).text;
  assert.ok(answeredText.includes("__proto__"));
  // a toString key must not collide with Object.prototype on context lookup;
  // the box question is excluded from the completeness contract
  const wire = jev.encodeResponse(responseWith(`{"toString": "yes"}`), {
    questions: { urgent: { type: "box" } },
  });
  assert.deepEqual(JSON.parse(stringifyJson(wire.value.answers!)), {
    toString: { choice: "yes", type: "choice" },
  });
  assert.equal(wire.losses.length, 1);
  assert.equal(wire.losses[0]!.field, "toString");
});

test("mixed text and non-text user content is never treated as derived", () => {
  const request = {
    model: "m",
    system: [
      { type: "text" as const, text: `state text${INSTRUCTION_SUFFIX}` },
    ],
    messages: [
      {
        role: "user" as const,
        content: [
          { type: "text" as const, text: "question q (noul): " },
          { type: "image" as const, url: "https://example.com/i.png" },
        ],
      },
    ],
    params: {
      response_format: {
        type: "json_schema" as const,
        name: "jev_answers",
        schema: fromValue({
          type: "object",
          properties: {
            q: { type: "number", minimum: 0, maximum: 1 },
          },
          required: ["q"],
        }),
      },
    },
  };
  const result = jev.encodeRequest(request as unknown as Request);
  assert.equal(result.losses.length, 1);
  assert.equal(result.losses[0]!.path, "messages[0]");
});

test("typed answers carry verbatim without reordering nested values", () => {
  const result = jev.encodeResponse(
    responseWith(
      `{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}}`,
    ),
  );
  const rendered = stringifyJson(result.value.answers!);
  // the carried probabilities object keeps its source key order and tokens
  assert.ok(rendered.includes('"probabilities":{"z":0.2,"a":0.8}'), rendered);
});

test("range validation uses exact decimal comparison", () => {
  const noul = jev.encodeResponse(responseWith(`{"q": 1.0000000000000001}`), {
    questions: { q: { type: "noul" } },
  });
  const noulLosses = noul.losses.filter((l) => l.reason === "degraded");
  assert.equal(noulLosses.length, 1);
  assert.equal(noulLosses[0]!.field, "noul");

  const scored = jev.encodeResponse(responseWith(`{"s": 1.0000000000000001}`), {
    questions: { s: { type: "score", criteria: ["low", "high"] } },
  });
  const scoreLosses = scored.losses.filter((l) => l.reason === "degraded");
  assert.equal(scoreLosses.length, 1);
  assert.equal(scoreLosses[0]!.field, "score");
});

test("a whole unsupported question suppresses its inner field audit", () => {
  const result = jev.decodeRequestJSON(
    `{"model":"m","state":"s","questions":{"box":{"type":"box","bogus":1},"ok":{"type":"noul"}}}`,
  );
  const boxLosses = result.losses.filter((l) => l.path === "questions.box");
  assert.equal(boxLosses.length, 1);
  assert.equal(boxLosses[0]!.field, "type");
});

test("integer-like keys sort lexicographically in canonical text", () => {
  const decoded = jev.decodeRequestJSON(
    `{"model":"m","state":{"10":"ten","2":"two"},"questions":{"q":{"type":"noul"}}}`,
  );
  const systemText = (decoded.value.system![0] as TextBlock).text;
  assert.ok(systemText.startsWith('{"10":"ten","2":"two"}'), systemText);
});

test("an empty model name is a structural error", () => {
  assert.throws(
    () =>
      jev.decodeRequestJSON(
        `{"model":"","state":"s","questions":{"q":{"type":"noul"}}}`,
      ),
    /model is required/,
  );
  assert.throws(
    () =>
      jev.decodeResponseJSON(
        `{"model":"","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`,
      ),
    /model is required/,
  );
});
