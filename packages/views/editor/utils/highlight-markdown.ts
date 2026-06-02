/**
 * highlight-markdown — read-only `==text==` → `<mark>text</mark>` transform.
 *
 * The editor (Tiptap) parses `==text==` natively via the HighlightExtension's
 * markdownTokenizer. The read-only surface uses react-markdown, which has no
 * notion of `==` highlight syntax, so we lower it to a raw `<mark>` element here.
 * readonly-content.tsx already runs `rehype-raw` (so the raw `<mark>` becomes a
 * real element) and whitelists `mark` in its sanitize schema. Because the inner
 * text is left untouched, nested Markdown inside a highlight (e.g. `==**bold**==`)
 * is still parsed by remark — matching the editor, which keeps inner formatting
 * via inlineTokens.
 *
 * The match rules mirror HighlightExtension's HIGHLIGHT_TOKEN_RE so the two
 * renderers agree on what is a highlight:
 *   - no whitespace directly inside the fences (`==x==` highlights, `== x ==` not)
 *   - non-empty content (`====` stays literal)
 *
 * `==` inside code (fenced blocks, inline code) and math (`$…$`, `$$…$$`) is left
 * untouched — those are literal contexts where `==` must not become a highlight.
 */

const HIGHLIGHT_RE = /==(?!\s)([\s\S]*?[^\s])==/g;

interface Range {
  start: number;
  end: number;
}

/**
 * Spans where `==` must NOT be interpreted as highlight syntax: fenced code,
 * inline code, and inline/display math. Mirrors the code-range scan in
 * @multica/ui/markdown's linkify so highlight and autolink skip the same
 * literal contexts.
 */
function findLiteralRanges(text: string): Range[] {
  const ranges: Range[] = [];
  const add = (re: RegExp) => {
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      const start = m.index;
      if (ranges.some((r) => start >= r.start && start < r.end)) continue;
      ranges.push({ start, end: start + m[0].length });
    }
  };
  add(/```[\s\S]*?```/g); // fenced code
  add(/\$\$[\s\S]*?\$\$/g); // display math
  add(/(?<!\$)\$(?!\$)[^$\n]+\$(?!\$)/g); // inline math
  add(/(?<!`)`(?!`)[^`\n]+`(?!`)/g); // inline code
  return ranges;
}

function isInside(pos: number, ranges: Range[]): boolean {
  return ranges.some((r) => pos >= r.start && pos < r.end);
}

/**
 * Lower `==text==` highlight syntax to raw `<mark>` for the react-markdown
 * read-only pipeline. No-op when the text contains no `==`.
 */
export function highlightToHtml(markdown: string): string {
  if (!markdown.includes("==")) return markdown;
  const literalRanges = findLiteralRanges(markdown);

  let result = "";
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  HIGHLIGHT_RE.lastIndex = 0;
  while ((match = HIGHLIGHT_RE.exec(markdown)) !== null) {
    if (isInside(match.index, literalRanges)) continue;
    result += markdown.slice(lastIndex, match.index);
    result += `<mark>${match[1]}</mark>`;
    lastIndex = match.index + match[0].length;
  }
  result += markdown.slice(lastIndex);
  return result;
}
