// @vitest-environment node
import { describe, expect, it } from "vitest";
import { getDir } from "./direction";

describe("getDir", () => {
  it.each(["ar", "ar-SA", "AR-eg", "ar_EG", "he", "fa-IR", "ur"])("returns rtl for %s", (locale) => {
    expect(getDir(locale)).toBe("rtl");
  });

  it.each(["en", "zh-Hans", "ja", "ko", "fr-FR", "", "arc-Syrc-x"])("returns ltr for %s", (locale) => {
    expect(getDir(locale)).toBe("ltr");
  });
});
