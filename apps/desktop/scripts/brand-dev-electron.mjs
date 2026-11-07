#!/usr/bin/env node
// Rebrand the bundled Electron.app's Info.plist so `pnpm dev:desktop`
// shows "Multica Canary" in the menu bar, Cmd+Tab switcher, and
// Activity Monitor. On macOS these titles come from CFBundleName at
// launch time — `app.setName()` cannot override them at runtime, so
// patching the plist in node_modules is the only working fix.
//
// Idempotent: runs on every dev launch and no-ops once the plist already
// matches. The patch is isolated to this worktree's node_modules — we
// unlink the file before rewriting so we never mutate a pnpm-store inode
// shared with another project.
//
// In a worktree, scripts/dev.mjs sets DESKTOP_APP_SUFFIX so the name becomes
// "Multica Canary <suffix>" — distinguishable in Cmd+Tab and matching the app
// name src/main/index.ts derives from the same env var.

import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

if (process.platform !== "darwin") process.exit(0);

const DESIRED_NAME = process.env.DESKTOP_APP_SUFFIX
  ? `Multica Canary ${process.env.DESKTOP_APP_SUFFIX}`
  : "Multica Canary";

const require = createRequire(import.meta.url);
// `require('electron')` returns the path to the executable
// (.../Electron.app/Contents/MacOS/Electron). Walk up to Contents/Info.plist.
const electronBin = require("electron");
const plistPath = resolve(electronBin, "../../Info.plist");

function plistGet(key) {
  try {
    return execFileSync(
      "/usr/libexec/PlistBuddy",
      ["-c", `Print :${key}`, plistPath],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    ).trim();
  } catch {
    return "";
  }
}

function plistSet(key, value) {
  try {
    execFileSync("/usr/libexec/PlistBuddy", [
      "-c",
      `Set :${key} ${value}`,
      plistPath,
    ]);
  } catch {
    execFileSync("/usr/libexec/PlistBuddy", [
      "-c",
      `Add :${key} string ${value}`,
      plistPath,
    ]);
  }
}

// The verification host gives each environment its own bundle ID so Computer
// Use can target that one app; ordinary dev runs keep Electron's stock ID.
const DESIRED_BUNDLE_ID = process.env.DESKTOP_BUNDLE_ID;
if (DESIRED_BUNDLE_ID && !/^[A-Za-z0-9.-]+$/.test(DESIRED_BUNDLE_ID)) {
  throw new Error(`Invalid DESKTOP_BUNDLE_ID: ${DESIRED_BUNDLE_ID}`);
}

if (
  plistGet("CFBundleName") === DESIRED_NAME &&
  plistGet("CFBundleDisplayName") === DESIRED_NAME &&
  (!DESIRED_BUNDLE_ID || plistGet("CFBundleIdentifier") === DESIRED_BUNDLE_ID)
) {
  process.exit(0);
}

// Break any pnpm hardlink to the global store: read, unlink, rewrite.
// PlistBuddy would otherwise write through the hardlink and mutate the
// shared store file (and every other project's Electron.app with it).
const original = readFileSync(plistPath);
unlinkSync(plistPath);
writeFileSync(plistPath, original);

plistSet("CFBundleName", DESIRED_NAME);
plistSet("CFBundleDisplayName", DESIRED_NAME);
if (DESIRED_BUNDLE_ID) plistSet("CFBundleIdentifier", DESIRED_BUNDLE_ID);

console.log(`[brand-dev-electron] ${plistPath} → CFBundleName="${DESIRED_NAME}"`);
