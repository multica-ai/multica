import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { fileChangesStatus } from "./file-changes-schema";
import { api, ApiError, errorCode } from "../api";
export * from "./file-changes-schema";
export const fileChangesKeys = {
  all: (wsId: string) => ["workspaces", wsId, "file-changes"] as const,
  list: (wsId: string, taskId: string) =>
    [...fileChangesKeys.all(wsId), taskId] as const,
};
export function fileChangesOptions(
  wsId: string,
  taskId: string,
  enabled = true,
) {
  return infiniteQueryOptions({
    queryKey: fileChangesKeys.list(wsId, taskId),
    enabled,
    initialPageParam: { cursor: "0", collectionId: null as string | null },
    queryFn: async ({ pageParam }) => {
      const page = await api.listRunFileChanges(taskId, pageParam.cursor);
      // A degraded later page must remain retryable at the same cursor.
      if (
        pageParam.cursor !== "0" &&
        (page.collectionId !== pageParam.collectionId ||
          fileChangesStatus(page) === "unavailable")
      )
        throw new Error("Invalid file changes page");
      return page;
    },
    getNextPageParam: (page, _pages, cursor) =>
      page.nextCursor !== null &&
      Number(page.nextCursor) > Number(cursor.cursor)
        ? { cursor: page.nextCursor, collectionId: page.collectionId }
        : undefined,
    refetchInterval: (query) =>
      query.state.data?.pages[0] &&
      fileChangesStatus(query.state.data.pages[0]) === "collecting"
        ? 2000
        : false,
    staleTime: 0,
  });
}
export function fileChangeBodyKey(
  wsId: string,
  taskId: string,
  collectionId: string | null,
  fileId: string,
) {
  return [
    ...fileChangesKeys.list(wsId, taskId),
    "body",
    collectionId,
    fileId,
  ] as const;
}
export function fileChangeBodyOptions(
  wsId: string,
  taskId: string,
  collectionId: string | null,
  fileId: string,
  enabled: boolean,
) {
  return queryOptions({
    queryKey: fileChangeBodyKey(wsId, taskId, collectionId, fileId),
    enabled,
    queryFn: () => api.getRunFileChangeBody(taskId, fileId),
    staleTime: Infinity,
    // Every failure renders a status card with an explicit retry button, and
    // the runtime's return to online triggers exactly one refetch. Automatic
    // retries would only multiply the server's 10 s relay waits.
    retry: false,
  });
}

// Relay outcomes the server marks with a stable error code: 503
// runtime_offline / 504 runtime_timeout / 429 relay_busy all mean "the runtime
// cannot serve this body right now" — one recoverable offline display state.
const offlineStatuses = new Set([503, 504, 429]);
const offlineCodes = new Set(["runtime_offline", "runtime_timeout", "relay_busy"]);
export type FileChangeBodyState = "ready" | "offline" | "purged" | "error";
/**
 * Classify a body fetch failure for display. Relay trouble with the
 * runtime (503/504/429, matched by status or error code so a proxy rewriting
 * statuses still lands) is one recoverable offline state, 410 is a terminal
 * purged record, and everything else — including a runtime that vanished
 * between manifest and relay — is a generic retryable failure.
 */
export function fileChangeBodyState(error: unknown): FileChangeBodyState {
  if (!(error instanceof ApiError)) return "error";
  const code = errorCode(error);
  if (error.status === 410 || code === "record_purged") return "purged";
  if (
    offlineStatuses.has(error.status) ||
    (code !== undefined && offlineCodes.has(code))
  )
    return "offline";
  return "error";
}
