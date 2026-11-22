"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { prefetchIssuePage } from "@multica/core/issues/queries";
import { useIsGuest } from "@multica/core/permissions";
import type { InboxItem } from "@multica/core/types";
import { useNavigation } from "../../navigation";

/** Unread notifications whose issue is warmed once the list is idle. */
export const INBOX_IDLE_PREFETCH_LIMIT = 3;

/**
 * Opening a notification should render from cache, not wait on the issue's
 * request chain. The first few unread rows are warmed one issue at a time when
 * the browser is idle; any other row is warmed when it is pointed at or
 * focused. Returns the per-row warm-up for those events.
 */
export function useInboxIssuePrefetch(
  wsId: string,
  rows: readonly InboxItem[],
  hrefOf: (item: InboxItem) => string,
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

  const idleTargets = useMemo(
    () => rows.filter((item) => item.issue_id && item.read !== true).slice(0, INBOX_IDLE_PREFETCH_LIMIT),
    [rows],
  );
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
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(() => void run(), { timeout: 500 });
      return () => {
        cancelled = true;
        window.cancelIdleCallback(id);
      };
    }
    const timer = window.setTimeout(() => void run(), 300);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed by the issues to warm
  }, [idleKey, wsId]);

  return prefetchRow;
}
