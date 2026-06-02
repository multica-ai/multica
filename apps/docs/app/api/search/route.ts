import { source } from "@/lib/source";
import { createFromSource } from "fumadocs-core/search/server";

// Orama doesn't ship a Chinese tokenizer and its built-in English regex
// strips Han characters entirely, so `locale=zh` would either return empty
// results or throw. Tokenize CJK input character-by-character and keep
// Latin/digit runs whole — gives serviceable recall for Chinese docs while
// letting Romanized terms (product names, CLI commands) still match.
function tokenizeCJK(raw: string): string[] {
  const tokens: string[] = [];
  const regex = /[一-鿿㐀-䶿]|[A-Za-z0-9]+/g;
  const lower = raw.toLowerCase();
  let match: RegExpExecArray | null;
  while ((match = regex.exec(lower)) !== null) {
    tokens.push(match[0]);
  }
  return tokens;
}

// Japanese mixes Hiragana, Katakana and Kanji; the English regex strips them
// all, and the zh tokenizer only keeps Han (Kanji), dropping kana entirely.
// Tokenize each kana/Kanji codepoint on its own and keep Latin/digit runs
// whole — same character-level recall strategy as tokenizeCJK, extended to
// the Hiragana (\u3040-\u309f) and Katakana (\u30a0-\u30ff) blocks, plus the
// ideographic iteration mark \u3005 which sits just below the kana blocks and
// recurs in common words (e.g. the JP for "various", "daily", "individual").
function tokenizeJapanese(raw: string): string[] {
  const tokens: string[] = [];
  const regex = /[\u3005\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff]|[A-Za-z0-9]+/g;
  const lower = raw.toLowerCase();
  let match: RegExpExecArray | null;
  while ((match = regex.exec(lower)) !== null) {
    tokens.push(match[0]);
  }
  return tokens;
}

export const { GET } = createFromSource(source, {
  localeMap: {
    ko: {
      components: {
        tokenizer: {
          language: "english",
        },
      },
    },
    ja: {
      components: {
        tokenizer: {
          language: "english",
          normalizationCache: new Map(),
          tokenize: tokenizeJapanese,
        },
      },
    },
    zh: {
      components: {
        tokenizer: {
          language: "english",
          normalizationCache: new Map(),
          tokenize: tokenizeCJK,
        },
      },
    },
  },
});
