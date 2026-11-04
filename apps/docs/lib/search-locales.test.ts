// @vitest-environment node
import { describe, expect, it } from "vitest";
import { i18n } from "./i18n";
import { searchLocaleMap, tokenizeCJK, tokenizeJapanese } from "./search-locales";

describe("searchLocaleMap", () => {
  it("covers every Chinese locale declared in i18n with the character-level tokenizer", () => {
    // Latin-script locales (en, fr) can use Orama's default tokenizer; every
    // Chinese locale needs the CJK one or search returns nothing for Han text.
    const chinese = i18n.languages.filter((lang) => lang.startsWith("zh"));
    expect(chinese).toEqual(["zh", "zh-Hant"]);
    expect(searchLocaleMap.zh.components.tokenizer.tokenize).toBe(tokenizeCJK);
    expect(searchLocaleMap["zh-Hant"].components.tokenizer.tokenize).toBe(tokenizeCJK);
  });

  it("tokenizes Traditional Chinese character by character and keeps Latin runs whole", () => {
    const { tokenize } = searchLocaleMap["zh-Hant"].components.tokenizer;
    expect(tokenize("建立 AI 代理")).toEqual(["建", "立", "ai", "代", "理"]);
  });
});

describe("tokenizers", () => {
  it("splits Han characters and keeps Romanized terms whole", () => {
    expect(tokenizeCJK("用 Claude Code 執行")).toEqual(["用", "claude", "code", "執", "行"]);
  });

  it("keeps kana for Japanese", () => {
    expect(tokenizeJapanese("タスクを作成")).toEqual(["タ", "ス", "ク", "を", "作", "成"]);
  });
});
