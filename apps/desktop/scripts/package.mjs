#!/usr/bin/env node
// Wrapper around `electron-builder` that keeps the Desktop version in
// lockstep with the CLI. Both are derived from `git describe --tags
// --always --dirty` — the same source GoReleaser reads for the CLI
// binary via the `main.version` ldflag — so a single `vX.Y.Z` tag push
// produces matching CLI and Desktop versions.
//
// Runs the existing bundle-cli.mjs first (so the Go binary is compiled
// and copied into resources/bin/), then invokes electron-builder with
// `-c.extraMetadata.version=<derived>` so the override applies at build
// time without mutating the tracked package.json.
//
// Extra CLI args after `pnpm package --` are forwarded to electron-builder
// unchanged (e.g. `--mac --arm64`).

import { execFileSync, spawnSync, execSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, "..");

function sh(cmd) {
  try {
    return execSync(cmd, { encoding: "utf-8" }).trim();
  } catch {
    return "";
  }
}

// Same derivation as bundle-cli.mjs / GoReleaser's {{.Version}}:
//   - on a tag commit          → "0.1.36"
//   - between tags             → "0.1.35-14-gf1415e96"  (semver prerelease)
//   - dirty working tree       → "0.1.35-14-gf1415e96-dirty"
//   - no tags in history       → "0.0.0-<shortcommit>"  (fallback)
// Leading `v` is stripped to match semver / package.json format.
function deriveVersion() {
  const raw = sh("git describe --tags --always --dirty");
  if (!raw) return null;
  const stripped = raw.replace(/^v/, "");
  if (!/^\d/.test(stripped)) {
    // No reachable tag — `git describe` fell back to just the commit hash.
    return `0.0.0-${stripped}`;
  }
  return stripped;
}

// Step 1: build + bundle the Go CLI via the existing script.
execFileSync("node", [resolve(here, "bundle-cli.mjs")], {
  stdio: "inherit",
  cwd: desktopRoot,
});

// Step 2: derive the version that should be written into the app.
const version = deriveVersion();
if (version) {
  console.log(`[package] Desktop version → ${version} (from git describe)`);
} else {
  console.warn(
    "[package] could not derive version from git; falling back to package.json",
  );
}

// Step 3: invoke electron-builder with the override (if we have one) plus
// any passthrough args the caller appended (--mac, --arm64, etc.).
const passthrough = process.argv.slice(2);
const builderArgs = [];
if (version) builderArgs.push(`-c.extraMetadata.version=${version}`);
builderArgs.push(...passthrough);

const result = spawnSync("electron-builder", builderArgs, {
  stdio: "inherit",
  cwd: desktopRoot,
  shell: true,
});

process.exit(result.status ?? 1);
