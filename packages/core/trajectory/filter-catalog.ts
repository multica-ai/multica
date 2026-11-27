import { CancelledError, QueryClient, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { IssueUnreadPageSchema } from "../api/schemas";
import { closeGraphScope, graphKeys, GraphReadError, isGraphScopeClosed, type GraphScope } from "./graph-web";

async function readMetadata(client: QueryClient, scope: GraphScope, signal: AbortSignal, path: string): Promise<unknown> {
  const current = () => { if (signal.aborted || isGraphScopeClosed(client, scope)) throw new CancelledError({ silent: true, revert: false }); };
  current();
  try { const value = await api.readTrajectoryFilterMetadata(path, { workspaceId: scope.workspaceId, signal }); current(); return value; }
  catch (error) {
    current();
    const status = error && typeof error === "object" && "status" in error && typeof error.status === "number" ? error.status : undefined;
    if (status === 401 || status === 403) closeGraphScope(client, scope);
    throw new GraphReadError(status);
  }
}

export function graphFilterUnreadOptions(scope: GraphScope) {
  return queryOptions({
    queryKey: [...graphKeys.all(scope), "filter-unread"] as const, staleTime: 0, retry: false,
    queryFn: async ({ client, signal }) => {
      const ids = new Set<string>(), cursors = new Set<string>();
      let cursor: string | null = null;
      do {
        const raw = await readMetadata(client, scope, signal, `/api/issues/unread${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`);
        const parsed = IssueUnreadPageSchema.safeParse(raw);
        if (!parsed.success) throw new GraphReadError();
        for (const item of parsed.data.items) {
          if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(item.issue_id)) throw new GraphReadError();
          ids.add(item.issue_id);
        }
        if (new TextEncoder().encode(JSON.stringify([...ids])).length > 65_536) throw new GraphReadError();
        cursor = parsed.data.next_cursor;
        if (cursor && cursors.has(cursor)) throw new GraphReadError();
        if (cursor) cursors.add(cursor);
      } while (cursor);
      return [...ids];
    },
  });
}
