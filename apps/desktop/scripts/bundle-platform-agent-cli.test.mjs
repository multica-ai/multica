import { createHash } from "node:crypto";
import {
  chmod,
  mkdtemp,
  mkdir,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  parseChecksumManifest,
  platformAgentArtifactName,
  platformAgentBinaryName,
  stageDevPlatformAgent,
  stageReleasePlatformAgent,
} from "./bundle-platform-agent-cli.mjs";

const tempDirs = [];

async function makeTempDir() {
  const dir = await mkdtemp(join(tmpdir(), "multica-platform-agent-"));
  tempDirs.push(dir);
  return dir;
}

function sha256(contents) {
  return createHash("sha256").update(contents).digest("hex");
}

afterEach(async () => {
  await Promise.all(
    tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })),
  );
});

describe("platform-agent artifact contract", () => {
  it.each([
    ["darwin", "x64", "platform-agent-cli_0.1.0_darwin_amd64"],
    ["darwin", "arm64", "platform-agent-cli_0.1.0_darwin_arm64"],
    ["linux", "x64", "platform-agent-cli_0.1.0_linux_amd64"],
    ["linux", "arm64", "platform-agent-cli_0.1.0_linux_arm64"],
    ["win32", "x64", "platform-agent-cli_0.1.0_windows_amd64.exe"],
    ["win32", "arm64", "platform-agent-cli_0.1.0_windows_arm64.exe"],
  ])("maps Electron %s/%s to %s", (platform, arch, artifact) => {
    expect(platformAgentArtifactName("0.1.0", platform, arch)).toBe(artifact);
  });

  it("uses the canonical bundled executable name for each platform", () => {
    expect(platformAgentBinaryName("darwin")).toBe("platform-agent-cli");
    expect(platformAgentBinaryName("linux")).toBe("platform-agent-cli");
    expect(platformAgentBinaryName("win32")).toBe("platform-agent-cli.exe");
  });

  it("rejects unsupported Electron targets", () => {
    expect(() => platformAgentArtifactName("0.1.0", "freebsd", "x64")).toThrow(
      /unsupported target platform/i,
    );
    expect(() => platformAgentArtifactName("0.1.0", "linux", "ia32")).toThrow(
      /unsupported target architecture/i,
    );
  });
});

describe("parseChecksumManifest", () => {
  it("parses standard lowercase sha256sum lines", () => {
    const first = "a".repeat(64);
    const second = "0".repeat(64);
    expect(
      parseChecksumManifest(`${first}  first-file\n${second}  second.exe\n`),
    ).toEqual(
      new Map([
        ["first-file", first],
        ["second.exe", second],
      ]),
    );
  });

  it.each([
    ["uppercase digest", `${"A".repeat(64)}  artifact`],
    ["single separator space", `${"a".repeat(64)} artifact`],
    ["short digest", `${"a".repeat(63)}  artifact`],
    ["non-checksum text", "not a checksum"],
  ])("rejects a malformed manifest with %s", (_label, raw) => {
    expect(() => parseChecksumManifest(raw)).toThrow(/malformed checksum/i);
  });
});

describe("stageReleasePlatformAgent", () => {
  async function setupRelease({
    platform = "linux",
    arch = "x64",
    contents = "selected platform agent",
    checksum = sha256(contents),
    writeSelectedArtifact = true,
    manifestName,
  } = {}) {
    const root = await makeTempDir();
    const artifactDir = join(root, "artifacts");
    const destDir = join(root, "resources", "bin");
    const artifactName = platformAgentArtifactName("0.1.0", platform, arch);
    await mkdir(artifactDir, { recursive: true });
    await mkdir(destDir, { recursive: true });
    if (writeSelectedArtifact) {
      await writeFile(join(artifactDir, artifactName), contents);
    }
    await writeFile(
      join(artifactDir, "checksums.txt"),
      `${checksum}  ${manifestName ?? artifactName}\n`,
    );
    return { artifactDir, artifactName, contents, destDir };
  }

  it("copies only the selected verified artifact and preserves the Multica sibling", async () => {
    const fixture = await setupRelease();
    await writeFile(join(fixture.artifactDir, "unselected-artifact"), "other");
    await writeFile(join(fixture.destDir, "platform-agent-cli"), "stale unix");
    await writeFile(join(fixture.destDir, "platform-agent-cli.exe"), "stale windows");
    await writeFile(join(fixture.destDir, "multica"), "multica sibling");
    await writeFile(join(fixture.destDir, "multica.exe"), "multica windows sibling");

    await stageReleasePlatformAgent({
      version: "0.1.0",
      artifactDir: fixture.artifactDir,
      targetPlatform: "linux",
      targetArch: "x64",
      destDir: fixture.destDir,
    });

    expect(await readFile(join(fixture.destDir, "platform-agent-cli"), "utf8")).toBe(
      fixture.contents,
    );
    expect(await stat(join(fixture.destDir, "platform-agent-cli"))).toMatchObject({
      mode: expect.any(Number),
    });
    expect((await stat(join(fixture.destDir, "platform-agent-cli"))).mode & 0o777).toBe(
      0o755,
    );
    expect((await readdir(fixture.destDir)).sort()).toEqual([
      "multica",
      "multica.exe",
      "platform-agent-cli",
    ]);
  });

  it("fails when the selected artifact is missing", async () => {
    const fixture = await setupRelease({ writeSelectedArtifact: false });
    await expect(
      stageReleasePlatformAgent({
        version: "0.1.0",
        artifactDir: fixture.artifactDir,
        targetPlatform: "linux",
        targetArch: "x64",
        destDir: fixture.destDir,
      }),
    ).rejects.toThrow();
  });

  it("fails when checksums.txt has no entry for the selected artifact", async () => {
    const fixture = await setupRelease({ manifestName: "some-other-artifact" });
    await expect(
      stageReleasePlatformAgent({
        version: "0.1.0",
        artifactDir: fixture.artifactDir,
        targetPlatform: "linux",
        targetArch: "x64",
        destDir: fixture.destDir,
      }),
    ).rejects.toThrow(/checksum.*selected artifact/i);
  });

  it("fails when checksums.txt is malformed", async () => {
    const fixture = await setupRelease();
    await writeFile(join(fixture.artifactDir, "checksums.txt"), "malformed\n");
    await expect(
      stageReleasePlatformAgent({
        version: "0.1.0",
        artifactDir: fixture.artifactDir,
        targetPlatform: "linux",
        targetArch: "x64",
        destDir: fixture.destDir,
      }),
    ).rejects.toThrow(/malformed checksum/i);
  });

  it("fails when the selected artifact does not match its checksum", async () => {
    const fixture = await setupRelease({ checksum: "0".repeat(64) });
    await expect(
      stageReleasePlatformAgent({
        version: "0.1.0",
        artifactDir: fixture.artifactDir,
        targetPlatform: "linux",
        targetArch: "x64",
        destDir: fixture.destDir,
      }),
    ).rejects.toThrow(/checksum mismatch/i);
  });

  it.each([
    ["empty version", { version: "", artifactDir: "/artifacts" }, /version/i],
    ["relative artifact directory", { version: "0.1.0", artifactDir: "artifacts" }, /absolute/i],
  ])("fails closed for an %s", async (_label, overrides, expected) => {
    const destDir = await makeTempDir();
    await expect(
      stageReleasePlatformAgent({
        version: "0.1.0",
        artifactDir: "/artifacts",
        targetPlatform: "linux",
        targetArch: "x64",
        destDir,
        ...overrides,
      }),
    ).rejects.toThrow(expected);
  });
});

describe("stageDevPlatformAgent", () => {
  it("copies the configured host binary to the canonical name", async () => {
    const root = await makeTempDir();
    const devBinary = join(root, "external", "agent-dev");
    const destDir = join(root, "resources", "bin");
    await mkdir(join(root, "external"), { recursive: true });
    await writeFile(devBinary, "development platform agent");
    await chmod(devBinary, 0o755);

    await stageDevPlatformAgent({ devBinary, targetPlatform: "linux", destDir });

    expect(await readFile(join(destDir, "platform-agent-cli"), "utf8")).toBe(
      "development platform agent",
    );
    expect((await stat(join(destDir, "platform-agent-cli"))).mode & 0o777).toBe(0o755);
  });

  it("removes both stale Platform binaries and succeeds when no binary is configured", async () => {
    const destDir = await makeTempDir();
    await writeFile(join(destDir, "platform-agent-cli"), "stale unix");
    await writeFile(join(destDir, "platform-agent-cli.exe"), "stale windows");
    await writeFile(join(destDir, "multica"), "keep me");

    await expect(
      stageDevPlatformAgent({ devBinary: "", targetPlatform: "linux", destDir }),
    ).resolves.toBeUndefined();
    expect((await readdir(destDir)).sort()).toEqual(["multica"]);
  });

  it("removes stale copies and fails when the configured binary is missing", async () => {
    const destDir = await makeTempDir();
    const missingBinary = join(destDir, "does-not-exist");
    await writeFile(join(destDir, "platform-agent-cli"), "stale unix");
    await writeFile(join(destDir, "platform-agent-cli.exe"), "stale windows");

    await expect(
      stageDevPlatformAgent({
        devBinary: missingBinary,
        targetPlatform: "linux",
        destDir,
      }),
    ).rejects.toThrow();
    expect(await readdir(destDir)).toEqual([]);
  });
});
