import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/openai/responses/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("runs every Responses non-stream vector", () => {
  assert.ok(
    runNonstreamVectors("responses", {
      decodeRequest,
      decodeResponse,
      encodeRequest,
      encodeResponse,
    }) > 0,
  );
});
