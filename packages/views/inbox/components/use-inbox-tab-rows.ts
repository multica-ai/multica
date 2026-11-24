"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  archivedInboxPagesOptions,
  deduplicateArchivedInboxItems,
  snoozedInboxOptions,
} from "@multica/core/inbox/queries";
import {
  effectiveInboxFilters,
  inboxIssueFieldsSupport,
  useInboxFilters,
  type InboxFilterSupport,
} from "@multica/core/inbox/filter-store";
import type { SnoozedInboxIssue } from "@multica/core/types";
import { inboxListLoad } from "./use-filtered-inbox-rows";
import type { InboxTab } from "./inbox-view";

const NO_SNOOZED: SnoozedInboxIssue[] = [];

/**
 * The Snoozed tab: the issues the viewer snoozed, soonest back first. An
 * entry whose time has passed is back in the Inbox already, and the wake
 * timer refreshes the list.
 */
export function useSnoozedInboxRows(wsId: string, enabled = true) {
  const query = useQuery({ ...snoozedInboxOptions(wsId), enabled: enabled && !!wsId });
  const entries = query.data ?? NO_SNOOZED;
  const rows = useMemo(() => {
    const now = Date.now();
    return entries
      .filter((entry) => new Date(entry.snoozed_until).getTime() > now)
      .sort((a, b) => new Date(a.snoozed_until).getTime() - new Date(b.snoozed_until).getTime());
  }, [entries]);
  return {
    rows,
    load: inboxListLoad(query),
    retry: () => void query.refetch(),
    retrying: query.isFetching,
  };
}

/**
 * The Archived tab, a page at a time, under the Inbox filters. The server
 * filters the archive, so whether it has the issue fields can only be proven
 * by the rows it returns: an empty answer proves nothing and must not change
 * what is sent, so only rows without them (an older server) make those
 * dimensions dormant.
 *
 * `prefetch` loads the first page under these same filters while the tab is
 * closed, so opening it renders from cache.
 */
export function useArchivedInboxRows(wsId: string, enabled = true) {
  const qc = useQueryClient();
  const stored = useInboxFilters(wsId);
  const [issueFields, setIssueFields] = useState<InboxFilterSupport>("supported");
  const filters = useMemo(
    () => effectiveInboxFilters(stored, { priority: "supported", issueFields, archived: true }),
    [stored, issueFields],
  );
  const options = archivedInboxPagesOptions(wsId, filters);
  const query = useInfiniteQuery({ ...options, enabled: enabled && !!wsId });
  const items = useMemo(
    () => deduplicateArchivedInboxItems(query.data?.pages.flatMap((page) => page.items) ?? []),
    [query.data],
  );
  if (issueFields !== "unsupported" && inboxIssueFieldsSupport(items) === "unsupported") {
    setIssueFields("unsupported");
  }
  const prefetch = () => {
    if (wsId) void qc.prefetchInfiniteQuery(options);
  };
  return { query, items, filters, issueFieldsSupport: issueFields, prefetch };
}

/**
 * Warms the Snoozed and Archived tabs so a switch renders from cache: once
 * the workspace is idle, and again when a tab is pointed at or focused.
 *
 * A prefetch adds no observer, so an inbox event only marks the warmed lists
 * stale rather than refetching them; the tab refreshes once it opens.
 */
export function useInboxTabPrefetch(wsId: string, archived: { prefetch: () => void }) {
  const qc = useQueryClient();
  const prefetchTab = (tab: InboxTab) => {
    if (!wsId) return;
    if (tab === "archived") archived.prefetch();
    else if (tab === "snoozed") void qc.prefetchQuery(snoozedInboxOptions(wsId));
  };
  const latest = useRef(prefetchTab);
  useLayoutEffect(() => {
    latest.current = prefetchTab;
  });
  useEffect(() => {
    if (!wsId) return;
    const warm = () => {
      latest.current("snoozed");
      latest.current("archived");
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(warm, { timeout: 3000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = window.setTimeout(warm, 1500);
    return () => window.clearTimeout(timer);
  }, [wsId]);
  return prefetchTab;
}
