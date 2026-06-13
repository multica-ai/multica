import { describe, expect, it } from "vitest";
import { matchLocale, pickLocale } from "./pick-locale";
import type { LocaleAdapter } from "./types";

function makeAdapter(
  overrides: Partial<LocaleAdapter> = {},
): LocaleAdapter {
  return {
    getUserChoice: () => null,
    getSystemPreferences: () => [],
    persist: () => {},
    ...overrides,
  };
}

describe("matchLocale", () => {
  it("returns DEFAULT_LOCALE when given an empty list", () => {
    expect(matchLocale([])).toBe("en");
  });

  it("matches a clean supported tag", () => {
    expect(matchLocale(["zh-Hans"])).toBe("zh-Hans");
    expect(matchLocale(["ko"])).toBe("ko");
    expect(matchLocale(["ja"])).toBe("ja");
    expect(matchLocale(["en"])).toBe("en");
  });

  it("collapses region-tagged BCP-47 to the supported base", () => {
    expect(matchLocale(["en-US"])).toBe("en");
    expect(matchLocale(["zh-Hans-CN"])).toBe("zh-Hans");
    expect(matchLocale(["ko-KR"])).toBe("ko");
    expect(matchLocale(["ja-JP"])).toBe("ja");
  });

  it("falls back to DEFAULT_LOCALE when no candidate matches", () => {
    expect(matchLocale(["fr", "de"])).toBe("en");
  });

  it("zh-Hant (traditional) collapses to zh-Hans — same base subtag, better UX than English fallback", () => {
    expect(matchLocale(["zh-Hant"])).toBe("zh-Hans");
  });

  it("uses the first supported candidate when multiple appear", () => {
    expect(matchLocale(["fr", "zh-Hans", "en"])).toBe("zh-Hans");
    expect(matchLocale(["fr", "ko-KR", "en"])).toBe("ko");
    expect(matchLocale(["fr", "ja-JP", "en"])).toBe("ja");
  });

  it("matches Vietnamese region tags to vi", () => {
    expect(matchLocale(["vi"])).toBe("vi");
    expect(matchLocale(["vi-VN"])).toBe("vi");
    expect(matchLocale(["fr", "vi-VN", "en"])).toBe("vi");
  });

  it("returns DEFAULT_LOCALE for malformed BCP-47 tags rather than throwing", () => {
    expect(matchLocale(["----"])).toBe("en");
    expect(matchLocale(["x-private-only"])).toBe("en");
  });
});

describe("pickLocale", () => {
  it("prefers explicit user choice over system signal", () => {
    const adapter = makeAdapter({
      getUserChoice: () => "zh-Hans",
      getSystemPreferences: () => ["en-US"],
    });
    expect(pickLocale(adapter)).toBe("zh-Hans");
  });

  it("falls back to system preferences when no user choice", () => {
    const adapter = makeAdapter({
      getSystemPreferences: () => ["zh-Hans-CN", "en-US"],
    });
    expect(pickLocale(adapter)).toBe("zh-Hans");
  });

  it("returns DEFAULT_LOCALE when neither choice nor preference yields a match", () => {
    const adapter = makeAdapter({
      getUserChoice: () => null,
      getSystemPreferences: () => ["fr", "de"],
    });
    expect(pickLocale(adapter)).toBe("en");
  });

  it("ignores empty-string user choice and falls through to system", () => {
    const adapter = makeAdapter({
      getUserChoice: () => "",
      getSystemPreferences: () => ["zh-Hans"],
    });
    expect(pickLocale(adapter)).toBe("zh-Hans");
  });
});
