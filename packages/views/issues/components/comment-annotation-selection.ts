import { locateReplyAnnotation, type ReplyAnnotation } from "@multica/core/drafts/reply-annotation";

const EXCLUDED = 'button, input, textarea, [aria-hidden="true"], [hidden], script, style';
const BLOCKS = new Set(["P", "DIV", "LI", "PRE", "BLOCKQUOTE", "H1", "H2", "H3", "H4", "H5", "H6", "TR"]);

/** A deterministic visible-text index shared by capture and source navigation.
 * Block boundaries and code newlines survive; renderer controls are excluded.
 */
export function indexCommentText(root: HTMLElement, editable = false) {
  let text = "";
  const nodes: { node: Text; start: number; end: number }[] = [];
  const newline = () => { if (text && !text.endsWith("\n")) text += "\n"; };
  function visit(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) {
      const value = node.textContent ?? "";
      nodes.push({ node: node as Text, start: text.length, end: text.length + value.length });
      text += value;
    } else if (node instanceof HTMLElement) {
      if (node.matches(EXCLUDED) || (!editable && node.matches('[contenteditable="true"]'))) return;
      if (node.tagName === "BR") { text += "\n"; return; }
      const block = BLOCKS.has(node.tagName);
      if (block) newline();
      node.childNodes.forEach(visit);
      if (block) newline();
    }
  }
  root.childNodes.forEach(visit);
  return { text, nodes };
}

function contentRoot(node: Node): HTMLElement | null {
  return (node instanceof Element ? node : node.parentElement)?.closest<HTMLElement>("[data-comment-content]") ?? null;
}

export function captureCommentSelection(card: HTMLElement, selection: Selection | null, editable = false) {
  if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
  const range = selection.getRangeAt(0);
  const root = contentRoot(range.startContainer);
  if (!root || root !== contentRoot(range.endContainer) || !card.contains(root) ||
    root.closest("[data-annotation-thread]") !== card) return null;
  const { text, nodes } = indexCommentText(root, editable);
  const selected = nodes.filter(({ node }) => range.intersectsNode(node));
  const first = selected[0];
  const last = selected.at(-1);
  if (!first || !last) return null;
  const start = first.start + (range.startContainer === first.node ? range.startOffset : 0);
  const end = last.start + (range.endContainer === last.node ? range.endOffset : last.node.length);
  if (!text.slice(start, end).trim()) return null;
  return {
    sourceCommentId: root.dataset.commentContent!,
    quote: text.slice(start, end), start,
    prefix: text.slice(Math.max(0, start - 32), start),
    suffix: text.slice(end, end + 32),
    range: range.cloneRange(), root,
  };
}

export function annotationRange(root: HTMLElement, annotation: ReplyAnnotation, editable = false): Range | null {
  const { text, nodes } = indexCommentText(root, editable);
  const start = locateReplyAnnotation(text, annotation);
  if (start === null) return null;
  const end = start + annotation.quote.length;
  const first = nodes.find((n) => n.start <= start && n.end > start);
  const last = nodes.find((n) => n.start < end && n.end >= end);
  if (!first || !last) return null;
  const range = document.createRange();
  range.setStart(first.node, start - first.start);
  range.setEnd(last.node, end - last.start);
  return range;
}

export function findAnnotationSource(card: HTMLElement, sourceCommentId: string): HTMLElement | undefined {
  return Array.from(card.querySelectorAll<HTMLElement>("[data-comment-content]"))
    .find((node) => node.dataset.commentContent === sourceCommentId && node.closest("[data-annotation-thread]") === card);
}
