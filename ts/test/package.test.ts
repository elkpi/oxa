import assert from "node:assert/strict";
import test from "node:test";

import { ir, json, packageName } from "../src/index.js";

test("exports its npm package name", () => {
  assert.equal(packageName, "@elkpi/oxa");
});

test("re-exports the lossless JSON namespace", () => {
  assert.equal(json.stringifyJson(json.parseJson("1.0")), "1.0");
});

test("re-exports the IR namespace", () => {
  assert.equal(ir.specVersion, "0.1.0");
});
