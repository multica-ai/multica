#!/usr/bin/env node
import { spawn } from "node:child_process";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const port = process.env.FRONTEND_PORT || "3000";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const nextBin = resolve(repoRoot, "node_modules", "next", "dist", "bin", "next");

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
