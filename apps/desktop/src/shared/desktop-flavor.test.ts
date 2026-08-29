import { describe, expect, it } from "vitest";
import { desktopIdentity, resolveDesktopFlavor } from "./desktop-flavor";

describe("resolveDesktopFlavor", () => {
  it("recognizes the dedicated packaged LifeOS app", () => {
    expect(resolveDesktopFlavor("LifeOS")).toBe("lifeos");
  });

  it("supports a dedicated LifeOS development build", () => {
    expect(resolveDesktopFlavor("Multica", "true")).toBe("lifeos");
  });

  it("keeps the existing Multica build unchanged", () => {
    expect(resolveDesktopFlavor("Multica")).toBe("multica");
  });
});

describe("desktopIdentity", () => {
  it("isolates the LifeOS bundle identity and protocol", () => {
    expect(desktopIdentity("lifeos", false)).toEqual({
      appName: "LifeOS",
      appUserModelId: "ai.lifeos.desktop",
      protocol: "lifeos",
    });
  });

  it("preserves worktree suffixes in development", () => {
    expect(desktopIdentity("lifeos", true, "client").appName).toBe(
      "LifeOS Canary client",
    );
  });
});
