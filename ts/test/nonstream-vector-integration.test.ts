import assert from "node:assert/strict";
import test from "node:test";

import * as anthropic from "../src/anthropic/messages/index.js";
import * as chatcompletions from "../src/openai/chatcompletions/index.js";
import * as responses from "../src/openai/responses/index.js";
import {
  runCrossRequestVectors,
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
    20,
  );
});

test("runs Chat Completions cross request vectors across faces", () => {
  assert.equal(
    runCrossRequestVectors("chatcompletions", chatcompletions, {
      anthropic,
      responses,
    }),
    11,
  );
});

test("runs Anthropic cross request vectors across faces including native roundtrip", () => {
  assert.equal(
    runCrossRequestVectors("anthropic", anthropic, {
      anthropic,
      chatcompletions,
      responses,
    }),
    25,
  );
});
