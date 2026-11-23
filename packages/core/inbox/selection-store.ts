"use client";

import { create } from "zustand";
import type { HeldInboxRow } from "./open-row";

/**
 * The notification action that just emptied the Inbox, kept in memory so the
 * Inbox Zero surface can offer a persistent inline undo (and return to that
 * notification) after the transient toast has gone.
 */
export type LastInboxCleared =
  | {
      kind: "archived";
      itemId: string;
      key: string;
      label: string;
      href: string;
      clearedAt: number;
    }
  | {
      kind: "snoozed";
      itemId: string;
      key: string;
      label: string;
      href: string;
      until: string;
      wasRead: boolean;
      clearedAt: number;
    }
  | {
      kind: "unsubscribed";
      itemId: string;
      issueId: string;
      key: string;
      label: string;
      href: string;
      undoable: boolean;
      clearedAt: number;
    }
  | {
      kind: "batch_archived";
      count: number;
      itemIds: string[];
      key: string | null;
      clearedAt: number;
    };

export type InboxTransitionDirection = "next" | "prev";

/**
 * Directional hint recorded right before the Inbox advances to the next item
 * (`"next"`) or undoes back to a cleared one (`"prev"`), so the arriving
 * detail can play a subtle 12px glide aligned with list order.
 */
export interface InboxTransition {
  targetKey: string;
  direction: InboxTransitionDirection;
  at: number;
}

const INBOX_TRANSITION_TTL_MS = 3_000;

interface InboxSelectionState {
  lastResolvedKeyByWorkspace: Record<string, string>;
  rememberResolvedKey: (wsId: string, key: string) => void;
  /** The issue last opened from the Work panel's Inbox section. */
  sidebarIssueByWorkspace: Record<string, string | null>;
  rememberSidebarIssue: (wsId: string, issueId: string | null) => void;
  /**
   * Pending directional transition into the next (or undone) Inbox item.
   * Consumed once when that destination mounts; stale entries expire.
   */
  transitionByWorkspace: Record<string, InboxTransition>;
  recordTransition: (wsId: string, targetKey: string, direction: InboxTransitionDirection) => void;
  consumeTransition: (wsId: string, key: string) => InboxTransitionDirection | null;
  /**
   * Issues the Inbox section opened in another tab of this window. Desktop
   * tabs share this store, so each of these keeps counting as opened from
   * the Inbox beside the issue the current tab last opened from it.
   */
  sidebarTabIssuesByWorkspace: Record<string, string[]>;
  rememberSidebarTabIssue: (wsId: string, issueId: string) => void;
  forgetSidebarTabIssue: (wsId: string, issueId: string) => void;
  /**
   * The issue a notification row was last clicked for, with a counter that
   * moves on every click. The issue page replays its landing (the comment or
   * app request the link names) when the counter moves, which a click on the
   * row already open needs: the page neither remounts nor sees a new link.
   *
   * Until the page reports it `landed`, the click outranks the page's record
   * that it already landed on that comment. The record belongs to the issue's
   * route, which a click on another page cannot reach before it navigates.
   */
  landing: { issueId: string; token: number; landed: boolean } | null;
  requestLanding: (issueId: string) => void;
  /** The issue page landed the click with this token. */
  completeLanding: (token: number) => void;
  /**
   * The open notification's row as each list last showed it, by
   * `${wsId}:${list}`, so the list can keep it in place (`keepOpenInboxRow`).
   */
  heldRows: Record<string, HeldInboxRow>;
  holdRow: (wsId: string, list: string, row: HeldInboxRow) => void;
  /**
   * The open notification an Inbox action left, by workspace. It leaves its
   * lists for good, even while it stays open because nothing was left to
   * move on to.
   */
  leftRowKeyByWorkspace: Record<string, string>;
  leaveRow: (wsId: string, key: string) => void;
  /** Undoes `leaveRow` for an action that was undone or failed, so the row can be kept again. */
  returnRow: (wsId: string, key: string) => void;
  /** Forgets every row held or left for these notifications, whose issues are gone. */
  forgetRows: (wsId: string, keys: readonly string[]) => void;
  /** Forgets the rows held or left for notifications that are no longer open. */
  releaseRows: (wsId: string, openKeys: readonly string[]) => void;
  /** Forgets the row one list holds, for a list that is going away. */
  dropRow: (wsId: string, list: string) => void;
  /** The action that just emptied the Inbox, per workspace, for the Inbox Zero screen. */
  lastClearedByWorkspace: Record<string, LastInboxCleared>;
  recordLastCleared: (wsId: string, entry: LastInboxCleared) => void;
  clearLastCleared: (wsId: string, matchKey?: string) => void;
}

const heldRowId = (wsId: string, list: string) => `${wsId}:${list}`;

// Keep only the last resolved pointer per workspace, for this client run.
// Notification data and its cache lifetime remain owned by TanStack Query.
export const useInboxSelectionStore = create<InboxSelectionState>()((set, get) => ({
  lastResolvedKeyByWorkspace: {},
  rememberResolvedKey: (wsId, key) =>
    set((state) => state.lastResolvedKeyByWorkspace[wsId] === key ? state : {
      lastResolvedKeyByWorkspace: { ...state.lastResolvedKeyByWorkspace, [wsId]: key },
    }),
  sidebarIssueByWorkspace: {},
  rememberSidebarIssue: (wsId, issueId) =>
    set((state) => (state.sidebarIssueByWorkspace[wsId] ?? null) === issueId ? state : {
      sidebarIssueByWorkspace: { ...state.sidebarIssueByWorkspace, [wsId]: issueId },
    }),
  transitionByWorkspace: {},
  recordTransition: (wsId, targetKey, direction) =>
    set((state) => ({
      transitionByWorkspace: {
        ...state.transitionByWorkspace,
        [wsId]: { targetKey, direction, at: Date.now() },
      },
    })),
  consumeTransition: (wsId, key) => {
    const entry = get().transitionByWorkspace[wsId];
    if (!entry || entry.targetKey !== key) return null;
    const fresh = Date.now() - entry.at <= INBOX_TRANSITION_TTL_MS;
    set((state) => {
      if (state.transitionByWorkspace[wsId] !== entry) return state;
      const { [wsId]: _consumed, ...transitionByWorkspace } = state.transitionByWorkspace;
      return { transitionByWorkspace };
    });
    return fresh ? entry.direction : null;
  },
  sidebarTabIssuesByWorkspace: {},
  rememberSidebarTabIssue: (wsId, issueId) =>
    set((state) => {
      const issueIds = state.sidebarTabIssuesByWorkspace[wsId] ?? [];
      return issueIds.includes(issueId) ? state : {
        sidebarTabIssuesByWorkspace: { ...state.sidebarTabIssuesByWorkspace, [wsId]: [...issueIds, issueId] },
      };
    }),
  forgetSidebarTabIssue: (wsId, issueId) =>
    set((state) => {
      const issueIds = state.sidebarTabIssuesByWorkspace[wsId] ?? [];
      return issueIds.includes(issueId) ? {
        sidebarTabIssuesByWorkspace: {
          ...state.sidebarTabIssuesByWorkspace,
          [wsId]: issueIds.filter((id) => id !== issueId),
        },
      } : state;
    }),
  landing: null,
  requestLanding: (issueId) =>
    set((state) => ({ landing: { issueId, token: (state.landing?.token ?? 0) + 1, landed: false } })),
  completeLanding: (token) =>
    set((state) => state.landing?.token === token && !state.landing.landed
      ? { landing: { ...state.landing, landed: true } }
      : state),
  heldRows: {},
  holdRow: (wsId, list, row) =>
    set((state) => {
      if (state.leftRowKeyByWorkspace[wsId] === row.key) return state;
      const id = heldRowId(wsId, list);
      const current = state.heldRows[id];
      if (
        current?.key === row.key &&
        current.item === row.item &&
        current.index === row.index &&
        current.filters === row.filters
      ) {
        return state;
      }
      return { heldRows: { ...state.heldRows, [id]: row } };
    }),
  leftRowKeyByWorkspace: {},
  leaveRow: (wsId, key) =>
    set((state) => ({
      leftRowKeyByWorkspace: { ...state.leftRowKeyByWorkspace, [wsId]: key },
      heldRows: Object.fromEntries(
        Object.entries(state.heldRows).filter(([id, row]) => !(id.startsWith(`${wsId}:`) && row.key === key)),
      ),
    })),
  returnRow: (wsId, key) =>
    set((state) => {
      if (state.leftRowKeyByWorkspace[wsId] !== key) return state;
      const { [wsId]: _returned, ...leftRowKeyByWorkspace } = state.leftRowKeyByWorkspace;
      return { leftRowKeyByWorkspace };
    }),
  forgetRows: (wsId, keys) =>
    set((state) => {
      const held = Object.entries(state.heldRows).filter(
        ([id, row]) => !(id.startsWith(`${wsId}:`) && keys.includes(row.key)),
      );
      const left = state.leftRowKeyByWorkspace[wsId];
      const leftGoes = left !== undefined && keys.includes(left);
      const cleared = state.lastClearedByWorkspace[wsId];
      const clearedGoes = !!cleared?.key && keys.includes(cleared.key);
      if (!leftGoes && !clearedGoes && held.length === Object.keys(state.heldRows).length) return state;
      const { [wsId]: _forgotten, ...otherLeft } = state.leftRowKeyByWorkspace;
      const { [wsId]: _cleared, ...otherCleared } = state.lastClearedByWorkspace;
      return {
        heldRows: Object.fromEntries(held),
        leftRowKeyByWorkspace: leftGoes ? otherLeft : state.leftRowKeyByWorkspace,
        lastClearedByWorkspace: clearedGoes ? otherCleared : state.lastClearedByWorkspace,
      };
    }),
  releaseRows: (wsId, openKeys) =>
    set((state) => {
      const left = state.leftRowKeyByWorkspace[wsId];
      const leftStays = left === undefined || openKeys.includes(left);
      const held = Object.entries(state.heldRows).filter(
        ([id, row]) => !id.startsWith(`${wsId}:`) || openKeys.includes(row.key),
      );
      if (leftStays && held.length === Object.keys(state.heldRows).length) return state;
      const { [wsId]: _released, ...otherLeft } = state.leftRowKeyByWorkspace;
      return {
        heldRows: Object.fromEntries(held),
        leftRowKeyByWorkspace: leftStays ? state.leftRowKeyByWorkspace : otherLeft,
      };
    }),
  dropRow: (wsId, list) =>
    set((state) => {
      const id = heldRowId(wsId, list);
      if (!(id in state.heldRows)) return state;
      const { [id]: _dropped, ...heldRows } = state.heldRows;
      return { heldRows };
    }),
  lastClearedByWorkspace: {},
  recordLastCleared: (wsId, entry) =>
    set((state) => ({
      lastClearedByWorkspace: { ...state.lastClearedByWorkspace, [wsId]: entry },
    })),
  clearLastCleared: (wsId, matchKey) =>
    set((state) => {
      const current = state.lastClearedByWorkspace[wsId];
      if (!current) return state;
      if (matchKey !== undefined && current.key !== matchKey) return state;
      const { [wsId]: _cleared, ...lastClearedByWorkspace } = state.lastClearedByWorkspace;
      return { lastClearedByWorkspace };
    }),
}));
