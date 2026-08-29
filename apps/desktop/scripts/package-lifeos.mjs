#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const packageScript = resolve(here, "package.mjs");
const configPath = resolve(here, "..", "electron-builder.lifeos.yml");
const appPath = resolve(here, "..", "dist", "lifeos", "mac-arm64", "LifeOS.app");

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
if (result.status !== 0) process.exit(result.status ?? 1);

if (!existsSync(appPath)) {
  console.error(`[package-lifeos] missing packaged app: ${appPath}`);
  process.exit(1);
}

const plistPath = resolve(appPath, "Contents", "Info.plist");
const plistValue = (key, format = "raw") =>
  execFileSync("plutil", ["-extract", key, format, "-o", "-", plistPath], {
    encoding: "utf8",
  }).trim();

const bundleName = plistValue("CFBundleDisplayName");
const bundleId = plistValue("CFBundleIdentifier");
const urlTypes = JSON.parse(plistValue("CFBundleURLTypes", "json"));
const schemes = urlTypes.flatMap((entry) => entry.CFBundleURLSchemes ?? []);
if (
  bundleName !== "LifeOS" ||
  bundleId !== "ai.lifeos.desktop" ||
  schemes.length !== 1 ||
  schemes[0] !== "lifeos"
) {
  console.error(
    `[package-lifeos] identity check failed: name=${bundleName} id=${bundleId} schemes=${schemes.join(",")}`,
  );
  process.exit(1);
}

console.log(
  `[package-lifeos] verified bundle identity → ${bundleName} (${bundleId}, lifeos://)`,
);
