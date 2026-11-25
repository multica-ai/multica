import { describe, expect, it } from "vitest";
import { matchLocale, pickLocale } from "./pick-locale";
import type { LocaleAdapter } from "./types";

function makeAdapter(overrides: Partial<LocaleAdapter> = {}): LocaleAdapter {
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
    expect(matchLocale(["fr"])).toBe("fr");
    expect(matchLocale(["es"])).toBe("es");
    expect(matchLocale(["ru"])).toBe("ru");
    expect(matchLocale(["pt-BR"])).toBe("pt-BR");
  });

  it("collapses region-tagged BCP-47 to the supported base", () => {
    expect(matchLocale(["en-US"])).toBe("en");
    expect(matchLocale(["zh-Hans-CN"])).toBe("zh-Hans");
    expect(matchLocale(["ko-KR"])).toBe("ko");
    expect(matchLocale(["ja-JP"])).toBe("ja");
    expect(matchLocale(["fr-FR"])).toBe("fr");
    expect(matchLocale(["fr-CA"])).toBe("fr");
    expect(matchLocale(["es-ES"])).toBe("es");
    expect(matchLocale(["es-MX"])).toBe("es");
    expect(matchLocale(["ru-RU"])).toBe("ru");
    expect(matchLocale(["ru-BY"])).toBe("ru");
    expect(matchLocale(["pt"])).toBe("pt-BR");
    expect(matchLocale(["pt-PT"])).toBe("pt-BR");
  });

  it("falls back to DEFAULT_LOCALE when no candidate matches", () => {
    expect(matchLocale(["it", "de"])).toBe("en");
  });

  it("Traditional-script and TW/HK tags resolve to zh-Hant, mainland tags to zh-Hans", () => {
    expect(matchLocale(["zh-Hant"])).toBe("zh-Hant");
    expect(matchLocale(["zh-TW"])).toBe("zh-Hant");
    expect(matchLocale(["zh-HK"])).toBe("zh-Hant");
    expect(matchLocale(["zh-CN"])).toBe("zh-Hans");
  });

  it("uses the first supported candidate when multiple appear", () => {
    expect(matchLocale(["it", "zh-Hans", "en"])).toBe("zh-Hans");
    expect(matchLocale(["it", "ko-KR", "en"])).toBe("ko");
    expect(matchLocale(["it", "ja-JP", "en"])).toBe("ja");
    expect(matchLocale(["it", "pt-BR", "en"])).toBe("pt-BR");
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
      getSystemPreferences: () => ["it", "de"],
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
