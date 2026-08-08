#!/usr/bin/env node

import { createHash, randomUUID } from "node:crypto";
import { execFile } from "node:child_process";
import { constants as fsConstants } from "node:fs";
import { chmod, copyFile, lstat, mkdir, readFile, rename, rm } from "node:fs/promises";
import { basename, dirname, isAbsolute, join, resolve } from "node:path";
import { promisify } from "node:util";
import { fileURLToPath, pathToFileURL } from "node:url";

const execFileAsync = promisify(execFile);

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
  if (Object.hasOwn(PLATFORM_TO_ARTIFACT, targetPlatform)) return targetPlatform;
  throw new Error(
    `[bundle-platform-agent-cli] unsupported target platform: ${targetPlatform}. ` +
      "Use darwin, linux, or win32.",
  );
}

function normalizeTargetArch(targetArch) {
  if (Object.hasOwn(ARCH_TO_ARTIFACT, targetArch)) return targetArch;
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

export function parseGoBuildMetadata(raw) {
  const goos = raw.match(/\bGOOS=([a-z0-9]+)\b/)?.[1];
  const goarch = raw.match(/\bGOARCH=([a-z0-9]+)\b/)?.[1];
  if (!goos || !goarch) {
    throw new Error(
      "[bundle-platform-agent-cli] Go build metadata must include GOOS and GOARCH",
    );
  }
  return { goos, goarch };
}

export function parsePlatformAgentVersionMarker(binary) {
  const matches = binary
    .toString("latin1")
    .matchAll(/platform-agent-cli-release-version:([0-9]+\.[0-9]+\.[0-9]+(?:[.+-][0-9A-Za-z.-]+)?)/g);
  const versions = new Set(Array.from(matches, (match) => match[1]));
  if (versions.size !== 1) {
    throw new Error(
      "[bundle-platform-agent-cli] binary must contain exactly one release version marker",
    );
  }
  return versions.values().next().value;
}

export async function inspectPlatformAgentBinary(
  artifactPath,
  goBinary = process.env.GO_BINARY || "go",
) {
  try {
    const { stdout } = await execFileAsync(
      goBinary,
      ["version", "-m", artifactPath],
      { maxBuffer: 1024 * 1024 },
    );
    return {
      ...parseGoBuildMetadata(stdout),
      version: parsePlatformAgentVersionMarker(await readFile(artifactPath)),
    };
  } catch (error) {
    throw new Error(
      `[bundle-platform-agent-cli] cannot inspect Go binary metadata for ${artifactPath}: ${error.message}`,
    );
  }
}

async function removeStagedPlatformAgent(destDir) {
  await Promise.all([
    rm(join(destDir, "platform-agent-cli"), { force: true }),
    rm(join(destDir, "platform-agent-cli.exe"), { force: true }),
  ]);
}

async function atomicCopy(source, destination, mode) {
  const tempPath = join(
    dirname(destination),
    `.${basename(destination)}.${process.pid}.${randomUUID()}.tmp`,
  );
  try {
    await copyFile(source, tempPath, fsConstants.COPYFILE_EXCL);
    if (mode !== undefined) await chmod(tempPath, mode);
    await rename(tempPath, destination);
  } finally {
    await rm(tempPath, { force: true });
  }
}

async function requireRegularFile(path, label) {
  let info;
  try {
    info = await lstat(path);
  } catch (error) {
    throw new Error(`[bundle-platform-agent-cli] cannot stat ${label}: ${error.message}`);
  }
  if (!info.isFile()) {
    throw new Error(`[bundle-platform-agent-cli] ${label} must be a regular file: ${path}`);
  }
  return info;
}

export async function stageReleasePlatformAgent({
  version = process.env.PLATFORM_AGENT_CLI_VERSION,
  artifactDir = process.env.PLATFORM_AGENT_CLI_ARTIFACT_DIR,
  targetPlatform = process.platform,
  targetArch = process.arch,
  destDir = defaultDestDir,
  inspectBinary = inspectPlatformAgentBinary,
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
  await requireRegularFile(artifactPath, "selected release artifact");
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

  const metadata = await inspectBinary(artifactPath);
  const expectedPlatform = PLATFORM_TO_ARTIFACT[normalizeTargetPlatform(targetPlatform)];
  const expectedArch = ARCH_TO_ARTIFACT[normalizeTargetArch(targetArch)];
  if (metadata.goos !== expectedPlatform) {
    throw new Error(
      `[bundle-platform-agent-cli] binary target platform mismatch: expected ${expectedPlatform}, got ${metadata.goos}`,
    );
  }
  if (metadata.goarch !== expectedArch) {
    throw new Error(
      `[bundle-platform-agent-cli] binary target architecture mismatch: expected ${expectedArch}, got ${metadata.goarch}`,
    );
  }
  if (metadata.version !== version) {
    throw new Error(
      `[bundle-platform-agent-cli] binary version mismatch: expected ${version}, got ${metadata.version}`,
    );
  }

  await mkdir(destDir, { recursive: true });
  const destBinary = join(destDir, platformAgentBinaryName(targetPlatform));
  await atomicCopy(
    artifactPath,
    destBinary,
    targetPlatform === "win32" ? undefined : 0o755,
  );
  const staleBinary = join(
    destDir,
    targetPlatform === "win32" ? "platform-agent-cli" : "platform-agent-cli.exe",
  );
  await rm(staleBinary, { force: true });
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
  if (!devBinary) {
    await removeStagedPlatformAgent(destDir);
    console.warn(
      "[bundle-platform-agent-cli] PLATFORM_AGENT_CLI_DEV_BINARY not set — " +
        "continuing without a bundled Platform Agent CLI",
    );
    return;
  }

  if (!isAbsolute(devBinary)) {
    throw new Error(
      "[bundle-platform-agent-cli] PLATFORM_AGENT_CLI_DEV_BINARY must be an absolute path",
    );
  }
  const info = await requireRegularFile(devBinary, "development binary");
  if (targetPlatform !== "win32" && (info.mode & 0o111) === 0) {
    throw new Error(
      `[bundle-platform-agent-cli] development binary must be executable on ${targetPlatform}: ${devBinary}`,
    );
  }

  const destBinary = join(destDir, binaryName);
  await atomicCopy(
    devBinary,
    destBinary,
    targetPlatform === "win32" ? undefined : 0o755,
  );
  const staleBinary = join(
    destDir,
    targetPlatform === "win32" ? "platform-agent-cli" : "platform-agent-cli.exe",
  );
  await rm(staleBinary, { force: true });
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
