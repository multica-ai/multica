import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueViewContainerKey,
  useActiveIssueViewStore,
} from "@multica/core/issue-views/active-view-store";
import type { IssueViewScope } from "@multica/core/issue-views/queries";
import { useNavigation } from "@multica/views/navigation";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

const tabViews = new Map<string, Record<string, string>>();
let shownTabId: string | null = null;
let unsubscribe: (() => void) | null = null;

/**
 * Give every tab its own open views. The active-view store is one map for the
 * whole window, so it is swapped with the active tab inside the tab store's
 * subscription — zustand runs it synchronously in `set()`, before React
 * renders the incoming tab — and each tab's map is parked here meanwhile.
 * Memory only, like the store: a tab's `?view=` is what survives a restart.
 */
export function initIssueViewTabs(): void {
  if (unsubscribe) return;
  shownTabId = getActiveTab(useTabStore.getState())?.id ?? null;
  unsubscribe = useTabStore.subscribe((state) => {
    const nextId = getActiveTab(state)?.id ?? null;
    if (nextId === shownTabId) return;
    if (shownTabId) tabViews.set(shownTabId, useActiveIssueViewStore.getState().active);
    shownTabId = nextId;
    const open = new Set(Object.values(state.byWorkspace).flatMap((group) => group.tabs.map((tab) => tab.id)));
    for (const id of tabViews.keys()) if (!open.has(id)) tabViews.delete(id);
    useActiveIssueViewStore.setState({ active: (nextId && tabViews.get(nextId)) || {} });
  });
}

export function __resetIssueViewTabsForTests(): void {
  unsubscribe?.();
  unsubscribe = null;
  shownTabId = null;
  tabViews.clear();
}

/**
 * Desktop counterpart of web's useIssueViewUrlSync, around a page whose issue
 * surface can open saved views. The tab's URL wins when the page mounts with
 * a `?view=` (a cmd+clicked tab, a restored session) and on every later URL
 * change (back/forward); the store writes back with replace otherwise, so the
 * URL always names what the tab shows.
 */
export function IssueViewUrlSync({ scope, children }: { scope: IssueViewScope; children: React.ReactNode }) {
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const containerKey = issueViewContainerKey(wsId, scope);
  const urlView = navigation.searchParams.get("view");
  const activeViewId = useActiveIssueViewStore((s) => s.active[containerKey] ?? null);
  const setActive = useActiveIssueViewStore((s) => s.setActive);
  const seenUrlView = useRef<string | null | undefined>(undefined);
  // Until the URL is adopted the store can still hold another surface's view;
  // rendering against it would mount (and fetch) that view first.
  const [adopting, setAdopting] = useState(() => urlView !== null && urlView !== activeViewId);

  useLayoutEffect(() => {
    const mounting = seenUrlView.current === undefined;
    if (urlView === seenUrlView.current) return;
    seenUrlView.current = urlView;
    if (!mounting || urlView) setActive(containerKey, urlView);
    setAdopting(false);
  }, [containerKey, urlView, setActive]);

  useEffect(() => {
    const current = useActiveIssueViewStore.getState().active[containerKey] ?? null;
    if (current === seenUrlView.current) return;
    const params = new URLSearchParams(navigation.searchParams);
    if (current) params.set("view", current);
    else params.delete("view");
    const search = params.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}${navigation.hash}`);
  }, [activeViewId, containerKey, navigation]);

  return adopting ? null : children;
}
