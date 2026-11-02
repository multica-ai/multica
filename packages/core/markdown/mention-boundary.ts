/**
 * Where an `@` starts a mention token rather than continuing the word before it.
 *
 * Single source of truth for both composers: the web/desktop editor
 * (packages/views/editor/extensions/mention-suggestion.tsx) and the mobile
 * comment composer (apps/mobile/lib/mention-serialize.ts). They have to agree —
 * the same text must offer the picker on every client.
 *
 * Tiptap's own rule is `allowedPrefixes`, defaulting to `[" "]`: a half-width
 * space and nothing else. That makes a mention unreachable in the two ways CJK
 * text is actually typed — with no separator at all, and after the full-width
 * space (U+3000) an IME inserts. The rule it means to protect is narrower than
 * "not a space": an `@` glued to the end of a word, as in `user@example.com`.
 * So the question is whether the character before the `@` is part of a word.
 *
 * Two things make that question non-obvious:
 *
 *   - Word characters are Unicode letters, digits, marks and `_`. An ASCII-only
 *     class makes every accented, Cyrillic or Greek letter look like a
 *     boundary, which re-opens the address case for `josé@example.com`,
 *     `почта@mail.ru` and `αλφα@example.com`.
 *   - Scripts written without spaces between words are the exception: there is
 *     no separator to type, so the `好` in `你好@Mi` is where the token starts.
 *
 * Known ambiguity, accepted rather than solved: `用户@example.com` cannot be
 * told apart from a mention typed with no separator, and opens the picker. No
 * character-level rule that keeps `你好@Mi` working can distinguish the two.
 *
 * Pure — no IO, no global state.
 */

/** Unicode word characters. An `@` glued to one of these continues a word. */
const WORD_CHARACTER = /[\p{L}\p{N}\p{M}_]/u;

/**
 * Scripts that do not separate words with spaces, where a word can therefore
 * end directly against the `@`. Latin, Cyrillic, Greek and friends are absent
 * on purpose: their writers type a space before `@`, and that absence is also
 * what keeps their email addresses from opening the picker.
 *
 * Written with `Script=`, not `Script_Extensions=`, because Hermes — the JS
 * engine the mobile app runs on — rejects `\p{Script_Extensions=Thai}` (also
 * Lao, Khmer and Tibetan) as an invalid property name and fails to parse the
 * module, which would take the app down with it. The escapes trailing the
 * scripts are the code points that carry one of these scripts only as an
 * extension *and* are word characters, so a `Script=` test alone would read
 * them as word continuations: U+3099/U+309A and U+FF9E/U+FF9F voiced sound
 * marks, and U+30FC/U+FF70 prolonged sound marks — the last character of
 * `コーヒー`, which would otherwise not open the picker.
 */
const SPACELESS_SCRIPT =
  /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Thai}\p{Script=Lao}\p{Script=Khmer}\p{Script=Myanmar}\p{Script=Tibetan}\u3099\u309A\u30FC\uFF70\uFF9E\uFF9F]/u;

/**
 * True when an `@` typed immediately after `before` starts a token.
 *
 * `before` is the text preceding the `@`; only its last code point is read, and
 * an empty string means the `@` opens the text. Callers pass at most two code
 * units so an astral-plane code point arrives whole.
 */
export function isMentionBoundaryAfter(before: string): boolean {
  const codePoints = [...before];
  const previous = codePoints[codePoints.length - 1];
  if (previous === undefined) return true;
  return SPACELESS_SCRIPT.test(previous) || !WORD_CHARACTER.test(previous);
}
