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
//
// The `normalizeGitVersion` helper is exported so tests can cover the
// version-derivation logic without shelling out.

import { execFileSync, spawnSync, execSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, "..");

function sh(cmd) {
  try {
    return execSync(cmd, { encoding: "utf-8" }).trim();
  } catch {
    return "";
  }
}

/**
 * Pure transformation from the `git describe --tags --always --dirty`
 * output to the value we feed into electron-builder's extraMetadata.version.
 *
 *   - empty input              → null   (caller should fall back)
 *   - "v0.1.36"                → "0.1.36"
 *   - "v0.1.35-14-gf1415e96"   → "0.1.35-14-gf1415e96"  (semver prerelease)
 *   - "v0.1.35-…-dirty"        → same, dirty suffix preserved
 *   - "f1415e96" (no tag)      → "0.0.0-f1415e96"        (fallback)
 *
 * Leading `v` is stripped so the result is valid semver for package.json.
 */
export function normalizeGitVersion(raw) {
  if (!raw) return null;
  const stripped = raw.replace(/^v/, "");
  if (!/^\d/.test(stripped)) {
    // No reachable tag — `git describe` fell back to just the commit hash.
    return `0.0.0-${stripped}`;
  }
  return stripped;
}

function deriveVersion() {
  return normalizeGitVersion(sh("git describe --tags --always --dirty"));
}

function main() {
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

  // Step 3: assemble electron-builder args.
  const passthrough = process.argv.slice(2);
  const builderArgs = [];
  if (version) builderArgs.push(`-c.extraMetadata.version=${version}`);

  // Step 4: gracefully degrade for local dev builds. electron-builder.yml
  // sets `notarize: true` so real releases notarize in-build (keeping the
  // stapled .app consistent with latest-mac.yml's SHA512). But a mac dev
  // who just wants to smoke-test a local package doesn't have Apple
  // credentials, and would otherwise hit a hard failure at the notarize
  // step. Detect the missing env and flip notarize off for this run only.
  if (!process.env.APPLE_TEAM_ID) {
    console.warn(
      "[package] APPLE_TEAM_ID not set — skipping notarization (local dev build). " +
        "Set APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD + APPLE_TEAM_ID for a release build.",
    );
    builderArgs.push("-c.mac.notarize=false");
  }

  builderArgs.push(...passthrough);

  // Step 5: invoke electron-builder. pnpm puts node_modules/.bin on PATH
  // for the script run, so spawnSync finds the binary without needing a
  // shell wrapper (avoids any risk of argv interpolation).
  const result = spawnSync("electron-builder", builderArgs, {
    stdio: "inherit",
    cwd: desktopRoot,
  });

  if (result.error) {
    console.error(
      "[package] failed to spawn electron-builder:",
      result.error.message,
    );
    process.exit(1);
  }
  process.exit(result.status ?? 1);
}

// Only run when invoked as a CLI, not when imported by a test file.
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  main();
}
