/**
 * Screen-scoped state for "compose a reply to comment X" — read by the
 * inline comment composer, written by the comment long-press action sheet
 * ("Reply").
 *
 * Why a global Zustand store and not props:
 *   - The trigger (long-press action sheet) and the consumer (composer at
 *     the bottom of the screen) live in different component trees. Threading
 *     a callback through TimelineList → CommentCard → useCommentLongPress
 *     would mean three layers of prop drilling for one boolean signal.
 *   - Only one reply can be in-flight at a time per issue detail screen,
 *     so a singleton store is the right shape.
 *
 * Lifecycle: the issue-detail screen clears the store on unmount (same
 * pattern as `comment-select-store.ts`). Each fresh navigation into an
 * issue starts with no reply target, so a stale target can't leak across
 * issues.
 */
import { create } from "zustand";

export interface ReplyTarget {
  commentId: string;
  /** Display name for the "Replying to X" chip above the input. Resolved
   *  at trigger time via `useActorLookup().getName(actor_type, actor_id)`
   *  so the chip doesn't have to do its own actor lookup. */
  actorName: string;
  /** Raw markdown of the parent comment. The chip renders it via
   *  `stripMarkdown()` + `numberOfLines={2}` so the user sees what they're
   *  replying to without leaving the keyboard. May be empty string for
   *  attachment-only comments. */
  preview: string;
}

interface State {
  target: ReplyTarget | null;
  setTarget: (target: ReplyTarget) => void;
  clear: () => void;
}

export const useReplyTargetStore = create<State>((set) => ({
  target: null,
  setTarget: (target) => set({ target }),
  clear: () => set({ target: null }),
}));
