import assert from "node:assert/strict";
import test from "node:test";

import * as anthropic from "../src/anthropic/messages/index.js";
import * as chatcompletions from "../src/openai/chatcompletions/index.js";
import * as responses from "../src/openai/responses/index.js";
import {
  runCrossRequestVectors,
  runCrossResponseVectors,
  runNonstreamVectors,
} from "./nonstream-vector-runner.js";

test("binds every face's non-stream vectors through one runner", () => {
  const groups = [
    ["chatcompletions", chatcompletions],
    ["responses", responses],
    ["anthropic", anthropic],
  ] as const;
  for (const [face, adapter] of groups)
    assert.ok(runNonstreamVectors(face, adapter) > 0, face);
});

test("preserves Responses instructions and composed losses across faces", () => {
  assert.equal(
    runCrossRequestVectors("responses", responses, {
      anthropic,
      chatcompletions,
      responses,
    }),
    26,
  );
});

test("runs Chat Completions cross request vectors across faces", () => {
  assert.equal(
    runCrossRequestVectors("chatcompletions", chatcompletions, {
      anthropic,
      responses,
    }),
    15,
  );
});

test("runs Anthropic cross request vectors across faces including native roundtrip", () => {
  assert.equal(
    runCrossRequestVectors("anthropic", anthropic, {
      anthropic,
      chatcompletions,
      responses,
    }),
    27,
  );
});

test("runs Responses cross response vectors across faces", () => {
  assert.equal(
    runCrossResponseVectors("responses", responses, {
      anthropic,
      chatcompletions,
      responses,
    }),
    10,
  );
});

test("runs Chat Completions cross response vectors across faces", () => {
  assert.equal(
    runCrossResponseVectors("chatcompletions", chatcompletions, {
      anthropic,
      responses,
    }),
    4,
  );
});

test("runs Anthropic cross response vectors across faces", () => {
  assert.equal(
    runCrossResponseVectors("anthropic", anthropic, {
      anthropic,
      chatcompletions,
      responses,
    }),
    8,
  );
});
