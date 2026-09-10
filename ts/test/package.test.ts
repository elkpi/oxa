import assert from "node:assert/strict";
import test from "node:test";

import { json, packageName } from "../src/index.js";

test("exports its npm package name", () => {
  assert.equal(packageName, "@elkpi/oxa");
});

test("re-exports the lossless JSON namespace", () => {
  assert.equal(json.stringifyJson(json.parseJson("1.0")), "1.0");
});
