// @vitest-environment node
import { describe, expect, it } from "vitest";
import { alternateKey, isDownloadKey, platformOf, resolvePrimary } from "./download-target";
import { detectFromUserAgent } from "./os-detect";

const ASSETS = {
  macArm64Dmg: "https://downloads.test/multica-desktop-0.8.1-mac-arm64.dmg",
  macX64Dmg: "https://downloads.test/multica-desktop-0.8.1-mac-x64.dmg",
  winX64Exe: "https://downloads.test/multica-desktop-0.8.1-windows-x64.exe",
  linuxArm64AppImage: "https://downloads.test/multica-desktop-0.8.1-linux-arm64.AppImage",
};

const SAFARI_MAC =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15";
const CHROME_WINDOWS =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36";
const FIREFOX_LINUX_ARM = "Mozilla/5.0 (X11; Linux aarch64; rv:131.0) Gecko/20100101 Firefox/131.0";
const IPHONE =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

describe("detectFromUserAgent", () => {
  it("defaults Macs to Apple Silicon without claiming certainty", () => {
    expect(detectFromUserAgent(SAFARI_MAC)).toEqual({ os: "mac", arch: "arm64", archConfident: false });
  });

  it("reads Windows, Linux architecture and phones", () => {
    expect(detectFromUserAgent(CHROME_WINDOWS).os).toBe("windows");
    expect(detectFromUserAgent(FIREFOX_LINUX_ARM)).toMatchObject({ os: "linux", arch: "arm64" });
    expect(detectFromUserAgent(IPHONE).os).toBe("mobile");
  });

  it("trusts the Sec-CH-UA hints over the UA string", () => {
    expect(detectFromUserAgent(CHROME_WINDOWS, { platform: '"macOS"' }).os).toBe("mac");
    expect(detectFromUserAgent(CHROME_WINDOWS, { mobile: "?1" }).os).toBe("mobile");
  });
});

describe("resolvePrimary", () => {
  it("offers the installer for the detected platform", () => {
    expect(resolvePrimary(detectFromUserAgent(SAFARI_MAC), ASSETS)).toEqual({ key: "macArm64Dmg", href: ASSETS.macArm64Dmg });
    expect(resolvePrimary({ os: "mac", arch: "x64", archConfident: true }, ASSETS)?.key).toBe("macX64Dmg");
    expect(resolvePrimary(detectFromUserAgent(FIREFOX_LINUX_ARM), ASSETS)?.key).toBe("linuxArm64AppImage");
  });

  it("offers nothing to phones, unknown systems or a release without the file", () => {
    expect(resolvePrimary(detectFromUserAgent(IPHONE), ASSETS)).toBeNull();
    expect(resolvePrimary(detectFromUserAgent(""), ASSETS)).toBeNull();
    expect(resolvePrimary(detectFromUserAgent(CHROME_WINDOWS), {})).toBeNull();
  });
});

describe("download keys", () => {
  it("validates the started param and maps keys to platform rows", () => {
    expect(isDownloadKey("linuxAmd64Deb")).toBe(true);
    expect(isDownloadKey("../etc")).toBe(false);
    expect(platformOf("linuxAmd64Deb")).toBe("linuxX64");
    expect(alternateKey("macArm64Dmg")).toBe("macX64Dmg");
    expect(alternateKey("winX64Exe")).toBeNull();
  });
});
