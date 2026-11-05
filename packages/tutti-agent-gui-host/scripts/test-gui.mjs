import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const gui = resolve(root, "vendor/tutti/packages/agent/gui");
const vitest = resolve(dirname(fileURLToPath(import.meta.resolve("vitest/package.json"))), "vitest.mjs");
const result = spawnSync(process.execPath, [vitest, "run", ...process.argv.slice(2)], {
  cwd: gui, stdio: "inherit", env: { ...process.env, NO_COLOR: "1", LC_ALL: "C" }
});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;
