import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import ts from "typescript";

const faceRoots = [
  "openai/chatcompletions",
  "openai/responses",
  "anthropic/messages",
] as const;

function moduleArea(file: string): string | undefined {
  const relativeFile = file.replace(/^src\//, "");
  return faceRoots.find(
    (root) => relativeFile === root || relativeFile.startsWith(`${root}/`),
  );
}

function importedArea(file: string, specifier: string): string | undefined {
  const src = specifier.startsWith("@elkpi/oxa/")
    ? specifier.slice("@elkpi/oxa/".length).replace(/\/index$/, "")
    : specifier.startsWith(".")
      ? resolve(dirname(file), specifier)
          .replace(/\.(?:js|ts)$/, "")
          .replace(/\/index$/, "")
          .replace(/^.*\/src\//, "")
      : undefined;
  if (src === undefined) return undefined;
  if (src === "ir" || src.startsWith("ir/")) return "ir";
  return faceRoots.find((root) => src === root || src.startsWith(`${root}/`));
}

function moduleSpecifiers(source: string): readonly string[] {
  const parsed = ts.createSourceFile(
    "architecture.ts",
    source,
    ts.ScriptTarget.Latest,
    true,
  );
  const specifiers: string[] = [];
  const visit = (node: ts.Node): void => {
    if (
      (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) &&
      node.moduleSpecifier !== undefined &&
      ts.isStringLiteralLike(node.moduleSpecifier)
    )
      specifiers.push(node.moduleSpecifier.text);
    ts.forEachChild(node, visit);
  };
  visit(parsed);
  return specifiers;
}

export function forbiddenImports(file: string, source: string): string[] {
  const sourceArea = moduleArea(file);
  const inSse = file.replace(/^src\//, "").startsWith("sse/");
  const violations: string[] = [];
  for (const specifier of moduleSpecifiers(source)) {
    const targetArea = importedArea(file, specifier);
    if (targetArea === undefined) continue;
    if (
      sourceArea !== undefined &&
      targetArea !== "ir" &&
      targetArea !== sourceArea
    )
      violations.push(
        `${file} imports ${targetArea} from another protocol face`,
      );
    if (inSse)
      violations.push(
        `${file} imports ${targetArea} from the opaque SSE adapter`,
      );
  }
  return violations;
}

async function sourceFiles(root: string): Promise<string[]> {
  const files: string[] = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) files.push(...(await sourceFiles(path)));
    else if (entry.isFile() && entry.name.endsWith(".ts")) files.push(path);
  }
  return files.sort();
}

export async function findArchitectureViolations(
  root = resolve(process.cwd(), "src"),
): Promise<string[]> {
  const violations: string[] = [];
  for (const file of await sourceFiles(root)) {
    const name = `src/${relative(root, file)}`;
    violations.push(...forbiddenImports(name, await readFile(file, "utf8")));
  }
  return violations;
}
