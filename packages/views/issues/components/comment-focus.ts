/**
 * Resolve an arbitrary comment to the root row owned by the virtualized issue
 * timeline. Replies render inside their root row and therefore never have a
 * Virtuoso index of their own.
 */
export function resolveCommentFocusTarget(
  commentId: string,
  itemIds: readonly string[],
  replyToRoot: ReadonlyMap<string, string>,
): { rootId: string; index: number } | null {
  const rootId = replyToRoot.get(commentId) ?? commentId;
  const index = itemIds.indexOf(rootId);
  return index < 0 ? null : { rootId, index };
}

/**
 * The public comment id belongs to the virtualized thread wrapper, which can
 * be much taller than the viewport because it also contains every reply. Use
 * the comment's own header as the visual landing point when it is mounted.
 */
export function findCommentFocusAnchor(
  wrapper: HTMLElement,
  commentId: string,
): HTMLElement {
  const anchors = wrapper.querySelectorAll<HTMLElement>(
    "[data-comment-focus-anchor]",
  );
  return (
    Array.from(anchors).find(
      (anchor) => anchor.dataset.commentFocusAnchor === commentId,
    ) ?? wrapper
  );
}
