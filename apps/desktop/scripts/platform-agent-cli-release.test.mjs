import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS,
  PLATFORM_AGENT_CLI_RELEASE_REPOSITORY,
  PLATFORM_AGENT_CLI_RELEASE_VERSION,
  downloadPlatformAgentRelease,
} from "./platform-agent-cli-release.mjs";

const tempDirs = [];

async function makeTempDir() {
  const dir = await mkdtemp(join(tmpdir(), "multica-platform-agent-download-"));
  tempDirs.push(dir);
  return dir;
}

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })),
  );
});

describe("pinned Platform Agent CLI release", () => {
  it("owns the authoritative repository, version, and complete six-target inventory", () => {
    expect(PLATFORM_AGENT_CLI_RELEASE_REPOSITORY).toBe(
      "multica-ai/platform-agent-cli",
    );
    expect(PLATFORM_AGENT_CLI_RELEASE_VERSION).toBe("0.2.0");
    expect(PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS).toEqual([
      "platform-agent-cli_0.2.0_darwin_amd64",
      "platform-agent-cli_0.2.0_darwin_arm64",
      "platform-agent-cli_0.2.0_linux_amd64",
      "platform-agent-cli_0.2.0_linux_arm64",
      "platform-agent-cli_0.2.0_windows_amd64.exe",
      "platform-agent-cli_0.2.0_windows_arm64.exe",
      "checksums.txt",
    ]);
  });

  it("downloads every pinned asset and exports strict staging inputs", async () => {
    const root = await makeTempDir();
    const artifactDir = join(root, "artifacts");
    const githubEnv = join(root, "github.env");
    const calls = [];
    const exec = async (command, args) => {
      calls.push([command, args]);
      for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
        if (artifact !== "checksums.txt") {
          await writeFile(join(artifactDir, artifact), artifact);
        }
      }
      await writeFile(
        join(artifactDir, "checksums.txt"),
        PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS.filter(
          (artifact) => artifact !== "checksums.txt",
        )
          .map((artifact) => `${"a".repeat(64)}  ${artifact}`)
          .join("\n") + "\n",
      );
    };

    await downloadPlatformAgentRelease({ artifactDir, githubEnv, exec });

    expect(calls).toHaveLength(1);
    expect(calls[0][0]).toBe("gh");
    expect(calls[0][1]).toContain("multica-ai/platform-agent-cli");
    expect(calls[0][1]).toContain("v0.2.0");
    for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
      expect(calls[0][1]).toContain(artifact);
    }
    expect(await readFile(githubEnv, "utf8")).toBe(
      `PLATFORM_AGENT_CLI_VERSION=0.2.0\nPLATFORM_AGENT_CLI_ARTIFACT_DIR=${artifactDir}\n`,
    );
  });

  it("rejects a download whose checksum manifest omits any target", async () => {
    const root = await makeTempDir();
    const artifactDir = join(root, "artifacts");
    const exec = async () => {
      for (const artifact of PLATFORM_AGENT_CLI_RELEASE_ARTIFACTS) {
        await writeFile(join(artifactDir, artifact), artifact);
      }
      await writeFile(
        join(artifactDir, "checksums.txt"),
        `${"a".repeat(64)}  platform-agent-cli_0.2.0_linux_amd64\n`,
      );
    };

    await expect(
      downloadPlatformAgentRelease({ artifactDir, exec }),
    ).rejects.toThrow(/checksum.*darwin_amd64/i);
  });

  it("rejects a relative destination before invoking GitHub", async () => {
    let invoked = false;
    await expect(
      downloadPlatformAgentRelease({
        artifactDir: "relative/artifacts",
        exec: async () => {
          invoked = true;
        },
      }),
    ).rejects.toThrow(/absolute/i);
    expect(invoked).toBe(false);
  });
});
