"use client";

import { useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
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
 */
export function useArchivedInboxRows(wsId: string, enabled = true) {
  const stored = useInboxFilters(wsId);
  const [issueFields, setIssueFields] = useState<InboxFilterSupport>("supported");
  const filters = useMemo(
    () => effectiveInboxFilters(stored, { priority: "supported", issueFields, archived: true }),
    [stored, issueFields],
  );
  const query = useInfiniteQuery({ ...archivedInboxPagesOptions(wsId, filters), enabled: enabled && !!wsId });
  const items = useMemo(
    () => deduplicateArchivedInboxItems(query.data?.pages.flatMap((page) => page.items) ?? []),
    [query.data],
  );
  if (issueFields !== "unsupported" && inboxIssueFieldsSupport(items) === "unsupported") {
    setIssueFields("unsupported");
  }
  return { query, items, filters, issueFieldsSupport: issueFields };
}
