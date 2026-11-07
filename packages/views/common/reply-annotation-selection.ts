import { locateReplyAnnotation, type ReplyAnnotation } from "@multica/core/drafts/reply-annotation";

import { captureTextSelection, indexRenderedText } from "./text-selection";

export function captureAnnotationSelection(card: HTMLElement, selection: Selection | null, options: { contentAttribute: string; scopeAttribute: string; editable?: boolean }) {
  const captured = captureTextSelection(card, selection, options);
  if (!captured) return null;
  const { sourceId, ...rest } = captured;
  return { ...rest, sourceCommentId: sourceId };
}

export function annotationRange(root: HTMLElement, annotation: ReplyAnnotation, editable = false): Range | null {
  const { text, nodes } = indexRenderedText(root, editable);
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

export function findAnnotationSource(card: HTMLElement, sourceCommentId: string, contentAttribute: string, scopeAttribute: string): HTMLElement | undefined {
  return Array.from(card.querySelectorAll<HTMLElement>(`[${contentAttribute}]`))
    .find((node) => node.getAttribute(contentAttribute) === sourceCommentId && node.closest(`[${scopeAttribute}]`) === card);
}
