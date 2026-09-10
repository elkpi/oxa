import assert from "node:assert/strict";
import test from "node:test";

import { mapModel } from "../src/modelmap.js";

test("uses identity mapping when no mapper matches", () => {
  assert.equal(mapModel(undefined, "gpt-test"), "gpt-test");
  assert.equal(
    mapModel((model) => (model === "a" ? "b" : undefined), "a"),
    "b",
  );
  assert.equal(
    mapModel((model) => (model === "a" ? "b" : undefined), "other"),
    "other",
  );
});
