// FIR-2947 (TECH-2947 follow-up) — sort method + direction for the inbox list.
// Lives in cerebro-inbox so the upstream-zone inbox page only needs small
// CEREBRO-PATCH-marked imports.

import { useCallback, useEffect, useState } from "react";

export type InboxSortMethod = "last_message" | "last_other" | "last_me";
export type InboxSortDirection = "desc" | "asc";

export interface InboxSortPreference {
  method: InboxSortMethod;
  direction: InboxSortDirection;
}

export const INBOX_SORT_METHOD_OPTIONS: { value: InboxSortMethod; label: string }[] = [
  { value: "last_message", label: "Last message" },
  { value: "last_other", label: "Last from another actor" },
  { value: "last_me", label: "Last from me" },
];

export const INBOX_SORT_DIRECTION_OPTIONS: { value: InboxSortDirection; label: string }[] = [
  { value: "desc", label: "Newest first" },
  { value: "asc", label: "Oldest first" },
];

const DEFAULT_PREF: InboxSortPreference = { method: "last_message", direction: "desc" };

function isMethod(s: string | null): s is InboxSortMethod {
  return s === "last_message" || s === "last_other" || s === "last_me";
}

function isDirection(s: string | null): s is InboxSortDirection {
  return s === "desc" || s === "asc";
}

// Per-(workspace, user) localStorage-backed preference for the inbox sort.
export function useInboxSortPreference(
  wsId: string,
  userId: string,
): [InboxSortPreference, (next: Partial<InboxSortPreference>) => void] {
  const storageKey = `multica_inbox_sort_${wsId}_${userId}`;
  const [pref, setPrefState] = useState<InboxSortPreference>(() => {
    if (typeof window === "undefined") return DEFAULT_PREF;
    const saved = window.localStorage.getItem(storageKey);
    if (!saved) return DEFAULT_PREF;
    const [m, d] = saved.split(",");
    return {
      method: isMethod(m ?? null) ? (m as InboxSortMethod) : DEFAULT_PREF.method,
      direction: isDirection(d ?? null) ? (d as InboxSortDirection) : DEFAULT_PREF.direction,
    };
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(storageKey, `${pref.method},${pref.direction}`);
  }, [pref, storageKey]);
  const setPref = useCallback((next: Partial<InboxSortPreference>) => {
    setPrefState((cur) => ({ ...cur, ...next }));
  }, []);
  return [pref, setPref];
}

// Per-(workspace, user) toggle for whether the focus/priorities panel is shown.
// Default ON — the panel is the headline reason the feature exists.
export function useFocusListVisibility(wsId: string, userId: string): [boolean, (visible: boolean) => void] {
  const storageKey = `multica_inbox_show_priorities_${wsId}_${userId}`;
  const [visible, setVisibleState] = useState<boolean>(() => {
    if (typeof window === "undefined") return true;
    const saved = window.localStorage.getItem(storageKey);
    if (saved === null) return true;
    return saved === "1";
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(storageKey, visible ? "1" : "0");
  }, [visible, storageKey]);
  return [visible, setVisibleState];
}

// `extractor` gives the three time signals for one entry. -Infinity means the
// signal is unavailable for that entry — those entries fall to the bottom of
// the list for `last_me` / `last_other` regardless of direction.
export interface InboxSortSignals {
  lastMessage: number;
  lastFromMe: number;
  lastFromOther: number;
}

export function sortByInboxPreference<T>(
  entries: T[],
  pref: InboxSortPreference,
  extractor: (entry: T) => InboxSortSignals,
): T[] {
  const dir = pref.direction === "desc" ? -1 : 1;
  const pick = (s: InboxSortSignals): number => {
    if (pref.method === "last_me") return s.lastFromMe;
    if (pref.method === "last_other") return s.lastFromOther;
    return s.lastMessage;
  };
  return [...entries].sort((a, b) => {
    const sa = pick(extractor(a));
    const sb = pick(extractor(b));
    // -Infinity means unknown for this entry; always sink to the bottom.
    const aMissing = !Number.isFinite(sa);
    const bMissing = !Number.isFinite(sb);
    if (aMissing && !bMissing) return 1;
    if (!aMissing && bMissing) return -1;
    if (aMissing && bMissing) return 0;
    return (sa - sb) * dir;
  });
}
