import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";

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
  if (!specifier.startsWith(".")) return undefined;
  const target = resolve(dirname(file), specifier)
    .replace(/\\/g, "/")
    .replace(/\.(?:js|ts)$/, "")
    .replace(/\/index$/, "");
  const src = target.slice(target.lastIndexOf("/src/") + 5);
  if (src === "ir" || src.startsWith("ir/")) return "ir";
  return faceRoots.find((root) => src === root || src.startsWith(`${root}/`));
}

export function forbiddenImports(file: string, source: string): string[] {
  const sourceArea = moduleArea(file);
  const inSse = file.replace(/^src\//, "").startsWith("sse/");
  const violations: string[] = [];
  const imports = /(?:\bfrom\s*|\bimport\s*(?:\(\s*)?)["']([^"']+)["']/g;
  for (const match of source.matchAll(imports)) {
    const targetArea = importedArea(file, match[1] ?? "");
    if (targetArea === undefined) continue;
    if (
      sourceArea !== undefined &&
      targetArea !== "ir" &&
      targetArea !== sourceArea
    ) {
      violations.push(
        `${file} imports ${targetArea} from another protocol face`,
      );
    }
    if (inSse) {
      violations.push(
        `${file} imports ${targetArea} from the opaque SSE adapter`,
      );
    }
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
    const name = `src/${relative(root, file).replace(/\\/g, "/")}`;
    violations.push(...forbiddenImports(name, await readFile(file, "utf8")));
  }
  return violations;
}
