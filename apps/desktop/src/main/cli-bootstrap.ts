import { app } from "electron";
import { execFile } from "child_process";
import { createHash } from "crypto";
import { createReadStream, createWriteStream, existsSync } from "fs";
import { chmod, mkdir, rename, rm } from "fs/promises";
import { join, dirname } from "path";
import { pipeline } from "stream/promises";
import { tmpdir } from "os";
import { Readable } from "stream";

// Desktop bootstraps its own copy of the `multica` CLI into userData on first
// launch, so users never have to brew-install anything. Build-time decoupled:
// we don't bundle the binary into the .app, we download whatever the upstream
// release is at first run.

const GITHUB_LATEST_BASE =
  "https://github.com/multica-ai/multica/releases/latest/download";

function platformAssetName(): string {
  const osMap: Record<string, string> = {
    darwin: "darwin",
    linux: "linux",
    win32: "windows",
  };
  const archMap: Record<string, string> = {
    x64: "amd64",
    arm64: "arm64",
  };
  const os = osMap[process.platform];
  const arch = archMap[process.arch];
  if (!os || !arch) {
    throw new Error(
      `unsupported platform for CLI auto-install: ${process.platform}/${process.arch}`,
    );
  }
  const ext = process.platform === "win32" ? "zip" : "tar.gz";
  return `multica_${os}_${arch}.${ext}`;
}

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
): Promise<void> {
  const checksums = await fetchChecksums();
  const expected = checksums.get(assetName);
  if (!expected) {
    throw new Error(
      `no checksum for ${assetName} in checksums.txt — refusing to install unverified binary`,
    );
  }
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
  const assetName = platformAssetName();
  const url = `${GITHUB_LATEST_BASE}/${assetName}`;

  const workDir = join(tmpdir(), `multica-cli-${Date.now()}`);
  await mkdir(workDir, { recursive: true });

  try {
    const archivePath = join(workDir, assetName);
    console.log(`[cli-bootstrap] downloading ${url}`);
    await downloadToFile(url, archivePath);

    console.log(`[cli-bootstrap] verifying ${assetName} against checksums.txt`);
    await verifyChecksum(archivePath, assetName);

    console.log(`[cli-bootstrap] extracting ${assetName}`);
    await extractArchive(archivePath, workDir);

    const extractedBin = join(workDir, binaryName());
    if (!existsSync(extractedBin)) {
      throw new Error(
        `archive ${assetName} did not contain ${binaryName()} at its root`,
      );
    }

    await mkdir(dirname(target), { recursive: true });
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
export async function ensureManagedCli(): Promise<string> {
  const target = managedCliPath();
  if (existsSync(target)) return target;
  return installFresh();
}
