import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/anthropic/messages/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("runs every Anthropic non-stream vector", () => {
  assert.ok(
    runNonstreamVectors("anthropic", {
      decodeRequest,
      decodeResponse,
      encodeRequest,
      encodeResponse,
    }) > 0,
  );
});
