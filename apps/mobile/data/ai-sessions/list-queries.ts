import { queryOptions, type QueryClient } from "@tanstack/react-query";
import type { LocalV2SessionPage, LocalV2Summary } from "@multica/core/local-conversation/v2";
import { refreshAISessionBrowse, aiKeys, type AISessionClient, type SessionScope } from "./client";

export const SESSION_LIST_PAGE_SIZE = 8;

export function sessionPageCursors(count: number, read: (cursor: string) => LocalV2SessionPage | undefined): string[] {
  const cursors = [""];
  while (cursors.length < count) {
    const next = read(cursors[cursors.length - 1])?.next_cursor;
    if (!next || cursors.includes(next)) break;
    cursors.push(next);
  }
  return cursors;
}

export function sessionPageSummaries(pages: (LocalV2SessionPage | undefined)[]): LocalV2Summary[] {
  const rows = new Map<string, LocalV2Summary>();
  for (const page of pages) for (const row of page?.summaries ?? []) {
    if ((rows.get(row.conversation_id)?.summary_seq ?? -1) < row.summary_seq) rows.set(row.conversation_id, row);
  }
  return [...rows.values()];
}
export const sessionGroupsOptions = (client: AISessionClient, archived: boolean, search: string) => queryOptions({
  queryKey: [...aiKeys.browse(client.scope), "groups", archived, search.trim()],
  queryFn: ({ signal }) => client.sessionGroups(archived, search.trim(), signal),
  staleTime: 30_000, retry: false,
});

export const sessionPageOptions = (client: AISessionClient, groupId: string, archived: boolean, search: string, cursor: string) => queryOptions({
  queryKey: [...aiKeys.browse(client.scope), "page", groupId, archived, search.trim(), cursor],
  queryFn: ({ signal }) => client.sessionPage({ groupId, archived, search: search.trim(), cursor, limit: SESSION_LIST_PAGE_SIZE }, signal),
  staleTime: 30_000, retry: false,
});

export const invalidateAISessionLists = (qc: QueryClient, scope: SessionScope) => refreshAISessionBrowse(qc, scope);
