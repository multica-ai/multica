/**
 * Per-comment text-selection mode. When the user taps "Select Text" in the
 * comment's long-press action sheet (`components/issue/comment-context-
 * menu.tsx` → `useCommentLongPress`), the targeted comment id is parked
 * here. `CommentBody` reads this store and, when matched, (a) drops the
 * `Pressable.onLongPress` handler so the long-press gesture no longer
 * races with text-selection, and (b) sets `selectable={true}` on the
 * Markdown body — the next long-press inside the bubble fires UIKit's
 * native selection magnifier + handles + Copy/Look Up callout.
 *
 * Why a separate Zustand store (not props / context):
 *   - Only one comment can be in selection mode at a time across the app —
 *     selecting comment B implicitly clears comment A by id replacement.
 *   - The flip happens from inside the context-menu callback, which lives
 *     in a different component tree than the comment list — easier to wire
 *     via a global store than to thread callbacks through every parent.
 *
 * Lifecycle: cleared when (a) the user taps the floating Done pill,
 * (b) the timeline scrolls, or (c) the issue-detail screen unmounts —
 * so each fresh navigation into an issue starts with no comment selected.
 */
import { create } from "zustand";

interface State {
  selectingId: string | null;
  setSelecting: (commentId: string) => void;
  clear: () => void;
}

export const useCommentSelectStore = create<State>((set) => ({
  selectingId: null,
  setSelecting: (commentId) => set({ selectingId: commentId }),
  clear: () => set({ selectingId: null }),
}));
