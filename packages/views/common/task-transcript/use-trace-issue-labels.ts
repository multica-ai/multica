"use client";

import { useCallback, useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { TraceEvent } from "./trace-event-presenter";
import { collectTraceIssueIds, replaceTraceIssueIds } from "./trace-issue-labels";

export function useTraceIssueLabels(wsId: string, issueId: string, events: readonly TraceEvent[], enabled = true) {
  const ids = useMemo(() => collectTraceIssueIds(issueId, events), [issueId, events]);
  const issues = useQueries({
    queries: ids.map((id) => ({
      ...issueDetailOptions(wsId, id),
      enabled: enabled && !!wsId,
      staleTime: 60_000,
      retry: false,
    })),
    combine: (results) => results.flatMap(({ data }) =>
      data?.id && data.identifier ? [{ id: data.id, identifier: data.identifier }] : []),
  });
  const labels = useMemo(() => new Map(issues.map((issue) => [issue.id.toLowerCase(), issue.identifier])), [issues]);
  return useCallback((text: string) => replaceTraceIssueIds(text, labels), [labels]);
}
