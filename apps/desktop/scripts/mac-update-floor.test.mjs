// @vitest-environment node
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const desktopRoot = resolve(import.meta.dirname, "..");
const {
  applyMacUpdateFloor,
  MAC_UPDATE_MIN_DARWIN_VERSION,
} = require("./mac-update-floor.cjs");
// Parse with the same YAML library electron-updater uses (not a direct dep).
const yaml = createRequire(require.resolve("electron-updater"))("js-yaml");
// The same module object electron-updater reads `release()` from.
const os = require("node:os");
// Real decision logic shipped in installed clients (v0.6.5 bundles
// electron-updater 6.8.3). Deep import: the package has no `exports` map.
const { AppUpdater } = require("electron-updater/out/AppUpdater");

function macContext(releaseInfo) {
  return {
    electronPlatformName: "darwin",
    packager: { platformSpecificBuildOptions: { releaseInfo } },
  };
}

// Shape electron-builder's createUpdateInfo() produces:
// { version, files, path, sha512, ...platformSpecificBuildOptions.releaseInfo }
function publishedUpdateInfo(context) {
  return {
    version: "0.7.0",
    files: [{ url: "multica-desktop-0.7.0-mac-arm64.zip", sha512: "x" }],
    path: "multica-desktop-0.7.0-mac-arm64.zip",
    sha512: "x",
    ...context.packager.platformSpecificBuildOptions.releaseInfo,
  };
}

// What an installed client decides after fetching latest-mac.yml.
function installedClientAcceptsUpdate(ymlText, darwinRelease) {
  vi.spyOn(os, "release").mockReturnValue(darwinRelease);
  const updateInfo = yaml.load(ymlText);
  const fakeUpdater = { _logger: { info() {}, warn() {} } };
  return AppUpdater.prototype.checkIfUpdateSupported.call(fakeUpdater, updateInfo);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("mac-update-floor beforePack hook", () => {
  it("adds a Darwin kernel floor (not a macOS marketing version)", () => {
    expect(MAC_UPDATE_MIN_DARWIN_VERSION).toBe("22.0.0");
    const ctx = macContext({ releaseNotes: "notes" });

    applyMacUpdateFloor(ctx);

    expect(ctx.packager.platformSpecificBuildOptions.releaseInfo).toEqual({
      releaseNotes: "notes",
      minimumSystemVersion: "22.0.0",
    });
  });

  it("leaves Windows and Linux metadata untouched", () => {
    for (const platform of ["win32", "linux"]) {
      const ctx = macContext(undefined);
      ctx.electronPlatformName = platform;

      applyMacUpdateFloor(ctx);

      expect(ctx.packager.platformSpecificBuildOptions.releaseInfo).toBeUndefined();
    }
  });

  it("fails the build instead of silently publishing metadata without the floor", () => {
    expect(() =>
      applyMacUpdateFloor({ electronPlatformName: "darwin", packager: {} }),
    ).toThrow(/minimumSystemVersion/);
  });

  it("is wired into electron-builder.yml together with the macOS 13 bundle floor", () => {
    const config = yaml.load(
      readFileSync(resolve(desktopRoot, "electron-builder.yml"), "utf8"),
    );
    expect(config.beforePack).toBe("./scripts/mac-update-floor.cjs");
    expect(config.mac.minimumSystemVersion).toBe("13.0.0");
  });
});

describe("installed clients (electron-updater 6.8.3) consuming the new metadata", () => {
  const ctx = macContext(undefined);
  applyMacUpdateFloor(ctx);
  const yml = yaml.dump(publishedUpdateInfo(ctx));

  it("macOS 12 and older skip the update", () => {
    expect(installedClientAcceptsUpdate(yml, "21.6.0")).toBe(false); // macOS 12
    expect(installedClientAcceptsUpdate(yml, "20.6.0")).toBe(false); // macOS 11
  });

  it("macOS 13+ still update", () => {
    expect(installedClientAcceptsUpdate(yml, "22.1.0")).toBe(true); // macOS 13
    expect(installedClientAcceptsUpdate(yml, "25.0.0")).toBe(true);
  });

  it("documents the traps: no floor, or a marketing-version floor, lets macOS 12 download", () => {
    const base = publishedUpdateInfo(macContext(undefined));
    expect(installedClientAcceptsUpdate(yaml.dump(base), "21.6.0")).toBe(true);
    expect(
      installedClientAcceptsUpdate(
        yaml.dump({ ...base, minimumSystemVersion: "13.0.0" }),
        "21.6.0",
      ),
    ).toBe(true);
  });
});
