#!/usr/bin/env node
// Builds the `multica` CLI from server/cmd/multica and copies the binary
// into apps/desktop/resources/bin/ so electron-vite (dev) and electron-
// builder (prod) pick it up. Running this on every dev/build/package
// invocation guarantees the bundled CLI always matches the current Go
// source — no more stale binary surprises. Go's build cache makes the
// no-op case (nothing changed) effectively free.
//
// ldflags mirror `make build` so `multica --version` reports a meaningful
// version / commit / date.
//
// Graceful: if `go` is not installed (e.g. frontend-only contributor), we
// skip the build and fall through to auto-install at runtime. A genuine
// Go compile error is fatal — you want that to block dev, not hide.

import { access, chmod, copyFile, mkdir } from "node:fs/promises";
import { constants } from "node:fs";
import { execFileSync, execSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const serverDir = join(repoRoot, "server");

const binName = process.platform === "win32" ? "multica.exe" : "multica";
const srcBinary = join(serverDir, "bin", binName);
const destDir = join(repoRoot, "apps", "desktop", "resources", "bin");
const destBinary = join(destDir, binName);

function sh(cmd) {
  try {
    return execSync(cmd, { encoding: "utf-8" }).trim();
  } catch {
    return "";
  }
}

function hasGo() {
  try {
    execSync("go version", { stdio: "pipe" });
    return true;
  } catch {
    return false;
  }
}

async function exists(p) {
  try {
    await access(p, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

if (hasGo()) {
  const version = sh("git describe --tags --always --dirty") || "dev";
  const commit = sh("git rev-parse --short HEAD") || "unknown";
  const date = new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const ldflags = `-X main.version=${version} -X main.commit=${commit} -X main.date=${date}`;

  console.log(
    `[bundle-cli] go build → ${srcBinary} (version=${version} commit=${commit})`,
  );
  execFileSync(
    "go",
    [
      "build",
      "-ldflags",
      ldflags,
      "-o",
      join("bin", binName),
      "./cmd/multica",
    ],
    { cwd: serverDir, stdio: "inherit" },
  );
} else {
  console.warn(
    "[bundle-cli] `go` not found in PATH — skipping CLI build. " +
      "Desktop will use whatever is already in resources/bin/, or fall back " +
      "to auto-installing the latest release at runtime.",
  );
}

if (!(await exists(srcBinary))) {
  console.warn(
    `[bundle-cli] ${srcBinary} not present — Desktop will fall back to ` +
      `auto-installing the latest release at runtime.`,
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
    // Non-fatal. Unsigned binaries still run when the parent app is trusted.
  }
}

console.log(`[bundle-cli] bundled ${srcBinary} → ${destBinary}`);
