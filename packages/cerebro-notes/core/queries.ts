import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { safeParseNote, safeParseNotes } from "./types";
import type { ListNotesParams } from "./types";

// Query keys, workspace-scoped so switching workspaces swaps the cache.
export const noteKeys = {
  all: (wsId: string) => ["notes", wsId] as const,
  list: (wsId: string, params: ListNotesParams) =>
    [
      ...noteKeys.all(wsId),
      "list",
      params.q ?? "",
      params.limit ?? 50,
      params.offset ?? 0,
    ] as const,
  recent: (wsId: string, limit: number) =>
    [...noteKeys.all(wsId), "recent", limit] as const,
  detail: (wsId: string, id: string) =>
    [...noteKeys.all(wsId), "detail", id] as const,
};

export function notesListOptions(wsId: string, params: ListNotesParams = {}) {
  return queryOptions({
    queryKey: noteKeys.list(wsId, params),
    queryFn: async () => safeParseNotes(await api.listNotes(params)),
    enabled: Boolean(wsId),
  });
}

export function recentNotesOptions(wsId: string, limit = 5) {
  return queryOptions({
    queryKey: noteKeys.recent(wsId, limit),
    queryFn: async () => safeParseNotes(await api.listRecentNotes(limit)),
    enabled: Boolean(wsId),
  });
}

export function noteDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: noteKeys.detail(wsId, id),
    queryFn: async () => safeParseNote(await api.getNote(id)),
    enabled: Boolean(wsId && id),
  });
}
