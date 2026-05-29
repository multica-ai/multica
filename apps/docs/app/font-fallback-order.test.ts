import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const chineseFonts = ["PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC"];
const koreanFonts = ["Apple SD Gothic Neo", "Malgun Gothic", "Noto Sans CJK KR"];
const japaneseFonts = ["Hiragino Sans", "Yu Gothic", "Noto Sans CJK JP"];

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

// Japanese Kanji share the Han Unicode block with Chinese, so the docs
// Japanese-first CJK stack must be scoped to html[lang|="ja"] (zh/en keep
// Chinese-first) and order Japanese fonts before the Chinese families.
function expectJapaneseScopedOverride(source: string) {
  expect(source).toContain('html[lang|="ja"]');

  const japaneseIndexes = japaneseFonts.map((font) => source.indexOf(font));
  expect(japaneseIndexes).not.toContain(-1);

  const firstJapanese = Math.min(...japaneseIndexes);
  const lastChinese = Math.max(
    ...chineseFonts.map((font) => source.lastIndexOf(font)),
  );
  expect(firstJapanese).toBeLessThan(lastChinese);
}

describe("CJK font fallback order", () => {
  it("keeps docs Chinese font fallbacks before Korean font fallbacks", () => {
    const cssSource = readFileSync(
      resolve(process.cwd(), "app/global.css"),
      "utf8",
    );

    expectChineseFontsBeforeKoreanFonts(cssSource);
  });

  it("scopes the Japanese-first CJK stack to html[lang|='ja']", () => {
    const cssSource = readFileSync(
      resolve(process.cwd(), "app/global.css"),
      "utf8",
    );

    expectJapaneseScopedOverride(cssSource);
  });
});
