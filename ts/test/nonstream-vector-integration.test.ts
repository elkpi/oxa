import assert from "node:assert/strict";
import test from "node:test";

import * as anthropic from "../src/anthropic/messages/index.js";
import * as chatcompletions from "../src/openai/chatcompletions/index.js";
import * as responses from "../src/openai/responses/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("binds every face's non-stream vectors through one runner", () => {
  const groups = [
    ["chatcompletions", chatcompletions],
    ["responses", responses],
    ["anthropic", anthropic],
  ] as const;
  for (const [face, adapter] of groups)
    assert.ok(runNonstreamVectors(face, adapter) > 0, face);
});
