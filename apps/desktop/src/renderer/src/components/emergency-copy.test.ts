import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resolveDesktopEmergencyLocale } from "./emergency-copy";

describe("resolveDesktopEmergencyLocale", () => {
  const originalLang = document.documentElement.lang;
  const originalDesktopAPI = window.desktopAPI;

  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.lang = "en";
    window.desktopAPI = {
      ...(originalDesktopAPI ?? {}),
      systemLocale: "",
    } as typeof window.desktopAPI;
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.lang = originalLang;
    window.desktopAPI = originalDesktopAPI;
  });

  it("uses explicit preferredLocale when provided", () => {
    expect(resolveDesktopEmergencyLocale("ja")).toBe("ja");
  });

  it("prefers persisted multica-locale over index.html hardcoded lang='en'", () => {
    window.localStorage.setItem("multica-locale", "zh-Hans");
    expect(resolveDesktopEmergencyLocale()).toBe("zh-Hans");
  });

  it("falls back to desktopAPI.systemLocale before hardcoded lang='en'", () => {
    window.desktopAPI = {
      ...(window.desktopAPI ?? {}),
      systemLocale: "zh_TW",
    } as typeof window.desktopAPI;
    expect(resolveDesktopEmergencyLocale()).toBe("zh-Hant");
  });

  it("does not misclassify kok as Korean", () => {
    window.desktopAPI = {
      ...(window.desktopAPI ?? {}),
      systemLocale: "kok-IN",
    } as typeof window.desktopAPI;
    expect(resolveDesktopEmergencyLocale()).toBe("en");
  });
});
