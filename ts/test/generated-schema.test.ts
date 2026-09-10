import assert from "node:assert/strict";
import test from "node:test";

import type { LossSchemaDocument } from "../src/generated/loss-schema.js";

const validLoss = {
  path: "output[0]",
  field: "type",
  reason: "unsupported-semantic",
} satisfies LossSchemaDocument;

const invalidLoss: LossSchemaDocument = {
  path: "output[0]",
  field: "type",
  // @ts-expect-error The schema limits loss reasons to the declared vocabulary.
  reason: "not-a-loss-reason",
};
void invalidLoss;

test("generates structural declarations from loss schema", () => {
  assert.equal(validLoss.reason, "unsupported-semantic");
});
