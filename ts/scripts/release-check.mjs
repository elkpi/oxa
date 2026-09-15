import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const packageRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const repositoryRoot = path.resolve(packageRoot, "..");
const goRoot = path.join(repositoryRoot, "go");
const npmCli = process.env.npm_execpath;

if (npmCli === undefined) {
  throw new Error("release:check must be invoked through npm");
}

function run(label, command, args, cwd) {
  console.log(`\n==> ${label}`);
  execFileSync(command, args, { cwd, stdio: "inherit" });
}

function runNpm(label, script) {
  run(label, process.execPath, [npmCli, "run", script], packageRoot);
}

run(
  "vectors and manifest",
  "go",
  ["run", "./cmd/veccheck", "-root", "..", "-check-manifest"],
  goRoot,
);
runNpm("generated declarations", "generate:check");
runNpm("format", "fmt");
runNpm("types", "check");
runNpm("Node", "test:node");
runNpm("Web", "test:web");
runNpm("package contents", "test:package");
runNpm("clean ESM and TypeScript consumer", "test:consumer");
run("Go tests", "go", ["test", "-count=1", "./..."], goRoot);
run("Go build", "go", ["build", "./..."], goRoot);
run("Go vet", "go", ["vet", "./..."], goRoot);

console.log("\n==> Go format");
const unformatted = execFileSync("gofmt", ["-l", "."], {
  cwd: goRoot,
  encoding: "utf8",
});
if (unformatted.length > 0) {
  process.stderr.write(unformatted);
  throw new Error("gofmt reported unformatted files");
}

run("Go module path", "make", ["check-modulepath"], repositoryRoot);
console.log("\nrelease checks passed; no package was published");
