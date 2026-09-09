import assert from "node:assert/strict";
import test from "node:test";

import { packageName } from "../src/index.js";

test("exports its npm package name", () => {
  assert.equal(packageName, "@elkpi/oxa");
});
