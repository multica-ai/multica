import { indexCommentText } from "../../issues/components/comment-annotation-selection";

/**
 * Where a discussion sits in a doc: the quoted text and some context on each
 * side, in the rendered text of the editor (block boundaries as newlines).
 * The anchor lives beside the Markdown, never in it, so agents read a clean
 * doc; after every save it is found again by text.
 */
export interface DocAnchor {
  quote: string;
  prefix: string;
  suffix: string;
}

const CONTEXT = 64;

/** The anchor for the current selection inside `root`, or null. */
export function captureDocAnchor(root: HTMLElement, selection: Selection | null): DocAnchor | null {
  if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
  const range = selection.getRangeAt(0);
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
  const { text, nodes } = indexCommentText(root, true);
  const selected = nodes.filter(({ node }) => range.intersectsNode(node));
  const first = selected[0];
  const last = selected.at(-1);
  if (!first || !last) return null;
  const start = first.start + (range.startContainer === first.node ? range.startOffset : 0);
  const end = last.start + (range.endContainer === last.node ? range.endOffset : last.node.length);
  const quote = text.slice(start, end);
  if (!quote.trim()) return null;
  return {
    quote,
    prefix: text.slice(Math.max(0, start - CONTEXT), start),
    suffix: text.slice(end, end + CONTEXT),
  };
}

/**
 * Where `anchor` is in `text`, or null when its quote is gone. An occurrence
 * whose context still matches wins; otherwise the only occurrence; otherwise
 * the occurrence whose surrounding text agrees most with the saved context.
 */
export function locateDocAnchor(text: string, anchor: DocAnchor): number | null {
  if (!anchor.quote) return null;
  const starts: number[] = [];
  for (let at = text.indexOf(anchor.quote); at !== -1; at = text.indexOf(anchor.quote, at + 1)) {
    starts.push(at);
  }
  if (starts.length <= 1) return starts[0] ?? null;
  let best = starts[0]!;
  let bestScore = -1;
  for (const start of starts) {
    const before = text.slice(Math.max(0, start - anchor.prefix.length), start);
    const after = text.slice(start + anchor.quote.length, start + anchor.quote.length + anchor.suffix.length);
    const score = sharedSuffix(before, anchor.prefix) + sharedPrefix(after, anchor.suffix);
    if (score > bestScore) {
      best = start;
      bestScore = score;
    }
  }
  return best;
}

function sharedSuffix(a: string, b: string): number {
  let n = 0;
  while (n < a.length && n < b.length && a[a.length - 1 - n] === b[b.length - 1 - n]) n++;
  return n;
}

function sharedPrefix(a: string, b: string): number {
  let n = 0;
  while (n < a.length && n < b.length && a[n] === b[n]) n++;
  return n;
}

/** A DOM range over `anchor` in `root`, or null when the quote is gone. */
export function docAnchorRange(root: HTMLElement, anchor: DocAnchor): Range | null {
  const { text, nodes } = indexCommentText(root, true);
  const start = locateDocAnchor(text, anchor);
  if (start === null) return null;
  const end = start + anchor.quote.length;
  const first = nodes.find((n) => n.start <= start && n.end > start);
  const last = nodes.find((n) => n.start < end && n.end >= end);
  if (!first || !last) return null;
  const range = document.createRange();
  range.setStart(first.node, start - first.start);
  range.setEnd(last.node, end - last.start);
  return range;
}
