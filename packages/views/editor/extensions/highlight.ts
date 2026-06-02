import Highlight from "@tiptap/extension-highlight";
import { matchHighlightAt } from "../utils/highlight-match";

/**
 * HighlightExtension — text highlight mark (`==text==` ⇄ <mark>).
 *
 * Builds on @tiptap/extension-highlight, which already supplies the `<mark>`
 * parseHTML/renderHTML, the `==text==` input/paste rules, the
 * setHighlight/toggleHighlight/unsetHighlight commands, and the Mod-Shift-H
 * shortcut. On top of that we add @tiptap/markdown serialization so highlights
 * round-trip through the stored Markdown as `==text==`:
 *
 *   - renderMarkdown: highlight mark → `==…==`. @tiptap/markdown renders marks by
 *     calling renderMarkdown with a placeholder child and splitting the result
 *     into opening/closing fences, so wrapping the placeholder in `==` yields a
 *     `==` open and `==` close.
 *   - markdownTokenizer + parseMarkdown: `==text==` in stored Markdown → highlight
 *     mark. inlineTokens keeps inner inline formatting (e.g. `==**bold**==`).
 *
 * Single colour (yellow) for now — `multicolor` stays off. A future multicolour
 * variant would need a syntax that can carry a colour (`==text==` cannot), so it
 * is intentionally out of scope here (see MUL-2934).
 *
 * BOUNDARY RULES live in utils/highlight-match.ts and are shared with the
 * read-only renderer (utils/highlight-markdown.ts) so the editor and the
 * read-only view can never disagree on what counts as a highlight:
 *   - no whitespace directly inside the fences (`==x==` highlights, `== x ==` not)
 *   - non-empty content (`====` stays literal text)
 *   - neither fence may sit inside code/math (a `==` inside `` `code` `` / `$math$`
 *     is literal), so ``==a `b==c` d==`` highlights the whole span, not `a `b`
 *   - a highlight may not cross a blank line / block boundary
 */

export const HighlightExtension = Highlight.extend({
  markdownTokenizer: {
    name: "highlight",
    level: "inline" as const,
    start(src: string) {
      return src.indexOf("==");
    },
    tokenize(src: string, _tokens: unknown, helpers: any) {
      const match = matchHighlightAt(src, 0);
      if (!match) return undefined;
      return {
        type: "highlight",
        raw: src.slice(0, match.end),
        tokens: helpers.inlineTokens(match.inner),
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) =>
    helpers.applyMark("highlight", helpers.parseInline(token.tokens)),

  renderMarkdown: (_node: any, helpers: any) => `==${helpers.renderChildren()}==`,
}).configure({ multicolor: false });
