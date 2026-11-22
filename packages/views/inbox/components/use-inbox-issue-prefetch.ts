"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { inboxRowKey, nextInboxRow } from "@multica/core/inbox/open-row";
import { prefetchIssuePage } from "@multica/core/issues/queries";
import { useIsGuest } from "@multica/core/permissions";
import type { InboxItem } from "@multica/core/types";
import { useNavigation } from "../../navigation";

/** Unread notifications whose issue is warmed once the list is idle. */
export const INBOX_IDLE_PREFETCH_LIMIT = 3;

/**
 * Opening a notification should render from cache, not wait on the issue's
 * request chain. While reading an open notification (`openKey`), its next two
 * neighbours in list order (read or unread) are warmed first so auto-advancing
 * stays instant; the first few unread rows follow when idle. Any other row is
 * warmed when it is pointed at or focused.
 */
export function useInboxIssuePrefetch(
  wsId: string,
  rows: readonly InboxItem[],
  hrefOf: (item: InboxItem) => string,
  openKey?: string | null,
) {
  const qc = useQueryClient();
  const { prefetch: prefetchRoute } = useNavigation();
  // A guest's issue page shows no runs, and the server refuses it them.
  const guest = useIsGuest(wsId);

  const prefetchRow = useCallback(
    (item: InboxItem) => {
      if (!wsId || !item.issue_id) return Promise.resolve();
      return prefetchIssuePage(qc, wsId, item.issue_id, item.issue_identifier, { runs: !guest }).catch(() => {
        // A failed warm-up is the page's own first request again, retried there.
      });
    },
    [qc, wsId, guest],
  );

  const idleTargets = useMemo(() => {
    const picked: InboxItem[] = [];
    const seen = new Set<string>();
    const add = (item: InboxItem | null | undefined) => {
      if (!item?.issue_id || seen.has(item.issue_id)) return;
      seen.add(item.issue_id);
      picked.push(item);
    };

    if (openKey) {
      const next1 = nextInboxRow(rows, openKey);
      add(next1);
      if (next1) {
        const next1Key = inboxRowKey(next1);
        const next2 = nextInboxRow(rows, next1Key, (row) => inboxRowKey(row) === openKey);
        add(next2);
      }
    }

    let unreadAdded = 0;
    for (const item of rows) {
      if (unreadAdded >= INBOX_IDLE_PREFETCH_LIMIT) break;
      if (!item.issue_id || item.read === true) continue;
      if (openKey && inboxRowKey(item) === openKey) continue;
      add(item);
      unreadAdded += 1;
    }

    return picked;
  }, [rows, openKey]);
  // Effect identity follows the issues, not the row objects, which realtime
  // patches replace on every inbox event.
  const idleKey = idleTargets.map((item) => item.issue_id).join(",");

  useEffect(() => {
    if (!idleKey) return;
    let cancelled = false;
    const run = async () => {
      // One issue at a time: each fans out into four requests.
      for (const item of idleTargets) {
        if (cancelled) return;
        prefetchRoute?.(hrefOf(item));
        await prefetchRow(item);
      }
    };
    const timeout = openKey ? 150 : 500;
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(() => void run(), { timeout });
      return () => {
        cancelled = true;
        window.cancelIdleCallback(id);
      };
    }
    const timer = window.setTimeout(() => void run(), openKey ? 100 : 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed by the issues to warm
  }, [idleKey, wsId]);

  return prefetchRow;
}
