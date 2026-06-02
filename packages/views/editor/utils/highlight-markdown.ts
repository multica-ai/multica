/**
 * highlight-markdown — read-only `==text==` → `<mark>text</mark>` transform.
 *
 * The editor (Tiptap) parses `==text==` natively via the HighlightExtension's
 * markdownTokenizer. The read-only surface uses react-markdown, which has no
 * notion of `==` highlight syntax, so we lower it to a raw `<mark>` element here.
 * readonly-content.tsx already runs `rehype-raw` (so the raw `<mark>` becomes a
 * real element) and whitelists `mark` in its sanitize schema. Because the inner
 * text is left untouched, nested Markdown inside a highlight (e.g. `==**bold**==`
 * or an inline `` `code` ``) is still parsed by remark — matching the editor.
 *
 * The matching rules live in highlight-match.ts and are shared with the editor
 * tokenizer, so the storage ↔ editor ↔ read-only boundary can never drift:
 * fences may not sit inside code/math, and a highlight may not cross a blank
 * line / block boundary.
 */

import { findLiteralRanges, matchHighlightAt } from "./highlight-match";

/**
 * Lower `==text==` highlight syntax to raw `<mark>` for the react-markdown
 * read-only pipeline. No-op when the text contains no `==`.
 */
export function highlightToHtml(markdown: string): string {
  if (!markdown.includes("==")) return markdown;
  const ranges = findLiteralRanges(markdown);

  let result = "";
  let cursor = 0;
  let i = markdown.indexOf("==");
  while (i !== -1) {
    const match = matchHighlightAt(markdown, i, ranges);
    if (match) {
      result += markdown.slice(cursor, i);
      result += `<mark>${match.inner}</mark>`;
      cursor = match.end;
      i = markdown.indexOf("==", match.end);
    } else {
      // Not a highlight here — advance past this `==` and keep looking.
      i = markdown.indexOf("==", i + 2);
    }
  }
  result += markdown.slice(cursor);
  return result;
}
