/**
 * Close-decision math for the comments-directory modal (RUYI-28).
 *
 * The modal auto-closes (`router.back()`) only when the timeline confirms
 * the target row on screen (`located`) for an intent minted AFTER this
 * modal mount. Review round 1 found the previous render-time
 * `mountedNonceRef` snapshot captured the user's FIRST selection too: in
 * a fresh session the snapshot condition was still armed when the first
 * `requestFocus` re-render ran, so located(1) hit `1 > 1` and the modal
 * never closed. The fix is structural: the component captures the
 * pre-mount nonce ONCE via a `useState` initializer reading the store
 * imperatively (React runs that initializer exactly once per mount, and
 * it cannot observe post-mount selections), then defers every close
 * decision to the two pure functions below — which this Node-only vitest
 * lane can exercise directly.
 */
import type {
  CommentFocusIntent,
  CommentFocusStatus,
} from "@/data/stores/comment-focus-store";

/**
 * Nonce that already existed in the focus store BEFORE this modal mount
 * (same issue only), or -1 when nothing was pending for it. Capturing a
 * pre-existing nonce stops a re-opened modal from replaying the previous
 * visit's already-`located` intent into an unrequested `router.back()`.
 */
export function preMountNonceOf(
  focusAtMount: CommentFocusIntent | null,
  issueId: string,
): number {
  return focusAtMount &&
    focusAtMount.issueId === issueId &&
    focusAtMount.nonce > 0
    ? focusAtMount.nonce
    : -1;
}

/**
 * Should this modal mount auto-close for the given store state? True only
 * for a `located` status that (a) belongs to the CURRENT intent, (b) is
 * for this issue, and (c) carries a nonce minted after this mount began
 * (`> preMountNonce`) — i.e. the user actually asked THIS visit to jump.
 */
export function shouldAutoCloseOnLocated(
  focus: CommentFocusIntent | null,
  status: CommentFocusStatus | null,
  issueId: string,
  preMountNonce: number,
): boolean {
  if (!focus || focus.issueId !== issueId || !status) return false;
  if (status.nonce !== focus.nonce) return false;
  return status.phase === "located" && status.nonce > preMountNonce;
}
