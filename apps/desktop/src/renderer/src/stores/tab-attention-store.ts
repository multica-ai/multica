import { create } from "zustand";
import { splitTabUrl } from "./tab-store";

/**
 * Ephemeral per-tab attention dots: a background chat or issue tab whose run
 * finished while the user was elsewhere gets a red dot until they look at it.
 *
 * Deliberately NOT part of the tab store: that store persists, and a dot is
 * ephemeral UI state (State Rules) — it must not survive a restart, and it
 * must not complicate the persisted shape or its migrations. Entries are
 * keyed by tab id. A closed tab's entry is inert (no tab renders it) and is
 * bounded by this session's activity; the visible tab clears its own entry
 * from the tab item.
 */
interface TabAttentionStore {
  /** Present key = dot shown on that tab. */
  attention: Record<string, true>;
  markTabAttention: (tabId: string) => void;
  clearTabAttention: (tabId: string) => void;
}

export const useTabAttentionStore = create<TabAttentionStore>()((set) => ({
  attention: {},
  markTabAttention: (tabId) =>
    set((s) => (tabId in s.attention ? s : { attention: { ...s.attention, [tabId]: true } })),
  clearTabAttention: (tabId) =>
    set((s) => {
      if (!(tabId in s.attention)) return s;
      const attention = { ...s.attention };
      delete attention[tabId];
      return { attention };
    }),
}));

/** Primitive subscription: does this tab currently show a dot? */
export function useTabAttention(tabId: string): boolean {
  return useTabAttentionStore((s) => tabId in s.attention);
}

/** The tab-group slices the matcher reads; satisfied by the tab store. */
interface TabGroupLike {
  tabs: { id: string; url: string }[];
  activeTabId: string;
}

/** A `task:completed` / `task:failed` payload — one finished run. */
export interface TaskFinishedEvent {
  /** Present for chat runs; absent for issue runs. */
  chat_session_id?: string;
  issue_id: string;
}

/**
 * Which tabs a finished run should dot.
 *
 * A chat run dots the chat tab currently showing its session (`/slug/chat`
 * is one tab; the `?session=` param says which conversation it holds). An
 * issue run dots that issue's detail tab. The tab the user is looking at
 * right now (active tab of the active workspace) is skipped — a dot there
 * would only state what is already on screen.
 */
export function attentionTargetTabIds(
  byWorkspace: Record<string, TabGroupLike>,
  activeWorkspaceSlug: string | null,
  event: TaskFinishedEvent,
): string[] {
  const targets: string[] = [];
  for (const [slug, group] of Object.entries(byWorkspace)) {
    const visibleTabId = slug === activeWorkspaceSlug ? group.activeTabId : null;
    for (const tab of group.tabs) {
      if (tab.id === visibleTabId) continue;
      if (tabMatchesEvent(slug, tab.url, event)) targets.push(tab.id);
    }
  }
  return targets;
}

function tabMatchesEvent(slug: string, url: string, event: TaskFinishedEvent): boolean {
  const { pathname, suffix } = splitTabUrl(url);
  const segments = pathname.split("/").filter(Boolean);
  if (segments[0] !== slug) return false;

  if (event.chat_session_id) {
    // The workspace's single chat tab, when it is showing this conversation.
    return (
      segments.length === 2 &&
      segments[1] === "chat" &&
      searchParamsOfSuffix(suffix).get("session") === event.chat_session_id
    );
  }

  // An issue detail tab: `/slug/issues/<id>`. List routes (`/slug/issues`,
  // `/slug/issues/new`) never have a third segment and cannot match.
  return segments.length === 3 && segments[1] === "issues" && segments[2] === event.issue_id;
}

function searchParamsOfSuffix(suffix: string): URLSearchParams {
  if (!suffix.startsWith("?")) return new URLSearchParams();
  const hashIndex = suffix.indexOf("#");
  return new URLSearchParams(hashIndex === -1 ? suffix.slice(1) : suffix.slice(1, hashIndex));
}
