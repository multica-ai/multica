#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const packageScript = resolve(here, "package.mjs");
const configPath = resolve(here, "..", "electron-builder.lifeos.yml");

const result = spawnSync(
  process.execPath,
  [
    packageScript,
    "--mac",
    "--arm64",
    "--publish",
    "never",
    "--config",
    configPath,
    ...process.argv.slice(2),
  ],
  {
    cwd: resolve(here, ".."),
    stdio: "inherit",
    env: {
      ...process.env,
      LIFEOS_DESKTOP_MODE: "true",
      CSC_IDENTITY_AUTO_DISCOVERY:
        process.env.CSC_IDENTITY_AUTO_DISCOVERY ?? "false",
    },
  },
);

if (result.error) {
  console.error("[package-lifeos] failed:", result.error.message);
  process.exit(1);
}
process.exit(result.status ?? 1);
