import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const packageRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const npmCli = process.env.npm_execpath;

if (npmCli === undefined) {
  throw new Error("test:package must be invoked through npm");
}

function runNpm(args, options) {
  return execFileSync(process.execPath, [npmCli, ...args], options);
}

const staleOutput = path.join(packageRoot, "dist", "stale-package-artifact.js");
mkdirSync(path.dirname(staleOutput), { recursive: true });
writeFileSync(staleOutput, "export const stale = true;\n");
let packResult;
try {
  packResult = JSON.parse(
    runNpm(["pack", "--json", "--dry-run", "--silent"], {
      cwd: packageRoot,
      encoding: "utf8",
    }),
  );
} finally {
  rmSync(staleOutput, { force: true });
}
const packed = Array.isArray(packResult)
  ? packResult
  : Object.values(packResult);
assert.equal(packed.length, 1, "npm pack should describe exactly one package");

const files = packed[0].files.map(({ path: file }) => file).sort();
const requiredAssets = ["LICENSE", "NOTICE", "README.md", "package.json"];
for (const asset of requiredAssets) {
  assert.ok(files.includes(asset), `packed package is missing ${asset}`);
}

const publicDistRoots = [
  "dist/anthropic/messages/",
  "dist/ir/",
  "dist/json/",
  "dist/openai/chatcompletions/",
  "dist/openai/responses/",
  "dist/sse/",
  "dist/stream/",
];
const publicRootFiles =
  /dist\/(?:error|index|loss|modelmap)\.(?:d\.ts|d\.ts\.map|js|js\.map)$/u;
const compiledExtension = /\.(?:d\.ts|d\.ts\.map|js|js\.map)$/u;
for (const file of files) {
  const isAsset = requiredAssets.includes(file);
  const isPublicRoot = publicRootFiles.test(file);
  const isPublicDirectory =
    publicDistRoots.some((prefix) => file.startsWith(prefix)) &&
    compiledExtension.test(file);
  assert.ok(
    isAsset || isPublicRoot || isPublicDirectory,
    `unexpected packed file: ${file}`,
  );
}

const metadata = JSON.parse(
  readFileSync(path.join(packageRoot, "package.json"), "utf8"),
);
assert.equal(metadata.version, "1.0.1");
assert.equal(metadata.type, "module");
assert.equal(metadata.main, "./dist/index.js");
assert.equal(metadata.types, "./dist/index.d.ts");
assert.equal(
  metadata.scripts.prepack,
  "npm run generate:check && npm run build",
);

const expectedExports = {
  ".": ["./dist/index.js", "./dist/index.d.ts"],
  "./error": ["./dist/error.js", "./dist/error.d.ts"],
  "./loss": ["./dist/loss.js", "./dist/loss.d.ts"],
  "./modelmap": ["./dist/modelmap.js", "./dist/modelmap.d.ts"],
  "./json": ["./dist/json/index.js", "./dist/json/index.d.ts"],
  "./ir": ["./dist/ir/index.js", "./dist/ir/index.d.ts"],
  "./sse": ["./dist/sse/index.js", "./dist/sse/index.d.ts"],
  "./stream": ["./dist/stream/index.js", "./dist/stream/index.d.ts"],
  "./openai/chatcompletions": [
    "./dist/openai/chatcompletions/index.js",
    "./dist/openai/chatcompletions/index.d.ts",
  ],
  "./openai/responses": [
    "./dist/openai/responses/index.js",
    "./dist/openai/responses/index.d.ts",
  ],
  "./anthropic/messages": [
    "./dist/anthropic/messages/index.js",
    "./dist/anthropic/messages/index.d.ts",
  ],
};
assert.deepEqual(
  Object.keys(metadata.exports).sort(),
  Object.keys(expectedExports).sort(),
);
for (const [subpath, [importTarget, typesTarget]] of Object.entries(
  expectedExports,
)) {
  assert.equal(metadata.exports[subpath].import, importTarget);
  assert.equal(metadata.exports[subpath].types, typesTarget);
  assert.ok(
    files.includes(importTarget.slice(2)),
    `${subpath} JavaScript target is not packed`,
  );
  assert.ok(
    files.includes(typesTarget.slice(2)),
    `${subpath} types target is not packed`,
  );
}
