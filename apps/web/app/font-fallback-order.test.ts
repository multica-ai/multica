import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const repoRoot = resolve(process.cwd(), "../..");
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

// Japanese Kanji share the Han Unicode block with Chinese, so the Korean
// "append after Chinese" tactic would render Japanese with Chinese glyph
// shapes. The Japanese CJK chain must therefore be (a) gated behind a lang
// selector so zh/en keep Chinese-first ordering, and (b) ordered Japanese
// fonts BEFORE the Chinese families inside that scoped stack.
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
  it("keeps web Chinese font fallbacks before Korean font fallbacks", () => {
    const cssSource = readFileSync(
      resolve(repoRoot, "apps/web/app/globals.css"),
      "utf8",
    );

    expectChineseFontsBeforeKoreanFonts(cssSource);
  });

  it("scopes the Japanese-first CJK stack to html[lang|='ja'] (web)", () => {
    const cssSource = readFileSync(
      resolve(repoRoot, "apps/web/app/globals.css"),
      "utf8",
    );

    expectJapaneseScopedOverride(cssSource);
  });

  it("keeps desktop Chinese font fallbacks before Korean font fallbacks", () => {
    const desktopCss = readFileSync(
      resolve(repoRoot, "apps/desktop/src/renderer/src/globals.css"),
      "utf8",
    );

    expectChineseFontsBeforeKoreanFonts(desktopCss);
  });

  it("scopes the Japanese-first CJK stack to html[lang|='ja'] (desktop)", () => {
    const desktopCss = readFileSync(
      resolve(repoRoot, "apps/desktop/src/renderer/src/globals.css"),
      "utf8",
    );

    expectJapaneseScopedOverride(desktopCss);
  });
});
