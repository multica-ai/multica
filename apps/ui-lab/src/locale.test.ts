// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import i18next from "i18next";
import {
  isLabLocale,
  loadLocale,
  LOCALE_STORAGE_KEY,
  productLocale,
} from "./locale";
import { isPreviewSettings } from "./protocol";
import { emptyDraft } from "./tokens";
import en from "./locales/en.json";
import zh from "./locales/zh.json";

afterEach(() => vi.unstubAllGlobals());

describe("UI Lab language preference", () => {
  it("defaults to English regardless of browser language and accepts only en or zh", () => {
    vi.stubGlobal("navigator", { language: "zh-CN" });
    for (const saved of [null, "", "ja", "zh-Hans", "garbage"]) {
      vi.stubGlobal("localStorage", {
        getItem: vi.fn().mockReturnValue(saved),
      });
      expect(loadLocale()).toBe("en");
    }
    for (const saved of ["en", "zh"]) {
      const getItem = vi.fn().mockReturnValue(saved);
      vi.stubGlobal("localStorage", { getItem });
      expect(loadLocale()).toBe(saved);
      expect(getItem).toHaveBeenCalledWith(LOCALE_STORAGE_KEY);
    }
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("Storage blocked");
      },
    });
    expect(loadLocale()).toBe("en");
  });

  it("validates iframe locales and maps zh to the production locale", () => {
    const message = {
      type: "multica-ui-lab:preview",
      selectedColor: "--brand",
      playbackSpeed: 1,
      scene: "button",
      theme: "light",
      buttonScale: "default",
      draft: emptyDraft(),
    };
    for (const locale of [
      undefined,
      null,
      "",
      "fr",
      "zh-Hans",
      {},
      "en",
      "zh",
    ]) {
      expect(isPreviewSettings({ ...message, locale })).toBe(
        isLabLocale(locale),
      );
    }
    expect(productLocale("en")).toBe("en");
    expect(productLocale("zh")).toBe("zh-Hans");
  });

  it("provides both catalogs with matching keys and working counts and interpolation", () => {
    const keys = (value: object, prefix = ""): string[] =>
      Object.entries(value).flatMap(([key, item]) =>
        item && typeof item === "object"
          ? keys(item, `${prefix}${key}.`)
          : [`${prefix}${key}`],
      );
    expect(keys(zh).sort()).toEqual(keys(en).sort());
    const i18n = i18next.createInstance();
    void i18n.init({
      lng: "en",
      initAsync: false,
      resources: { en: { uiLab: en }, zh: { uiLab: zh } },
    });
    const english = i18n.getFixedT("en", "uiLab");
    const chinese = i18n.getFixedT("zh", "uiLab");
    expect(english(($) => $.lab.preview.changeCount, { count: 1 })).toBe(
      "1 change",
    );
    expect(english(($) => $.lab.preview.changeCount, { count: 2 })).toBe(
      "2 changes",
    );
    expect(chinese(($) => $.lab.preview.changeCount, { count: 2 })).toBe(
      "2 项修改",
    );
    expect(
      english(($) => $.lab.notice.loaded, { name: "Custom <design>" }),
    ).toContain("Custom");
  });
});
