#!/usr/bin/env node

import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, rm, chmod } from "node:fs/promises";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const defaultDestDir = resolve(here, "..", "resources", "bin");

const PLATFORM_TO_ARTIFACT = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const ARCH_TO_ARTIFACT = {
  x64: "amd64",
  arm64: "arm64",
};

function normalizeTargetPlatform(targetPlatform) {
  if (targetPlatform in PLATFORM_TO_ARTIFACT) return targetPlatform;
  throw new Error(
    `[bundle-platform-agent-cli] unsupported target platform: ${targetPlatform}. ` +
      "Use darwin, linux, or win32.",
  );
}

function normalizeTargetArch(targetArch) {
  if (targetArch in ARCH_TO_ARTIFACT) return targetArch;
  throw new Error(
    `[bundle-platform-agent-cli] unsupported target architecture: ${targetArch}. ` +
      "Use x64 or arm64.",
  );
}

export function platformAgentArtifactName(version, targetPlatform, targetArch) {
  const platform = normalizeTargetPlatform(targetPlatform);
  const arch = normalizeTargetArch(targetArch);
  const suffix = platform === "win32" ? ".exe" : "";
  return (
    `platform-agent-cli_${version}_${PLATFORM_TO_ARTIFACT[platform]}_` +
    `${ARCH_TO_ARTIFACT[arch]}${suffix}`
  );
}

export function platformAgentBinaryName(targetPlatform) {
  const platform = normalizeTargetPlatform(targetPlatform);
  return platform === "win32" ? "platform-agent-cli.exe" : "platform-agent-cli";
}

export function parseChecksumManifest(raw) {
  const checksums = new Map();
  for (const line of raw.split(/\r?\n/)) {
    if (line === "") continue;
    const match = line.match(/^([0-9a-f]{64}) {2}(.+)$/);
    if (!match) {
      throw new Error(
        `[bundle-platform-agent-cli] malformed checksum line: ${JSON.stringify(line)}`,
      );
    }
    checksums.set(match[2], match[1]);
  }
  return checksums;
}

async function removeStagedPlatformAgent(destDir) {
  await Promise.all([
    rm(join(destDir, "platform-agent-cli"), { force: true }),
    rm(join(destDir, "platform-agent-cli.exe"), { force: true }),
  ]);
}

export async function stageReleasePlatformAgent({
  version = process.env.PLATFORM_AGENT_CLI_VERSION,
  artifactDir = process.env.PLATFORM_AGENT_CLI_ARTIFACT_DIR,
  targetPlatform = process.platform,
  targetArch = process.arch,
  destDir = defaultDestDir,
} = {}) {
  if (typeof version !== "string" || version.trim() === "") {
    throw new Error(
      "[bundle-platform-agent-cli] PLATFORM_AGENT_CLI_VERSION must be non-empty in release mode",
    );
  }
  if (typeof artifactDir !== "string" || !isAbsolute(artifactDir)) {
    throw new Error(
      "[bundle-platform-agent-cli] PLATFORM_AGENT_CLI_ARTIFACT_DIR must be an absolute path in release mode",
    );
  }

  const artifactName = platformAgentArtifactName(
    version,
    targetPlatform,
    targetArch,
  );
  const artifactPath = join(artifactDir, artifactName);
  const manifest = parseChecksumManifest(
    await readFile(join(artifactDir, "checksums.txt"), "utf8"),
  );
  const expectedChecksum = manifest.get(artifactName);
  if (!expectedChecksum) {
    throw new Error(
      `[bundle-platform-agent-cli] checksum missing for selected artifact: ${artifactName}`,
    );
  }

  const artifact = await readFile(artifactPath);
  const actualChecksum = createHash("sha256").update(artifact).digest("hex");
  if (actualChecksum !== expectedChecksum) {
    throw new Error(
      `[bundle-platform-agent-cli] checksum mismatch for ${artifactName}: ` +
        `expected ${expectedChecksum}, got ${actualChecksum}`,
    );
  }

  await mkdir(destDir, { recursive: true });
  await removeStagedPlatformAgent(destDir);
  const destBinary = join(destDir, platformAgentBinaryName(targetPlatform));
  await copyFile(artifactPath, destBinary);
  if (targetPlatform !== "win32") await chmod(destBinary, 0o755);
  console.log(
    `[bundle-platform-agent-cli] bundled ${artifactPath} → ${destBinary}`,
  );
}

export async function stageDevPlatformAgent({
  devBinary = process.env.PLATFORM_AGENT_CLI_DEV_BINARY,
  targetPlatform = process.platform,
  destDir = defaultDestDir,
} = {}) {
  const binaryName = platformAgentBinaryName(targetPlatform);
  await mkdir(destDir, { recursive: true });
  await removeStagedPlatformAgent(destDir);
  if (!devBinary) {
    console.warn(
      "[bundle-platform-agent-cli] PLATFORM_AGENT_CLI_DEV_BINARY not set — " +
        "continuing without a bundled Platform Agent CLI",
    );
    return;
  }

  const destBinary = join(destDir, binaryName);
  await copyFile(devBinary, destBinary);
  if (targetPlatform !== "win32") await chmod(destBinary, 0o755);
  console.log(`[bundle-platform-agent-cli] bundled ${devBinary} → ${destBinary}`);
}

function valueForFlag(argv, flag, fallback) {
  const index = argv.indexOf(flag);
  return index === -1 ? fallback : (argv[index + 1] ?? "");
}

async function main() {
  const argv = process.argv.slice(2);
  const targetPlatform = valueForFlag(
    argv,
    "--target-platform",
    process.platform,
  );
  const targetArch = valueForFlag(argv, "--target-arch", process.arch);
  if (argv.includes("--release")) {
    await stageReleasePlatformAgent({ targetPlatform, targetArch });
    return;
  }
  await stageDevPlatformAgent({ targetPlatform });
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await main();
}
