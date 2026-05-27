import { describe, expect, it } from "vitest";
import {
  isSupportedLocale,
  resolveLocaleFromSignals,
} from "./locale-routing";

describe("locale routing", () => {
  it("accepts only app-supported locale identifiers", () => {
    expect(isSupportedLocale("en")).toBe(true);
    expect(isSupportedLocale("zh-Hans")).toBe(true);
    expect(isSupportedLocale("zh")).toBe(false);
    expect(isSupportedLocale(null)).toBe(false);
  });

  it("normalizes legacy landing zh cookies to the app locale", () => {
    expect(
      resolveLocaleFromSignals({
        cookieLocale: "zh",
        acceptLanguage: "en-US,en;q=0.9",
      }),
    ).toBe("zh-Hans");
  });

  it("prefers cookie locale over Accept-Language", () => {
    expect(
      resolveLocaleFromSignals({
        cookieLocale: "en",
        acceptLanguage: "zh-CN,zh;q=0.9",
      }),
    ).toBe("en");
  });

  it("falls back to Accept-Language when no cookie is set", () => {
    expect(
      resolveLocaleFromSignals({
        acceptLanguage: "zh-CN,zh;q=0.9,en;q=0.8",
      }),
    ).toBe("zh-Hans");
  });
});
