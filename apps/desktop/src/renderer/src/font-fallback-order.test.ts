import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function expectChineseFontsBeforeKoreanFonts(source: string) {
  const firstChineseFont = source.indexOf("PingFang SC");
  const lastChineseFont = source.indexOf("Noto Sans CJK SC");
  const firstKoreanFont = source.indexOf("Apple SD Gothic Neo");

  expect(firstChineseFont).toBeGreaterThanOrEqual(0);
  expect(lastChineseFont).toBeGreaterThan(firstChineseFont);
  expect(firstKoreanFont).toBeGreaterThan(lastChineseFont);
}

describe("CJK font fallback order", () => {
  it("keeps desktop Chinese font fallbacks before Korean font fallbacks", () => {
    const desktopCss = readFileSync(
      resolve(process.cwd(), "src/renderer/src/globals.css"),
      "utf8",
    );

    expectChineseFontsBeforeKoreanFonts(desktopCss);
  });
});
