/**
 * Per-issue, session-scoped comment navigation intent + controlled root
 * expansion state (RUYI-28).
 *
 * Why a Zustand store (not props / route params):
 *   - The comments-directory modal route and the issue timeline live on
 *     different screens; Expo Router routes can't share state with the
 *     screen that opened them (same constraint documented for the
 *     new-issue draft flow). A tiny store is the established pattern
 *     (`reply-target-store`, `chat-session-picker-store`).
 *   - The timeline's root-collapse state must survive the FlashList's cell
 *     recycling (rows unmount when scrolled out of view) — per-row
 *     `useState` resets on recycle. Lifting to a per-issue set keeps the
 *     state stable while a row is off-screen.
 *
 * Non-persistent by design (approved scope: session-only state). No
 * `persist` middleware — leaving and re-entering the issue starts every
 * root collapsed again.
 *
 * Approved-scope guardrails encoded here:
 *   - This store never touches newCount / unread divider / last-viewed /
 *     sort / drafts. It owns ONLY {focus intent, focus status,
 *     expandedRoots}.
 *   - Focus carries a monotonic per-request nonce so re-selecting the
 *     same root re-triggers the locate effect (identical values would
 *     short-circuit a plain useEffect dep compare — same reason
 *     `highlightNonce` exists for the inbox deep-link).
 *   - The locate status is published here so the MODAL (a different
 *     screen) can observe it: `pending` keeps the modal open, `located`
 *     closes it, `failed` shows an inline error + retry. The timeline's
 *     controller is the only writer.
 */
import { create } from "zustand";

export interface CommentFocusIntent {
  issueId: string;
  /** Root comment id the timeline should expand + bring into view. */
  rootId: string;
  /** Bumped on every requestFocus call, even for the same rootId. */
  nonce: number;
}

/** Published by the timeline's locate controller for the modal to observe.
 *  - `pending`: scroll in flight — the modal stays open.
 *  - `located`: viewability-confirmed on screen — the modal closes.
 *  - `failed`: bounded attempts exhausted — the modal stays open with an
 *    inline error + Retry (which re-requests with a fresh nonce). */
export type CommentFocusStatus =
  | { phase: "pending"; nonce: number }
  | { phase: "located"; nonce: number }
  | {
      phase: "failed";
      nonce: number;
      reason: "not-found" | "layout" | "scroll" | "timeout";
    };

interface CommentFocusState {
  focus: CommentFocusIntent | null;
  /** Status of the CURRENT focus intent (or the last one, if it failed).
   *  Null before the first request. */
  status: CommentFocusStatus | null;
  /** issueId → set of root comment ids explicitly expanded this session. */
  expandedRoots: Record<string, Set<string>>;
  /** Request the timeline to expand + bounded-locate the given root.
   *  Resets the published status to `pending`. */
  requestFocus: (issueId: string, rootId: string) => void;
  /** Timeline controller callback — publish a terminal or in-flight
   *  status for the current intent. Stale nonces are ignored. */
  setStatus: (status: CommentFocusStatus) => void;
  /** Clear the navigation intent (expansion state is kept). */
  clearFocus: () => void;
  /** Mark one root expanded for an issue. */
  expandRoot: (issueId: string, rootId: string) => void;
  /** Mark one root collapsed for an issue. */
  collapseRoot: (issueId: string, rootId: string) => void;
  /** Wipe intent + expansion for one issue (timeline unmount hook). */
  resetIssue: (issueId: string) => void;
  /** Test/dev helper — full reset. */
  clearAll: () => void;
}

// Boxed so the test reset helper can zero it; still module-monotonic in
// production use (one counter for the whole session).
const counter = { n: 0 };

export const useCommentFocusStore = create<CommentFocusState>((set) => ({
  focus: null,
  status: null,
  expandedRoots: {},
  requestFocus: (issueId, rootId) => {
    counter.n += 1;
    const nonce = counter.n;
    set({
      focus: { issueId, rootId, nonce },
      status: { phase: "pending", nonce },
    });
  },
  setStatus: (status) =>
    set((s) => {
      // A status for an intent that was already replaced (user re-tapped)
      // must not overwrite the newer request's state.
      if (!s.focus || s.focus.nonce !== status.nonce) return s;
      return { status };
    }),
  clearFocus: () => set({ focus: null }),
  expandRoot: (issueId, rootId) =>
    set((s) => {
      const cur = s.expandedRoots[issueId];
      // Idempotent: repeated writes of the same root (deep-link rows
      // remounting inside the 5s highlight window, forceExpanded effects
      // re-running on recycled cells) must not allocate a fresh Set — the
      // previous always-new-Set shape re-notified every subscriber and
      // churned GC under rapid remounts.
      if (cur?.has(rootId)) return s;
      const next = new Set(cur);
      next.add(rootId);
      return { expandedRoots: { ...s.expandedRoots, [issueId]: next } };
    }),
  collapseRoot: (issueId, rootId) =>
    set((s) => {
      const cur = s.expandedRoots[issueId];
      // Same idempotence contract as expandRoot: collapsing a root that
      // isn't tracked (or belongs to an issue with no bucket yet) leaves
      // the record untouched.
      if (!cur || !cur.has(rootId)) return s;
      const next = new Set(cur);
      next.delete(rootId);
      return { expandedRoots: { ...s.expandedRoots, [issueId]: next } };
    }),
  resetIssue: (issueId) =>
    set((s) => {
      const expandedRoots = { ...s.expandedRoots };
      delete expandedRoots[issueId];
      const focusBelongsToIssue = s.focus && s.focus.issueId === issueId;
      return {
        expandedRoots,
        focus: focusBelongsToIssue ? null : s.focus,
        status: focusBelongsToIssue ? null : s.status,
      };
    }),
  clearAll: () =>
    set({ focus: null, status: null, expandedRoots: {} }),
}));

/** Test-only: also zero the module nonce counter so expected nonces stay
 *  deterministic across tests. */
export function resetCommentFocusStoreForTests(): void {
  counter.n = 0;
  useCommentFocusStore.getState().clearAll();
}
