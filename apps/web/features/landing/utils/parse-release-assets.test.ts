import { describe, expect, it } from "vitest";
import { parseReleaseAssets } from "./parse-release-assets";

function asset(name: string) {
  return {
    name,
    browser_download_url: `https://example.com/${name}`,
  };
}

describe("parseReleaseAssets", () => {
  it("keeps Apple Silicon and Intel macOS installers separate", () => {
    const assets = [
      asset("multica-desktop-0.4.2-mac-arm64.dmg"),
      asset("multica-desktop-0.4.2-mac-arm64.zip"),
      asset("multica-desktop-0.4.2-mac-x64.dmg"),
      asset("multica-desktop-0.4.2-mac-x64.zip"),
    ];

    expect(parseReleaseAssets(assets)).toEqual({
      macArm64Dmg:
        "https://example.com/multica-desktop-0.4.2-mac-arm64.dmg",
      macArm64Zip:
        "https://example.com/multica-desktop-0.4.2-mac-arm64.zip",
      macX64Dmg: "https://example.com/multica-desktop-0.4.2-mac-x64.dmg",
      macX64Zip: "https://example.com/multica-desktop-0.4.2-mac-x64.zip",
    });
  });

  it("ignores update metadata and unsupported macOS architectures", () => {
    expect(
      parseReleaseAssets([
        asset("latest-mac.yml"),
        asset("latest-x64-mac.yml"),
        asset("multica-desktop-0.4.2-mac-universal.dmg"),
      ]),
    ).toEqual({});
  });
});
