import { captureAnnotationSelection, findAnnotationSource as findSource } from "../../common/reply-annotation-selection";
export { annotationRange } from "../../common/reply-annotation-selection";

export function captureCommentSelection(card: HTMLElement, selection: Selection | null, editable = false) {
  return captureAnnotationSelection(card, selection, {
    contentAttribute: "data-comment-content", scopeAttribute: "data-annotation-thread", editable,
  });
}

export function findAnnotationSource(card: HTMLElement, sourceCommentId: string) {
  return findSource(card, sourceCommentId, "data-comment-content", "data-annotation-thread");
}
