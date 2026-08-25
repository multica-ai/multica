// @vitest-environment node

import { describe, expect, it } from "vitest";
import { resolveMobileLocale } from "./locale";

describe("resolveMobileLocale", () => {
  it.each([
    [{ languageCode: "zh", languageTag: "zh-Hans-CN", regionCode: "CN" }],
    [{ languageCode: "zh", languageTag: "zh-CN", regionCode: "CN" }],
    [{ languageCode: "zh", languageTag: "zh-SG", regionCode: "SG" }],
    [{ languageCode: "zh", languageTag: "zh", regionCode: null }],
  ])("selects Simplified Chinese for %o", (locale) => {
    expect(resolveMobileLocale([locale])).toBe("zh-Hans");
  });

  it.each([
    [{ languageCode: "en", languageTag: "en-US", regionCode: "US" }],
    [{ languageCode: "ja", languageTag: "ja-JP", regionCode: "JP" }],
    [{ languageCode: "zh", languageTag: "zh-Hant-TW", regionCode: "TW" }],
    [{ languageCode: "zh", languageTag: "zh-HK", regionCode: "HK" }],
    [{ languageCode: "zh", languageTag: "zh-MO", regionCode: null }],
  ])("falls back to English for %o", (locale) => {
    expect(resolveMobileLocale([locale])).toBe("en");
  });

  it("falls back to English when locale detection returns nothing", () => {
    expect(resolveMobileLocale([])).toBe("en");
  });
});
