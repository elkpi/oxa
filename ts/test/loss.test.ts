import assert from "node:assert/strict";
import test from "node:test";

import type { ConversionResult, Loss } from "../src/loss.js";

test("exposes readonly loss and conversion result shapes", () => {
  const losses: readonly Loss[] = [
    {
      path: "messages[0]",
      field: "cache_control",
      reason: "unmapped-field",
    },
  ];
  const result: ConversionResult<string> = { value: "ok", losses };

  assert.equal(result.value, "ok");
  assert.equal(result.losses[0]?.reason, "unmapped-field");
});
