import { describe, expect, it } from "vitest";
import { resolveLocale } from "./locale";

const device = (languageCode: string, regionCode: string | null = null, scriptCode?: string) => ({
  languageCode,
  regionCode,
  scriptCode,
});

describe("resolveLocale", () => {
  it("uses the first supported device language", () => {
    expect(resolveLocale([device("de", "DE"), device("ja", "JP"), device("en")])).toBe("ja");
  });

  it("tells Simplified and Traditional Chinese apart", () => {
    expect(resolveLocale([device("zh", "CN", "Hans")])).toBe("zh-Hans");
    expect(resolveLocale([device("zh", "TW", "Hant")])).toBe("zh-Hant");
    expect(resolveLocale([device("zh", "HK")])).toBe("zh-Hant");
    expect(resolveLocale([device("zh", "SG")])).toBe("zh-Hans");
    expect(resolveLocale([device("zh", "TW", "Hans")])).toBe("zh-Hans");
  });

  it("falls back to English", () => {
    expect(resolveLocale([device("de", "DE")])).toBe("en");
    expect(resolveLocale([])).toBe("en");
  });
});
