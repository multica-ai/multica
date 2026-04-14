#!/usr/bin/env node
// Copies the locally-built `multica` CLI into apps/desktop/resources/bin/
// so electron-vite (dev) and electron-builder (prod) pick it up. Desktop's
// cli-bootstrap prefers this bundled copy over the GitHub Releases download
// path, which keeps dev iteration fast (edit Go → make build → restart
// Desktop) and lets the released .app ship with a CLI out of the box.
//
// Graceful: if server/bin/multica is missing, prints a warning and exits
// with status 0 so the Desktop can still boot with auto-install fallback.

import { access, chmod, copyFile, mkdir } from "node:fs/promises";
import { constants } from "node:fs";
import { execSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");

const binName = process.platform === "win32" ? "multica.exe" : "multica";
const srcBinary = join(repoRoot, "server", "bin", binName);
const destDir = join(repoRoot, "apps", "desktop", "resources", "bin");
const destBinary = join(destDir, binName);

async function exists(p) {
  try {
    await access(p, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

if (!(await exists(srcBinary))) {
  console.warn(
    `[bundle-cli] ${srcBinary} not found — run 'make build' to bundle the CLI. ` +
      `Desktop will fall back to auto-installing the latest release at runtime.`,
  );
  process.exit(0);
}

await mkdir(destDir, { recursive: true });
await copyFile(srcBinary, destBinary);
await chmod(destBinary, 0o755);

// macOS: ad-hoc sign so Gatekeeper doesn't complain when the parent app
// (which itself may be unsigned in dev) spawns the child.
if (process.platform === "darwin") {
  try {
    execSync(`codesign -s - --force ${JSON.stringify(destBinary)}`, {
      stdio: "pipe",
    });
  } catch {
    // Non-fatal. Unsigned binaries still run when the parent is trusted.
  }
}

console.log(`[bundle-cli] bundled ${srcBinary} → ${destBinary}`);
