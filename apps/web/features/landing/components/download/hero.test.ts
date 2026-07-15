import { describe, expect, it } from "vitest";
import { createEnDict } from "../../i18n/en";
import type { DownloadAssets } from "../../utils/parse-release-assets";
import { resolveContent } from "./hero";

const dict = createEnDict(false).download.hero;
const assets: DownloadAssets = {
  macArm64Dmg: "https://example.com/mac-arm64.dmg",
  macArm64Zip: "https://example.com/mac-arm64.zip",
  macX64Dmg: "https://example.com/mac-x64.dmg",
  macX64Zip: "https://example.com/mac-x64.zip",
};

describe("macOS download hero", () => {
  it("selects the Intel artifacts when architecture detection is confident", () => {
    const content = resolveContent(
      { os: "mac", arch: "x64", archConfident: true },
      assets,
      false,
      dict,
    );

    expect(content.primary?.href).toBe(assets.macX64Dmg);
    expect(content.alt?.href).toBe(assets.macX64Zip);
  });

  it("keeps Apple Silicon on the established arm64 artifacts", () => {
    const content = resolveContent(
      { os: "mac", arch: "arm64", archConfident: true },
      assets,
      false,
      dict,
    );

    expect(content.primary?.href).toBe(assets.macArm64Dmg);
    expect(content.alt?.href).toBe(assets.macArm64Zip);
  });

  it("offers an explicit architecture choice when Safari cannot distinguish it", () => {
    const content = resolveContent(
      { os: "mac", arch: "unknown", archConfident: false },
      assets,
      false,
      dict,
    );

    expect(content.primary).toMatchObject({ href: assets.macArm64Dmg });
    expect(content.alt).toMatchObject({ href: assets.macX64Dmg });
    expect(content.hint).toBe(dict.macUnknown.hint);
  });
});
