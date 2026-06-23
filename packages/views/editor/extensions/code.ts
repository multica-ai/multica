import { markInputRule, markPasteRule } from "@tiptap/core";
import { Code } from "@tiptap/extension-code";

/**
 * Inline-code mark that fixes Tiptap's "eats the preceding character" bug.
 *
 * Tiptap's stock Code mark guards its input/paste rule with a *consuming*
 * leading group:
 *
 *     /(^|[^`])`([^`]+)`(?!`)$/
 *      └──┬──┘
 *      guard char — meant only to ensure the opening backtick isn't preceded
 *      by another backtick (so ```` ``` ```` stays a code block), but it is a
 *      real capturing group, so the char before the backtick lands in match[0].
 *
 * `markInputRule` / `markPasteRule` then delete the whole matched range from
 * `range.from + startSpaces`, where `startSpaces = fullMatch.search(/\S/)` only
 * skips *whitespace*. So:
 *
 *   - `foo ` + `` `bar` ``  → guard char is a space → startSpaces=1 → space kept ✓
 *   - `foo`  + `` `bar` ``  → guard char is `o`      → startSpaces=0 → `o` deleted ✗
 *
 * i.e. typing or pasting `` foo`bar` `` with no space swallows the `o`, turning
 * `foo` into `fo` + <code>bar</code>. (Reported on RAS-6.)
 *
 * Fix: replace the consuming `(^|[^`])` guard with a zero-width negative
 * lookbehind `(?<!`)`. It enforces the exact same "not preceded by a backtick"
 * constraint without putting the preceding character into the match, so nothing
 * before the opening backtick is ever deleted. Everything else (markdown
 * serialization, commands, Mod-e shortcut, `excludes: '_'`) is inherited from
 * the stock Code mark unchanged.
 *
 * Lookbehind is supported by every engine this editor runs in (Node ≥ 9,
 * Chromium/Electron, Firefox, Safari ≥ 16.4).
 */
export const inputRegex = /(?<!`)`([^`]+)`(?!`)$/;
export const pasteRegex = /(?<!`)`([^`]+)`(?!`)/g;

export const PatchedCode = Code.extend({
  addInputRules() {
    return [markInputRule({ find: inputRegex, type: this.type })];
  },

  addPasteRules() {
    return [markPasteRule({ find: pasteRegex, type: this.type })];
  },
});
