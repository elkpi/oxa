import { readdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const files = (await readdir(new URL("../dist-test/test/", import.meta.url)))
  .filter((file) => file.endsWith(".test.js"))
  .sort()
  .map((file) =>
    fileURLToPath(new URL(`../dist-test/test/${file}`, import.meta.url)),
  );
const child = spawn(process.execPath, ["--test", ...files], {
  stdio: "inherit",
});
child.on("exit", (code) => (process.exitCode = code ?? 1));
