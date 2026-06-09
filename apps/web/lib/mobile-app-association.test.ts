import { describe, expect, it } from "vitest";
import {
  buildAndroidAssetLinks,
  buildAppleAppSiteAssociation,
  hasAndroidAssetLinks,
  hasAppleAppIds,
} from "./mobile-app-association";

describe("mobile app association files", () => {
  it("builds Android asset links from release certificate fingerprints", () => {
    expect(buildAndroidAssetLinks("AA:BB\nCC:DD, AA:BB")).toEqual([
      {
        relation: ["delegate_permission/common.handle_all_urls"],
        target: {
          namespace: "android_app",
          package_name: "com.wujieai.multica",
          sha256_cert_fingerprints: ["AA:BB", "CC:DD"],
        },
      },
    ]);
  });

  it("returns an empty Android association when fingerprints are missing", () => {
    expect(buildAndroidAssetLinks("")).toEqual([]);
  });

  it("checks whether Android App Links configuration exists", () => {
    expect(hasAndroidAssetLinks("AA:BB")).toBe(true);
    expect(hasAndroidAssetLinks("")).toBe(false);
  });

  it("builds Apple app site association from a team id", () => {
    expect(buildAppleAppSiteAssociation("TEAM123").applinks.details).toEqual([
      {
        appIDs: ["TEAM123.com.wujieai.multica"],
        components: [{ "/": "/*/issues/*" }],
      },
    ]);
  });

  it("accepts full Apple app ids", () => {
    expect(buildAppleAppSiteAssociation("TEAM123.com.wujieai.multica").applinks.details[0]?.appIDs).toEqual([
      "TEAM123.com.wujieai.multica",
    ]);
  });

  it("checks whether Apple associated-domain configuration exists", () => {
    expect(hasAppleAppIds("TEAM123")).toBe(true);
    expect(hasAppleAppIds("")).toBe(false);
  });
});
