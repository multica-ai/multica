/**
 * InlineCodeFixedExtension — overlay the upstream @tiptap/extension-code
 * input and paste rules so the character before the opening backtick is
 * preserved.
 *
 * Why this exists
 * ===============
 *
 * The upstream Code mark in @tiptap/extension-code@3.22.1 wires both an
 * inputRule and a pasteRule through `markInputRule` / `markPasteRule`.
 * Both helpers compute the deletion start with `fullMatch.search(/\S/)`,
 * which assumes the regex's leading group is whitespace. Code's regex
 * uses a NON-whitespace guard group `(^|[^`])`, so `search(/\S/)` returns
 * 0 and the helper deletes from the start of the full match — eating the
 * guard character along with the opening backtick.
 *
 * Concrete repro (issue #4728): typing or pasting `abcd\`123\`` produces
 * `abc<code>123</code>` instead of `abcd<code>123</code>`.
 *
 * Fix: extend Code, replace `addInputRules` AND `addPasteRules` with
 * handlers that use the regex's first capture group as an explicit
 * boundary instead of delegating to the broken upstream helpers. The
 * two-capture-group regex is unchanged so existing behaviour
 * (no triple-backtick collision, no nested backticks, no IME-composition
 * trigger) is preserved.
 *
 * IME / composition note
 * ----------------------
 * ProseMirror's inputRules plugin returns false while
 * `EditorView.composing` is true, which means CJK / dead-key composition
 * itself never fires inline-code formatting. This is correct behaviour
 * inherited unchanged from upstream; the fix only changes the offset
 * arithmetic.
 *
 * How to remove this overlay
 * ==========================
 *
 * If upstream `markInputRule` / `markPasteRule` ever land a fix — e.g.
 * by replacing `search(/\S/)` with `match[1]?.length ?? 0`, or by passing
 * the boundary length explicitly — the `[REGRESSION DOC]` tests in
 * inline-code-input.test.ts will start failing. To remove the overlay:
 *
 *   1) Delete this file and inline-code-input.test.ts.
 *   2) Drop the `code: false` line from StarterKit.configure in
 *      extensions/index.ts, restoring StarterKit's stock Code mark.
 *   3) Drop the `@tiptap/extension-code` direct dependency from
 *      packages/views/package.json if no other code references it.
 *   4) Re-run vitest to confirm no other test pinned the overlay name.
 */
import { InputRule, PasteRule } from "@tiptap/core";
import { Code } from "@tiptap/extension-code";

/**
 * Mirrors the upstream regex shape. Kept inline (not re-exported from
 * `@tiptap/extension-code`) so the fix is self-contained — a future upstream
 * regex change won't silently alter this overlay's behaviour.
 *
 * - `(^|[^`])` — the boundary group: either start-of-block or a single
 *   non-backtick character. NOT consumed by the resulting code mark.
 * - `([^`]+)` — the code content, one or more non-backtick characters.
 * - `(?!`)` — must not be followed by a third backtick, so a fenced code
 *   block opener doesn't trigger the inline rule.
 */
const INLINE_CODE_INPUT_REGEX = /(^|[^`])`([^`]+)`(?!`)$/;

/**
 * Same shape as INLINE_CODE_INPUT_REGEX but global and without the `$`
 * anchor — pasteRules scan the whole pasted slice and need to match every
 * occurrence rather than only the suffix.
 */
const INLINE_CODE_PASTE_REGEX = /(^|[^`])`([^`]+)`(?!`)/g;

/**
 * Shared offset-correct mutation: take the `(boundary, codeText)` capture
 * shape and apply (delete close `, delete open `, addMark) to `tr` against
 * the original document `range.from`. Returns nothing — callers don't
 * need to know whether the rule fired.
 */
function applyInlineCodeMark(
  tr: import("@tiptap/pm/state").Transaction,
  rangeFrom: number,
  rangeTo: number,
  boundary: string,
  codeTextLength: number,
  markType: import("@tiptap/pm/model").MarkType,
) {
  // All four anchors are computed against the ORIGINAL document. We mutate
  // from the tail forward so earlier positions stay valid for later
  // operations.
  const openBacktick = rangeFrom + boundary.length;
  const codeFrom = openBacktick + 1;
  const codeTo = codeFrom + codeTextLength;
  const closeBacktickEnd = codeTo + 1;

  if (codeTo < rangeTo) {
    tr.delete(codeTo, Math.max(rangeTo, closeBacktickEnd));
  }
  tr.delete(openBacktick, codeFrom);

  const markFrom = openBacktick;
  const markTo = markFrom + codeTextLength;
  tr.addMark(markFrom, markTo, markType.create());
}

export const InlineCodeFixedExtension = Code.extend({
  addInputRules() {
    return [
      new InputRule({
        find: INLINE_CODE_INPUT_REGEX,
        handler: ({ state, range, match }) => {
          const boundary = match[1] ?? "";
          const codeText = match[2];
          if (!codeText) {
            return null;
          }
          applyInlineCodeMark(
            state.tr,
            range.from,
            range.to,
            boundary,
            codeText.length,
            this.type,
          );
          // Stop stored marks so the next character the user types isn't
          // also formatted as code.
          state.tr.removeStoredMark(this.type);
          return;
        },
      }),
    ];
  },

  addPasteRules() {
    return [
      new PasteRule({
        find: INLINE_CODE_PASTE_REGEX,
        handler: ({ state, range, match }) => {
          const boundary = match[1] ?? "";
          const codeText = match[2];
          if (!codeText) {
            return null;
          }
          // PasteRule scans the entire pasted slice. `range.from` is the
          // document position of THIS match's start, so the same offset
          // arithmetic the input rule uses applies unchanged.
          // `removeStoredMark` is intentionally NOT called here: a paste
          // does not leave a typing cursor at the end of the code span,
          // so storedMarks is irrelevant on the paste path.
          applyInlineCodeMark(
            state.tr,
            range.from,
            range.to,
            boundary,
            codeText.length,
            this.type,
          );
          return;
        },
      }),
    ];
  },
});
