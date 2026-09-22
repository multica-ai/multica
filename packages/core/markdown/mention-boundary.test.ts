import { describe, expect, it } from "vitest";
import { isMentionBoundaryAfter } from "./mention-boundary";

// The character-level matrix for the shared mention boundary rule. The wiring —
// that the editor and the mobile composer actually consult it — is covered
// where each of them lives (mention-boundary.test.ts under packages/views and
// mention-serialize.test.ts under apps/mobile).

/** The `@` is placed directly after this text, so its last code point decides. */
const OPENS: Array<[string, string]> = [
  ["nothing at all", ""],
  ["a half-width space", "hello "],
  // A Chinese IME inserts U+3000 for the space key.
  ["a full-width space", "你好　"],
  ["a tab", "hello\t"],
  ["a newline", "hello\n"],
  ["punctuation", "hello("],
  ["a CJK word with no separator", "你好"],
  ["a hiragana word", "こんにちは"],
  ["a katakana word", "テレビ"],
  // U+30FC carries Katakana only as a Script_Extensions value, so it needs the
  // explicit listing in SPACELESS_SCRIPT to count as part of the word.
  ["a katakana word ending in the prolonged sound mark", "コーヒー"],
  ["half-width katakana", "ｺｰﾋｰ"],
  ["a hangul word", "안녕하세요"],
  ["a thai word", "สวัสดี"],
  ["a lao word", "ສະບາຍດີ"],
  ["a khmer word", "ជំរាបសួរ"],
  ["a myanmar word", "မင်္ဂလာပါ"],
  ["a tibetan word", "བཀྲ་ཤིས"],
  ["an emoji", "🎉"],
];

const SHUT: Array<[string, string]> = [
  ["an ASCII word", "hello"],
  ["an ASCII capital", "Hello"],
  ["a digit", "2024"],
  ["an underscore", "snake_case"],
  ["an accented latin word", "café"],
  ["a spanish word", "josé"],
  ["a cyrillic word", "почта"],
  ["a greek word", "αλφα"],
  ["a vietnamese word", "chà"],
  ["a dotted domain", "user@example.com"],
  ["a full ASCII address", "first.last@example.co.uk"],
];

describe("isMentionBoundaryAfter", () => {
  it.each(OPENS)("opens after %s", (_name, before) => {
    expect(isMentionBoundaryAfter(before)).toBe(true);
  });

  it.each(SHUT)("stays shut after %s", (_name, before) => {
    expect(isMentionBoundaryAfter(before)).toBe(false);
  });

  it("reads only the last code point", () => {
    // The caller may hand over a two-unit tail; earlier characters are noise.
    expect(isMentionBoundaryAfter("café")).toBe(false);
    expect(isMentionBoundaryAfter("é")).toBe(false);
  });

  it("reads a code point outside the BMP whole", () => {
    // Deseret is a letter and not a spaceless script, so it stays shut — but
    // only if the two units arrive together. Read one unit at a time the
    // implementation would see a lone surrogate, fail both tests, and open.
    const astral = "𐐀";
    expect(astral.length).toBe(2);
    expect(isMentionBoundaryAfter(astral)).toBe(false);
  });
});
