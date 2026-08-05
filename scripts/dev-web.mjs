#!/usr/bin/env node
import { spawn } from "node:child_process";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const port = process.env.FRONTEND_PORT || "3000";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// Resolve Next from the web package context so we run the version declared by
// @multica/web (apps/web), not whatever copy is hoisted to the repo root.
const webRequire = createRequire(resolve(repoRoot, "apps", "web", "package.json"));
const nextBin = webRequire.resolve("next/dist/bin/next");

const proc = spawn(process.execPath, [nextBin, "dev", "--webpack", "--port", port], {
  stdio: "inherit",
});

const cleanup = () => {
  if (proc.exitCode === null) {
    proc.kill();
  }
};

process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);

proc.on("exit", (code) => process.exit(code ?? 1));
