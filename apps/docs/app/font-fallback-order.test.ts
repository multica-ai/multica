import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const chineseFonts = ["PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC"];
const koreanFonts = ["Apple SD Gothic Neo", "Malgun Gothic", "Noto Sans CJK KR"];

function expectChineseFontsBeforeKoreanFonts(source: string) {
  const chineseIndexes = chineseFonts.map((font) => source.indexOf(font));
  const koreanIndexes = koreanFonts.map((font) => source.indexOf(font));

  expect(chineseIndexes).not.toContain(-1);
  expect(koreanIndexes).not.toContain(-1);

  for (const chineseIndex of chineseIndexes) {
    for (const koreanIndex of koreanIndexes) {
      expect(chineseIndex).toBeLessThan(koreanIndex);
    }
  }
}

describe("CJK font fallback order", () => {
  it("keeps docs Chinese font fallbacks before Korean font fallbacks", () => {
    const layoutSource = readFileSync(
      resolve(process.cwd(), "app/[lang]/layout.tsx"),
      "utf8",
    );

    expectChineseFontsBeforeKoreanFonts(layoutSource);
  });
});
