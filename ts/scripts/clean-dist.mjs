import { rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const distributionDirectory = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../dist",
);
rmSync(distributionDirectory, { force: true, recursive: true });
