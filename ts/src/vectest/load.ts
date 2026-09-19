import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

import { OxaError } from "../error.js";
import {
  isJsonArray,
  isJsonNumber,
  parseJson,
  type JsonObject,
} from "../json/index.js";

export interface VectorFixture {
  readonly name: string;
  readonly path: string;
  readonly document: JsonObject;
}

/** Finds the nearest checkout root; returns undefined when installed as a dependency. */
export function findRepoRoot(start: string): string | undefined {
  let current = resolve(start);
  if (existsSync(current) && !statSync(current).isDirectory())
    current = dirname(current);
  for (;;) {
    if (
      existsSync(join(current, ".git")) &&
      existsSync(join(current, "vectors"))
    )
      return current;
    const parent = dirname(current);
    if (parent === current) return undefined;
    current = parent;
  }
}

/** Loads all golden vectors in deterministic path order. */
export function loadVectors(root: string): readonly VectorFixture[] {
  const directory = join(root, "vectors");
  return vectorFiles(directory)
    .sort()
    .map((path) => loadVector(path, root))
    .filter((v) => v.document.spec_version === "0.1.0");
}

function vectorFiles(directory: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...vectorFiles(path));
    else if (
      entry.isFile() &&
      entry.name.endsWith(".json") &&
      entry.name !== "manifest.json"
    )
      files.push(path);
  }
  return files;
}

function loadVector(path: string, root: string): VectorFixture {
  const document = object(parseJson(readFileSync(path, "utf8")), path);
  if (typeof document.name !== "string")
    throw new OxaError(
      "type-violation",
      `${path}: vector name must be a string`,
    );
  return { name: document.name, path: path.slice(root.length + 1), document };
}

function object(value: unknown, name: string): JsonObject {
  if (
    value === null ||
    typeof value !== "object" ||
    isJsonArray(value) ||
    isJsonNumber(value)
  )
    throw new OxaError("type-violation", `${name}: expected a JSON object`);
  return value as JsonObject;
}
