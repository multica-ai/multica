import { app } from "electron";
import { execFile } from "child_process";
import { createHash } from "crypto";
import {
  createReadStream,
  createWriteStream,
  existsSync,
  statSync,
} from "fs";
import { chmod, copyFile, mkdir, rename, rm } from "fs/promises";
import { join, dirname } from "path";
import { pipeline } from "stream/promises";
import { tmpdir } from "os";
import { Readable } from "stream";

import { selectPlatformReleaseAssetName } from "./cli-release-asset";

// Desktop prefers the bundled `multica` CLI shipped inside the app for
// same-repo builds, but it can also repair or bootstrap a managed copy in
// userData on first launch when the bundled binary is missing or unusable.

const GITHUB_LATEST_BASE =
  "https://github.com/multica-ai/multica/releases/latest/download";

function binaryName(): string {
  return process.platform === "win32" ? "multica.exe" : "multica";
}

export function managedCliPath(): string {
  return join(app.getPath("userData"), "bin", binaryName());
}

function run(cmd: string, args: string[], cwd?: string): Promise<void> {
  return new Promise((resolve, reject) => {
    execFile(cmd, args, { cwd }, (err) => (err ? reject(err) : resolve()));
  });
}

async function downloadToFile(url: string, dest: string): Promise<void> {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok || !res.body) {
    throw new Error(`download failed: ${res.status} ${res.statusText}`);
  }
  await mkdir(dirname(dest), { recursive: true });
  // Node's fetch returns a web ReadableStream; adapt to a Node stream for pipeline.
  const nodeStream = Readable.fromWeb(res.body as Parameters<typeof Readable.fromWeb>[0]);
  await pipeline(nodeStream, createWriteStream(dest));
}

// Fetch goreleaser's published checksums.txt and parse it into a
// filename → sha256 lookup. Format is `<hex>  <filename>` per line.
async function fetchChecksums(): Promise<Map<string, string>> {
  const url = `${GITHUB_LATEST_BASE}/checksums.txt`;
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(
      `checksums.txt fetch failed: ${res.status} ${res.statusText}`,
    );
  }
  const text = await res.text();
  const map = new Map<string, string>();
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const match = line.match(/^([a-f0-9]{64})\s+\*?(\S+)$/i);
    if (match) map.set(match[2], match[1].toLowerCase());
  }
  return map;
}

async function sha256OfFile(path: string): Promise<string> {
  const hash = createHash("sha256");
  await pipeline(createReadStream(path), hash);
  return hash.digest("hex");
}

async function verifyChecksum(
  archivePath: string,
  assetName: string,
  expected: string,
): Promise<void> {
  const actual = await sha256OfFile(archivePath);
  if (actual.toLowerCase() !== expected) {
    throw new Error(
      `checksum mismatch for ${assetName}: expected ${expected}, got ${actual}`,
    );
  }
}

async function extractArchive(archive: string, dest: string): Promise<void> {
  await mkdir(dest, { recursive: true });
  // Modern OSes all ship a `tar` that auto-detects tar.gz and zip:
  // - macOS/Linux: GNU tar or bsdtar
  // - Windows 10+: bsdtar is bundled as `tar.exe` since build 17063
  await run("tar", ["-xf", archive, "-C", dest]);
}

async function installFresh(): Promise<string> {
  const target = managedCliPath();
  const checksums = await fetchChecksums();
  const assetName = selectPlatformReleaseAssetName(checksums.keys());
  const expectedChecksum = checksums.get(assetName);
  if (!expectedChecksum) {
    throw new Error(
      `no checksum for ${assetName} in checksums.txt — refusing to install unverified binary`,
    );
  }
  const url = `${GITHUB_LATEST_BASE}/${assetName}`;

  const workDir = join(tmpdir(), `multica-cli-${Date.now()}`);
  await mkdir(workDir, { recursive: true });

  try {
    const archivePath = join(workDir, assetName);
    console.log(`[cli-bootstrap] downloading ${url}`);
    await downloadToFile(url, archivePath);

    console.log(`[cli-bootstrap] verifying ${assetName} against checksums.txt`);
    await verifyChecksum(archivePath, assetName, expectedChecksum);

    console.log(`[cli-bootstrap] extracting ${assetName}`);
    await extractArchive(archivePath, workDir);

    const extractedBin = join(workDir, binaryName());
    if (!existsSync(extractedBin)) {
      throw new Error(
        `archive ${assetName} did not contain ${binaryName()} at its root`,
      );
    }

    await mkdir(dirname(target), { recursive: true });
    await rm(target, { force: true }).catch(() => {});
    await rename(extractedBin, target);
    await chmod(target, 0o755);

    // macOS: ad-hoc sign so spawning the child never hits a gatekeeper quirk.
    // Non-fatal: unsigned binaries still execute when the parent app is trusted.
    if (process.platform === "darwin") {
      await run("codesign", ["-s", "-", "--force", target]).catch((err) => {
        console.warn("[cli-bootstrap] ad-hoc codesign failed:", err);
      });
    }

    console.log(`[cli-bootstrap] installed CLI at ${target}`);
    return target;
  } finally {
    await rm(workDir, { recursive: true, force: true }).catch(() => {});
  }
}

/**
 * Returns the path to a usable `multica` binary. If one is already present at
 * the managed userData location, returns it immediately. Otherwise downloads
 * the latest release asset for the current platform and installs it.
 */
export async function ensureManagedCli(
  options: { forceInstall?: boolean } = {},
): Promise<string> {
  const target = managedCliPath();
  if (existsSync(target) && !options.forceInstall) return target;
  return installFresh();
}

/**
 * Returns a CLI path the agent subprocess can reliably read and execute.
 *
 * On Windows, the bundled CLI lives under
 * `%LOCALAPPDATA%\Programs\<install dir>\resources\app.asar.unpacked\resources\bin\multica.exe`.
 * Agent subprocesses spawned by the daemon have been observed to fail listing
 * or invoking that directory (#2672 / MUL-2285) — `multica` simply doesn't
 * resolve, every `multica issue …` call dies, and the agent loops on errors.
 *
 * Mirror the bundled binary into the user-writable Electron `userData/bin/`
 * (same path `managedCliPath()` returns) so the daemon can prepend that dir
 * to the agent's PATH instead of the unpacked resources path. The mirror is
 * keyed by size: a re-copy fires when the bundled binary's size differs from
 * the managed copy (typically because Desktop auto-updated to a new build).
 *
 * On macOS/Linux the bundled path is reachable, so we just return it.
 *
 * Returns `null` only when neither bundled nor managed is usable; the daemon
 * will then fall back to its own executable directory.
 */
export async function ensureAgentAccessibleCli(
  bundledPath: string,
): Promise<string | null> {
  if (process.platform !== "win32") {
    return existsSync(bundledPath) ? bundledPath : null;
  }

  const managed = managedCliPath();
  const bundledExists = existsSync(bundledPath);

  if (bundledExists) {
    let needsCopy = !existsSync(managed);
    if (!needsCopy) {
      try {
        const a = statSync(bundledPath);
        const b = statSync(managed);
        needsCopy = a.size !== b.size;
      } catch {
        needsCopy = true;
      }
    }
    if (needsCopy) {
      try {
        await mkdir(dirname(managed), { recursive: true });
        // Atomic-ish replace: copy → chmod → rename. Windows can't rename
        // over a running executable, but the agent never holds the managed
        // copy open at this point — daemon is still starting up.
        const tmp = `${managed}.tmp-${process.pid}-${Date.now()}`;
        await copyFile(bundledPath, tmp);
        await chmod(tmp, 0o755);
        await rm(managed, { force: true }).catch(() => {});
        await rename(tmp, managed);
        console.log(
          `[cli-bootstrap] mirrored bundled CLI → ${managed} for agent PATH`,
        );
      } catch (err) {
        console.warn(
          "[cli-bootstrap] mirror to managed location failed:",
          err,
        );
        return existsSync(managed) ? managed : bundledPath;
      }
    }
    return managed;
  }

  return existsSync(managed) ? managed : null;
}
