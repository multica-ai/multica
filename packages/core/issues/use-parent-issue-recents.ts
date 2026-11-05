"use client";

import { useId, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { api } from "../api";
import {
  selectRecentIssues,
  useRecentIssuesStore,
} from "./stores/recent-issues-store";

/** Parent suggestions need a new authorization read for every open, even when
 * the ordinary issue-detail cache is fresh. Keep this session's results separate
 * so a denial never falls back to a previously visible title. */
export function useParentIssueRecents(wsId: string, open: boolean) {
  const pickerId = useId();
  const [session, setSession] = useState({ wsId, open, generation: 0 });
  if (session.wsId !== wsId || session.open !== open) {
    setSession({ wsId, open, generation: session.generation + 1 });
  }
  const entries = useRecentIssuesStore(selectRecentIssues(wsId));
  const ids = [...new Set(entries.map((entry) => entry.id))].slice(0, 20);
  const active = open && session.open && session.wsId === wsId;
  const queries = useQueries({
    queries: (active ? ids : []).map((id) => ({
      queryKey: ["parent-issue-recents", wsId, pickerId, session.generation, id],
      queryFn: async ({ signal }: { signal: AbortSignal }) => {
        const issue = await api.getIssue(id, { signal });
        if (issue.workspace_id !== wsId || issue.id !== id) {
          throw new Error("Parent suggestion does not belong to this workspace");
        }
        return issue;
      },
      gcTime: 0,
      staleTime: Infinity,
      retry: false,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    })),
  });
  return queries.flatMap((query) =>
    query.isSuccess && !query.isFetching ? [query.data] : [],
  );
}
