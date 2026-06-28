import { delimiter, resolve } from "node:path";
import { describe, it, expect } from "vitest";
import {
  builderArgsForTarget,
  envWithLocalBins,
  normalizeGitVersion,
  parsePackageArgs,
  resolveBuildMatrix,
  stripLeadingSeparator,
} from "./package.mjs";

describe("normalizeGitVersion", () => {
  it("returns null for empty / nullish input", () => {
    expect(normalizeGitVersion("")).toBe(null);
    expect(normalizeGitVersion(null)).toBe(null);
    expect(normalizeGitVersion(undefined)).toBe(null);
  });

  it("strips the leading v on a clean tag", () => {
    expect(normalizeGitVersion("v0.1.36")).toBe("0.1.36");
    expect(normalizeGitVersion("v1.0.0")).toBe("1.0.0");
  });

  it("preserves the prerelease suffix between tags", () => {
    expect(normalizeGitVersion("v0.1.35-14-gf1415e96")).toBe(
      "0.1.35-14-gf1415e96",
    );
  });

  it("preserves the dirty suffix on a modified worktree", () => {
    expect(normalizeGitVersion("v0.1.35-14-gf1415e96-dirty")).toBe(
      "0.1.35-14-gf1415e96-dirty",
    );
  });

  it("handles v-prefixed prerelease tags", () => {
    expect(normalizeGitVersion("v1.0.0-alpha")).toBe("1.0.0-alpha");
    expect(normalizeGitVersion("v1.0.0-rc.2")).toBe("1.0.0-rc.2");
  });

  it("falls back to 0.0.0-g<hash> when no tags are reachable", () => {
    // `git describe --tags --always` returns just the short commit hash
    // when there are no tags in the history at all. A hash that begins with
    // a digit (e.g. "2f24057b") is still not valid semver and must fall
    // through — otherwise electron-updater rejects it on launch. The `g`
    // prefix mirrors git describe's own `g<hash>` shorthand and keeps the
    // pre-release identifier a single alphanumeric token.
    expect(normalizeGitVersion("f1415e96")).toBe("0.0.0-gf1415e96");
    expect(normalizeGitVersion("abc1234")).toBe("0.0.0-gabc1234");
    expect(normalizeGitVersion("2f24057b")).toBe("0.0.0-g2f24057b");
  });

  it("prefixes an all-digit hash so the pre-release is valid semver", () => {
    // A short hash that is all decimal digits with a leading zero would
    // produce `0.0.0-0123456` — a numeric pre-release identifier must not
    // have a leading zero, so that value is invalid semver and
    // electron-updater would throw on the no-tag builds this fallback
    // exists to protect. The `g` prefix makes it a single alphanumeric
    // identifier, which is always valid.
    expect(normalizeGitVersion("0123456")).toBe("0.0.0-g0123456");
    expect(normalizeGitVersion("04567")).toBe("0.0.0-g04567");
  });
});

describe("stripLeadingSeparator", () => {
  it("removes the leading -- inserted by npm/pnpm", () => {
    expect(stripLeadingSeparator(["--", "--mac", "--arm64", "--publish", "always"])).toEqual([
      "--mac", "--arm64", "--publish", "always",
    ]);
  });

  it("leaves args untouched when there is no leading --", () => {
    expect(stripLeadingSeparator(["--mac", "--arm64"])).toEqual(["--mac", "--arm64"]);
  });

  it("does not strip a -- that appears mid-argv", () => {
    expect(stripLeadingSeparator(["--mac", "--", "--arm64"])).toEqual([
      "--mac", "--", "--arm64",
    ]);
  });

  it("handles an empty array", () => {
    expect(stripLeadingSeparator([])).toEqual([]);
  });
});

describe("parsePackageArgs", () => {
  it("collects per-platform targets and shared args", () => {
    expect(
      parsePackageArgs([
        "--win", "nsis",
        "--mac", "dmg", "zip",
        "--arm64",
        "--publish", "never",
      ]),
    ).toEqual({
      allPlatforms: false,
      sharedArgs: ["--publish", "never"],
      platformTargets: {
        mac: ["dmg", "zip"],
        win: ["nsis"],
        linux: [],
      },
      requestedPlatforms: ["win", "mac"],
      requestedArchs: ["arm64"],
    });
  });

  it("expands combined short flags", () => {
    expect(parsePackageArgs(["-mw", "--x64"]).requestedPlatforms).toEqual([
      "mac",
      "win",
    ]);
  });

  it("tracks the all-platforms shortcut", () => {
    expect(parsePackageArgs(["--all-platforms", "--publish", "never"]).allPlatforms).toBe(true);
  });
});

describe("resolveBuildMatrix", () => {
  it("defaults to the current host platform and arch", () => {
    expect(
      resolveBuildMatrix(
        {
          allPlatforms: false,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: [],
          requestedArchs: [],
        },
        "darwin",
        "arm64",
      ),
    ).toEqual([{ platform: "mac", arch: "arm64" }]);
  });

  it("expands all-platforms on macOS", () => {
    expect(
      resolveBuildMatrix(
        {
          allPlatforms: true,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: [],
          requestedArchs: [],
        },
        "darwin",
        "arm64",
      ),
    ).toEqual([
      { platform: "mac", arch: "arm64" },
      { platform: "win", arch: "x64" },
      { platform: "win", arch: "arm64" },
      { platform: "linux", arch: "x64" },
      { platform: "linux", arch: "arm64" },
    ]);
  });

  it("rejects unsupported architectures", () => {
    expect(() =>
      resolveBuildMatrix(
        {
          allPlatforms: false,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: ["win"],
          requestedArchs: ["universal"],
        },
        "darwin",
        "arm64",
      ),
    ).toThrow(/unsupported Desktop CLI architecture/);
  });
});

describe("builderArgsForTarget", () => {
  it("adds scoped output directories for multi-target builds", () => {
    expect(
      builderArgsForTarget(
        { platform: "win", arch: "arm64" },
        {
          allPlatforms: false,
          sharedArgs: ["--publish", "never"],
          platformTargets: { mac: [], win: ["nsis"], linux: [] },
          requestedPlatforms: ["win"],
          requestedArchs: ["arm64"],
        },
        "1.2.3",
        {
          disableMacNotarize: true,
          hostPlatform: "darwin",
          useScopedOutputDir: true,
        },
      ),
    ).toEqual([
      "-c.extraMetadata.version=1.2.3",
      "-c.mac.notarize=false",
      "--win",
      "nsis",
      "--arm64",
      "--publish",
      "never",
      "-c.directories.output=dist/win-arm64",
      "-c.publish.channel=latest-arm64",
    ]);
  });

  it("does not override the publish channel for Windows x64 (default latest.yml)", () => {
    expect(
      builderArgsForTarget(
        { platform: "win", arch: "x64" },
        {
          allPlatforms: false,
          sharedArgs: ["--publish", "always"],
          platformTargets: { mac: [], win: ["nsis"], linux: [] },
          requestedPlatforms: ["win"],
          requestedArchs: ["x64"],
        },
        "1.2.3",
        { hostPlatform: "win32", useScopedOutputDir: true },
      ),
    ).toEqual([
      "-c.extraMetadata.version=1.2.3",
      "--win",
      "nsis",
      "--x64",
      "--publish",
      "always",
      "-c.directories.output=dist/win-x64",
    ]);
  });

  it("defaults linux cross-builds to AppImage on non-Linux hosts", () => {
    expect(
      builderArgsForTarget(
        { platform: "linux", arch: "x64" },
        {
          allPlatforms: false,
          sharedArgs: ["--publish", "never"],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: ["linux"],
          requestedArchs: ["x64"],
        },
        "1.2.3",
        { hostPlatform: "darwin" },
      ),
    ).toEqual([
      "-c.extraMetadata.version=1.2.3",
      "--linux",
      "AppImage",
      "--x64",
      "--publish",
      "never",
    ]);
  });
});

describe("envWithLocalBins", () => {
  it("prepends desktop-local binary directories to PATH", () => {
    const desktopRoot = "/repo/apps/desktop";
    const result = envWithLocalBins(
      { PATH: ["/usr/local/bin", "/usr/bin"].join(delimiter) },
      desktopRoot,
    );
    expect(result.PATH.split(delimiter)).toEqual([
      resolve(desktopRoot, "node_modules", ".bin"),
      resolve(desktopRoot, "..", "..", "node_modules", ".bin"),
      "/usr/local/bin",
      "/usr/bin",
    ]);
  });

  it("preserves an existing Path key and avoids duplicate entries", () => {
    const desktopRoot = "/repo/apps/desktop";
    const desktopBin = resolve(desktopRoot, "node_modules", ".bin");
    const workspaceBin = resolve(desktopRoot, "..", "..", "node_modules", ".bin");
    const result = envWithLocalBins(
      { Path: [desktopBin, "runner-bin", workspaceBin].join(delimiter) },
      desktopRoot,
    );
    expect(result).not.toHaveProperty("PATH");
    expect(result.Path.split(delimiter)).toEqual([
      desktopBin,
      workspaceBin,
      "runner-bin",
    ]);
  });
});
