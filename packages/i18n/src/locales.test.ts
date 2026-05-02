import { describe, expect, test } from "vitest";
import { DEFAULT_LOCALE, isLocale, normalizeLocale, SUPPORTED_LOCALES } from "./locales";

describe("locales", () => {
  test("SUPPORTED_LOCALES contains zh-CN and en", () => {
    expect(SUPPORTED_LOCALES).toContain("zh-CN");
    expect(SUPPORTED_LOCALES).toContain("en");
  });

  test("DEFAULT_LOCALE is zh-CN", () => {
    expect(DEFAULT_LOCALE).toBe("zh-CN");
  });

  test("isLocale rejects unknown values", () => {
    expect(isLocale("zh-CN")).toBe(true);
    expect(isLocale("en")).toBe(true);
    expect(isLocale("fr")).toBe(false);
    expect(isLocale(undefined)).toBe(false);
  });

  test("normalizeLocale tolerates short forms and falls back to default", () => {
    expect(normalizeLocale("zh-CN")).toBe("zh-CN");
    expect(normalizeLocale("zh")).toBe("zh-CN");
    expect(normalizeLocale("zh-TW")).toBe("zh-CN");
    expect(normalizeLocale("en")).toBe("en");
    expect(normalizeLocale("en-US")).toBe("en");
    expect(normalizeLocale("fr-FR")).toBe(DEFAULT_LOCALE);
    expect(normalizeLocale(null)).toBe(DEFAULT_LOCALE);
    expect(normalizeLocale("")).toBe(DEFAULT_LOCALE);
  });
});
