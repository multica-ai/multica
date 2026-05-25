/**
 * Per-message text-selection mode for the chat surface. When the user taps
 * "Select Text" in a chat bubble's long-press action sheet
 * (`components/chat/message-long-press.tsx` → `useChatMessageLongPress`),
 * the targeted message id is parked here. The bubble row reads this store
 * and, when matched, (a) drops the `Pressable.onLongPress` wrapper so the
 * long-press gesture no longer races with text-selection, and (b) sets
 * `selectable={true}` on the Markdown / Text body — the next long-press
 * inside the bubble fires UIKit's native selection magnifier + handles +
 * Copy/Look Up callout.
 *
 * Why a separate Zustand store (not props / context, and not reused from
 * the comment surface):
 *   - Only one message can be in selection mode at a time across the app —
 *     selecting message B implicitly clears message A by id replacement.
 *   - The flip happens from inside the context-menu callback, which lives
 *     in a different component tree than the chat list — easier to wire
 *     via a global store than to thread callbacks through every parent.
 *   - Chat and comment ids share no namespace constraint, and the timeline-
 *     list's scroll-clear handler reads the comment store specifically;
 *     reusing it would let a comment scroll wipe a chat selection (or
 *     vice versa) and let id collisions cross-trigger selection state.
 *
 * Lifecycle: cleared when (a) the user taps the floating Done pill,
 * (b) the chat list scrolls, or (c) the chat tab loses focus —
 * so each fresh navigation into chat starts with no message selected.
 */
import { create } from "zustand";

interface State {
  selectingId: string | null;
  setSelecting: (messageId: string) => void;
  clear: () => void;
}

export const useChatSelectStore = create<State>((set) => ({
  selectingId: null,
  setSelecting: (messageId) => set({ selectingId: messageId }),
  clear: () => set({ selectingId: null }),
}));
