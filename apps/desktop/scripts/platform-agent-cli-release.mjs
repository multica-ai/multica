#!/usr/bin/env node

import { execFile } from "node:child_process";
import { appendFile, lstat, mkdir, readFile } from "node:fs/promises";
import { isAbsolute, join } from "node:path";
import { promisify } from "node:util";
import { pathToFileURL } from "node:url";

import { parseChecksumManifest } from "./bundle-platform-agent-cli.mjs";

const execFileAsync = promisify(execFile);

export const PLATFORM_AGENT_CLI_RELEASE_REPOSITORY =
  "multica-ai/platform-agent-cli";
export const PLATFORM_AGENT_CLI_RELEASE_VERSION = "0.2.0";
export const PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS = Object.freeze([
  "platform-agent-cli_0.2.0_darwin_amd64",
  "platform-agent-cli_0.2.0_darwin_arm64",
  "platform-agent-cli_0.2.0_linux_amd64",
  "platform-agent-cli_0.2.0_linux_arm64",
  "platform-agent-cli_0.2.0_windows_amd64.exe",
  "platform-agent-cli_0.2.0_windows_arm64.exe",
  "checksums.txt",
]);

async function runGitHubCLI(command, args) {
  await execFileAsync(command, args, { maxBuffer: 1024 * 1024 });
}

async function requireCompleteArtifactSet(artifactDir) {
  for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
    const path = join(artifactDir, artifact);
    let info;
    try {
      info = await lstat(path);
    } catch (error) {
      throw new Error(
        `[platform-agent-cli-release] missing downloaded artifact ${artifact}: ${error.message}`,
      );
    }
    if (!info.isFile()) {
      throw new Error(
        `[platform-agent-cli-release] downloaded artifact must be a regular file: ${artifact}`,
      );
    }
  }
  const checksums = parseChecksumManifest(
    await readFile(join(artifactDir, "checksums.txt"), "utf8"),
  );
  for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
    if (artifact !== "checksums.txt" && !checksums.has(artifact)) {
      throw new Error(
        `[platform-agent-cli-release] checksum manifest is missing ${artifact}`,
      );
    }
  }
}

export async function downloadPlatformAgentRelease({
  artifactDir,
  githubEnv = process.env.GITHUB_ENV,
  exec = runGitHubCLI,
} = {}) {
  if (typeof artifactDir !== "string" || !isAbsolute(artifactDir)) {
    throw new Error(
      "[platform-agent-cli-release] artifact destination must be an absolute path",
    );
  }
  await mkdir(artifactDir, { recursive: true });

  const args = [
    "release",
    "download",
    `v${PLATFORM_AGENT_CLI_RELEASE_VERSION}`,
    "--repo",
    PLATFORM_AGENT_CLI_RELEASE_REPOSITORY,
    "--dir",
    artifactDir,
    "--clobber",
  ];
  for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
    args.push("--pattern", artifact);
  }
  await exec("gh", args);
  await requireCompleteArtifactSet(artifactDir);

  if (githubEnv) {
    await appendFile(
      githubEnv,
      `PLATFORM_AGENT_CLI_VERSION=${PLATFORM_AGENT_CLI_RELEASE_VERSION}\n` +
        `PLATFORM_AGENT_CLI_ARTIFACT_DIR=${artifactDir}\n`,
      "utf8",
    );
  }
}

function valueForFlag(argv, flag) {
  const index = argv.indexOf(flag);
  return index === -1 ? "" : (argv[index + 1] ?? "");
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await downloadPlatformAgentRelease({
    artifactDir: valueForFlag(process.argv.slice(2), "--artifact-dir"),
  });
}
