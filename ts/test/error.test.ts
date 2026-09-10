import assert from "node:assert/strict";
import test from "node:test";

import { OxaError } from "../src/error.js";

test("preserves OxaError code and cause", () => {
  const cause = new Error("bad event");
  const error = new OxaError("stream-grammar", "expected message_start", {
    cause,
  });

  assert.equal(error.name, "OxaError");
  assert.equal(error.code, "stream-grammar");
  assert.equal(error.cause, cause);
});
