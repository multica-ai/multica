import type { createFromSource } from "fumadocs-core/search/server";

type LocaleMap = NonNullable<
  NonNullable<Parameters<typeof createFromSource>[1]>["localeMap"]
>;

// Orama doesn't ship a Chinese tokenizer and its built-in English regex
// strips Han characters entirely, so a Chinese locale left on the default
// would either return empty results or throw. Tokenize CJK input
// character-by-character and keep Latin/digit runs whole — gives serviceable
// recall for Chinese docs while letting Romanized terms (product names, CLI
// commands) still match.
export function tokenizeCJK(raw: string): string[] {
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
// the Hiragana (぀-ゟ) and Katakana (゠-ヿ) blocks, plus the
// ideographic iteration mark 々 which sits just below the kana blocks and
// recurs in common words (e.g. the JP for "various", "daily", "individual").
export function tokenizeJapanese(raw: string): string[] {
  const tokens: string[] = [];
  const regex = /[々぀-ヿ㐀-䶿一-鿿]|[A-Za-z0-9]+/g;
  const lower = raw.toLowerCase();
  let match: RegExpExecArray | null;
  while ((match = regex.exec(lower)) !== null) {
    tokens.push(match[0]);
  }
  return tokens;
}

// Orama settings per translated locale, keyed by the codes in lib/i18n.ts.
// Every locale that is not plain Latin text needs an entry here, or search
// silently falls back to the English tokenizer.
export const searchLocaleMap = {
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
  // Traditional Chinese: same character-level strategy as zh, with its own
  // normalization cache so the two indexes never share state.
  "zh-Hant": {
    components: {
      tokenizer: {
        language: "english",
        normalizationCache: new Map(),
        tokenize: tokenizeCJK,
      },
    },
  },
} satisfies LocaleMap;
