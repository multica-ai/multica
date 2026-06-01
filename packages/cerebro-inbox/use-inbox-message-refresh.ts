"use client";

import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";

/**
 * FIR-2684 — the global QueryClient pins `staleTime: Infinity`, so re-opening an
 * issue whose timeline is already cached does NOT refetch on mount; a comment
 * that arrived while the tab was backgrounded (no live WS event applied) would
 * be missing. The old inbox behaviour masked this by routing selection through
 * a Next.js RSC navigation that re-rendered + refetched the whole route — the
 * "whole page reloads" bug. Now selection is a silent URL update, so we
 * explicitly refetch just the opened issue's message (timeline + detail).
 * `invalidateQueries` refetches the active observers regardless of `staleTime`,
 * so the latest comment is always present — without reloading the page.
 */
export function refreshInboxMessageQueries(
  qc: QueryClient,
  wsId: string,
  issueId: string | null,
): void {
  if (!issueId) return;
  void qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
  void qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
}

/**
 * Refetch the opened inbox message whenever the selected issue changes. Pass
 * `null` when the open item is not an issue (channel/DM/chat) — then it no-ops.
 */
export function useInboxMessageRefresh(
  wsId: string,
  issueId: string | null,
): void {
  const qc = useQueryClient();
  useEffect(() => {
    refreshInboxMessageQueries(qc, wsId, issueId);
  }, [qc, wsId, issueId]);
}
