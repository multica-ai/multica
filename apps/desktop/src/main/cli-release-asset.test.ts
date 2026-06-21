import { describe, expect, it } from "vitest";

import { selectPlatformReleaseAssetName } from "./cli-release-asset";

describe("selectPlatformReleaseAssetName", () => {
  it("matches the versioned darwin archive from release assets", () => {
    const assetNames = [
      "checksums.txt",
      "cs-workflow-cli-1.2.3-darwin-amd64.tar.gz",
      "cs-workflow-cli-1.2.3-darwin-arm64.tar.gz",
      "cs-workflow-cli-1.2.3-linux-amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "darwin", "x64")).toBe(
      "cs-workflow-cli-1.2.3-darwin-amd64.tar.gz",
    );
  });

  it("matches the versioned windows zip archive", () => {
    const assetNames = [
      "cs-workflow-cli-1.2.3-windows-amd64.zip",
      "cs-workflow-cli-1.2.3-linux-amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "win32", "x64")).toBe(
      "cs-workflow-cli-1.2.3-windows-amd64.zip",
    );
  });

  it("fails when the current platform asset is missing", () => {
    expect(() =>
      selectPlatformReleaseAssetName(
        ["cs-workflow-cli-1.2.3-linux-amd64.tar.gz"],
        "darwin",
        "arm64",
      ),
    ).toThrow(/no release asset found/);
  });
});
