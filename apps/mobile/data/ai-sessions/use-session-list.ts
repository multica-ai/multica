import { useEffect, useState } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import type { AISessionClient, SessionScope } from "./client";
import { aiDraftKey } from "./drafts";
import { sessionGroupsOptions, sessionPageCursors, sessionPageOptions, sessionPageSummaries } from "./list-queries";
import { visibleSessions } from "./projection";

/** Requested page counts are UI state; each bounded server page lives in Query. */
export function useSessionList({ client, scope, connected, search, archived, expanded }: {
  client: AISessionClient | null; scope: SessionScope; connected: boolean;
  search: string; archived: boolean; expanded: Record<string, boolean>;
}) {
  const qc = useQueryClient();
  const [query, setQuery] = useState(search.trim());
  useEffect(() => { const timer = setTimeout(() => setQuery(search.trim()), 200); return () => clearTimeout(timer); }, [search]);
  const catalog = useQuery({
    ...(client ? sessionGroupsOptions(client, archived, query) : { queryKey: ["ai-sessions-disabled"], queryFn: async () => { throw new Error("device_unreachable"); } }),
    enabled: !!client && connected,
  });
  const frame = JSON.stringify([scope.userId, scope.workspaceId, scope.deviceId, archived, query]);
  const [requested, setRequested] = useState<{ frame: string; counts: Record<string, number> }>({ frame, counts: {} });
  const groups = (catalog.data?.groups ?? []).map((group) => {
    const key = aiDraftKey(scope, group.id || "unassigned");
    return { ...group, key, collapsed: !query && !expanded[key] };
  });
  // Follow the latest cursor chain after live updates move a page boundary.
  const descriptors = groups.flatMap((group) => sessionPageCursors(requested.frame === frame ? requested.counts[group.id] ?? 1 : 1,
    (cursor) => qc.getQueryData(sessionPageOptions(client!, group.id, archived, query, cursor).queryKey)).map((cursor) => ({ group, cursor })));
  const pages = useQueries({ queries: descriptors.map(({ group, cursor }) => ({ ...sessionPageOptions(client!, group.id, archived, query, cursor), enabled: !!client && connected && !group.collapsed && group.total > 0 })) });
  const sections = groups.map((group) => {
    const entries = pages.filter((_, index) => descriptors[index].group.id === group.id);
    const last = entries.at(-1);
    const summaries = sessionPageSummaries(entries.map((page) => page.data));
    const nextCursor = last?.data?.next_cursor;
    return { ...group, count: group.total, data: group.collapsed || group.total === 0 ? [] : visibleSessions(summaries, "", archived),
      loading: !group.collapsed && group.total > 0 && entries.some((page) => page.isPending),
      error: !group.collapsed && group.total > 0 ? entries.find((page) => page.isError)?.error : undefined,
      retry: () => {
        void catalog.refetch();
        if (entries.some((entry) => entry.error instanceof Error && entry.error.message === "invalid_request")) {
          setRequested((current) => ({ frame, counts: { ...(current.frame === frame ? current.counts : {}), [group.id]: 1 } }));
          void entries[0]?.refetch();
        } else for (const entry of entries) if (entry.isError) void entry.refetch();
      },
      hasMore: group.total > 0 && !!nextCursor,
      loadMore: () => {
        if (!nextCursor || last?.isFetching) return;
        setRequested((current) => {
          const counts = current.frame === frame ? current.counts : {};
          const nextCount = entries.length + 1;
          return (counts[group.id] ?? 1) >= nextCount ? current : { frame, counts: { ...counts, [group.id]: nextCount } };
        });
      },
    };
  });
  return { catalog, sections, query, refresh: () => {
    void catalog.refetch();
    for (const [index, page] of pages.entries()) if (!descriptors[index].group.collapsed && descriptors[index].group.total > 0) void page.refetch();
  } };
}
