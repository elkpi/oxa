import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import path from "node:path";

const packageRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
const node = process.execPath;
const temporaryRoot = mkdtempSync(
  path.join(tmpdir(), "oxa-typescript-consumer-"),
);
const npmConfig = path.join(temporaryRoot, ".npmrc");
writeFileSync(npmConfig, "");
const npmEnvironment = { ...process.env, NPM_CONFIG_USERCONFIG: npmConfig };
delete npmEnvironment.npm_config_allow_scripts;
delete npmEnvironment.NPM_CONFIG_ALLOW_SCRIPTS;

try {
  const packDirectory = path.join(temporaryRoot, "package");
  const consumerDirectory = path.join(temporaryRoot, "consumer");
  mkdirSync(packDirectory);
  mkdirSync(consumerDirectory);
  const packResult = JSON.parse(
    execFileSync(
      npm,
      ["pack", "--json", "--silent", "--pack-destination", packDirectory],
      {
        cwd: packageRoot,
        encoding: "utf8",
        env: npmEnvironment,
      },
    ),
  );
  const packed = Array.isArray(packResult)
    ? packResult
    : Object.values(packResult);
  const tarball = path.join(packDirectory, packed[0].filename);

  writeFileSync(
    path.join(consumerDirectory, "package.json"),
    JSON.stringify({ private: true, type: "module" }),
  );
  execFileSync(
    npm,
    ["install", "--no-audit", "--no-fund", "--package-lock=false", tarball],
    { cwd: consumerDirectory, env: npmEnvironment, stdio: "inherit" },
  );

  writeFileSync(
    path.join(consumerDirectory, "runtime.mjs"),
    `import assert from "node:assert/strict";
import { packageName, ir, json, sse, stream, chatcompletions, responses, anthropic } from "@elkpi/oxa";
import * as error from "@elkpi/oxa/error";
import * as loss from "@elkpi/oxa/loss";
import * as modelmap from "@elkpi/oxa/modelmap";
import * as irSubpath from "@elkpi/oxa/ir";
import * as jsonSubpath from "@elkpi/oxa/json";
import * as sseSubpath from "@elkpi/oxa/sse";
import * as streamSubpath from "@elkpi/oxa/stream";
import * as chatSubpath from "@elkpi/oxa/openai/chatcompletions";
import * as responsesSubpath from "@elkpi/oxa/openai/responses";
import * as anthropicSubpath from "@elkpi/oxa/anthropic/messages";

assert.equal(packageName, "@elkpi/oxa");
for (const [rootModule, subpathModule] of [
  [ir, irSubpath], [json, jsonSubpath], [sse, sseSubpath], [stream, streamSubpath],
  [chatcompletions, chatSubpath], [responses, responsesSubpath], [anthropic, anthropicSubpath],
]) {
  assert.deepEqual(Object.keys(rootModule), Object.keys(subpathModule));
}
assert.equal(typeof error.OxaError, "function");
assert.deepEqual(Object.keys(loss), []);
assert.equal(modelmap.mapModel(undefined, "model"), "model");
console.log("clean ESM consumer import passed");
`,
  );
  writeFileSync(
    path.join(consumerDirectory, "consumer.ts"),
    `import { packageName, mapModel, type Loss } from "@elkpi/oxa";
import { OxaError, type OxaErrorCode } from "@elkpi/oxa/error";
import type { ConversionResult } from "@elkpi/oxa/loss";
import type { ModelMapper } from "@elkpi/oxa/modelmap";
import type { Request } from "@elkpi/oxa/ir";
import type { JsonText } from "@elkpi/oxa/json";
import type { SseEvent } from "@elkpi/oxa/sse";
import type { StreamDecoder } from "@elkpi/oxa/stream";
import type { ChatCompletionsChunk } from "@elkpi/oxa/openai/chatcompletions";
import type { ResponsesStreamEvent } from "@elkpi/oxa/openai/responses";
import type { AnthropicStreamEvent } from "@elkpi/oxa/anthropic/messages";

const mapper: ModelMapper = (model) => model;
const result: ConversionResult<string> = { value: mapModel(mapper, packageName), losses: [] };
const values: [readonly Loss[], OxaError, OxaErrorCode, Request?, JsonText?, SseEvent?, StreamDecoder<unknown, unknown>?, ChatCompletionsChunk?, ResponsesStreamEvent?, AnthropicStreamEvent?] = [
  result.losses, new OxaError("invalid-json", "consumer"), "invalid-json",
];
void values;
`,
  );
  writeFileSync(
    path.join(consumerDirectory, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: {
        exactOptionalPropertyTypes: true,
        module: "NodeNext",
        moduleResolution: "NodeNext",
        noEmit: true,
        skipLibCheck: false,
        strict: true,
        target: "ES2022",
      },
      files: ["consumer.ts"],
    }),
  );

  execFileSync(
    node,
    [
      path.join(packageRoot, "node_modules", "typescript", "bin", "tsc"),
      "-p",
      "tsconfig.json",
    ],
    { cwd: consumerDirectory, stdio: "inherit" },
  );
  execFileSync(node, ["runtime.mjs"], {
    cwd: consumerDirectory,
    stdio: "inherit",
  });
  console.log("clean TypeScript consumer typecheck passed");
} finally {
  rmSync(temporaryRoot, { recursive: true, force: true });
}
