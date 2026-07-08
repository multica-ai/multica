/**
 * highlight-match — the single source of truth for what `==text==` highlight
 * spans look like. BOTH the editor tokenizer (extensions/highlight.ts) and the
 * read-only lowering (utils/highlight-markdown.ts) use this so the storage ↔
 * editor ↔ read-only boundary rules can never drift.
 *
 * Rules for a highlight `==INNER==`:
 *   - no whitespace directly inside the fences (`==x==` ok, `== x ==` not)
 *   - INNER is non-empty (`====` stays literal)
 *   - neither fence may sit inside a code/math literal span — a `==` inside
 *     inline code/math or a fenced/`$$` block is literal and must not open or
 *     close a highlight (so ``==a `b==c` d=="`` highlights the whole thing, the
 *     inner `==` stays code)
 *   - INNER may not cross a blank line / block boundary (`==a\n\nb==` is two
 *     literal paragraphs, matching how the editor lexes it)
 */

export interface Range {
  start: number;
  end: number;
}

/**
 * A blank line: a line break, optional spaces/tabs, then another line break.
 * Handles both LF and CRLF (Windows / pasted) line endings so the boundary
 * rule holds regardless of how the content was stored.
 */
const BLANK_LINE_RE = /\r?\n[ \t]*\r?\n/;

/**
 * Spans where `==` must NOT be interpreted as highlight syntax: fenced code,
 * inline code, and inline/display math. Mirrors the code-range scan in
 * @multica/ui/markdown's linkify so highlight and autolink skip the same
 * literal contexts. Earlier (block-level) ranges win so a `==`/backtick inside
 * a fenced block is not also counted as inline.
 */
export function findLiteralRanges(text: string): Range[] {
  const ranges: Range[] = [];
  const add = (re: RegExp) => {
    let m: RegExpExecArray | null;
    re.lastIndex = 0;
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
 * Try to match a highlight whose opening `==` begins at `i`. Returns the
 * exclusive end offset (just past the closing `==`) and the inner text, or
 * null if no valid highlight starts here.
 *
 * `ranges` may be passed in when the caller already computed literal ranges for
 * the whole text (read-only path); otherwise they are computed from `text`.
 */
export function matchHighlightAt(
  text: string,
  i: number,
  ranges?: Range[],
): { end: number; inner: string } | null {
  if (text[i] !== "=" || text[i + 1] !== "=") return null;
  const innerStart = i + 2;
  // Non-empty and no whitespace directly after the opening fence.
  if (innerStart >= text.length) return null;
  if (/\s/.test(text[innerStart]!)) return null;

  const r = ranges ?? findLiteralRanges(text);
  if (isInside(i, r)) return null; // opening fence is literal (inside code/math)

  // INNER may not cross a blank line: cap the scan at the first one.
  const blankRel = text.slice(innerStart).search(BLANK_LINE_RE);
  const scanLimit = blankRel === -1 ? text.length : innerStart + blankRel;

  for (let j = innerStart + 1; j <= scanLimit && j + 1 < text.length; j++) {
    if (text[j] !== "=" || text[j + 1] !== "=") continue;
    if (isInside(j, r)) continue; // closing fence is literal — keep scanning
    if (/\s/.test(text[j - 1]!)) continue; // no whitespace directly before fence
    return { end: j + 2, inner: text.slice(innerStart, j) };
  }
  return null;
}
