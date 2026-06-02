import Highlight from "@tiptap/extension-highlight";

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
 * BOUNDARY RULES — must stay in sync with the read-only renderer's regex in
 * utils/highlight-markdown.ts so the editor and the read-only view agree on what
 * counts as a highlight:
 *   - no whitespace directly inside the fences (`==x==` highlights, `== x ==` does not)
 *   - non-empty content (`====` stays literal text)
 */
const HIGHLIGHT_TOKEN_RE = /^==(?!\s)([\s\S]*?[^\s])==/;

export const HighlightExtension = Highlight.extend({
  markdownTokenizer: {
    name: "highlight",
    level: "inline" as const,
    start(src: string) {
      return src.indexOf("==");
    },
    tokenize(src: string, _tokens: unknown, helpers: any) {
      const match = HIGHLIGHT_TOKEN_RE.exec(src);
      if (!match) return undefined;
      return {
        type: "highlight",
        raw: match[0],
        tokens: helpers.inlineTokens(match[1]),
      };
    },
  },

  parseMarkdown: (token: any, helpers: any) =>
    helpers.applyMark("highlight", helpers.parseInline(token.tokens)),

  renderMarkdown: (_node: any, helpers: any) => `==${helpers.renderChildren()}==`,
}).configure({ multicolor: false });
