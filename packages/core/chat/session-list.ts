import type { QueryClient, QueryKey } from "@tanstack/react-query";

/**
 * Refetch a chat sessions list so it reflects a server write that has just
 * committed (e.g. the title a first send initializes).
 *
 * A bare invalidate joins a list request already in flight, and that request
 * may have read the row before the write. With `staleTime: Infinity` its stale
 * response would then stick, and a realtime patch cannot help while the row is
 * not cached yet. Cancelling first makes the refetch start after the commit.
 */
export async function refetchChatSessionsAfterCommit(
  qc: QueryClient,
  queryKey: QueryKey,
): Promise<void> {
  await qc.cancelQueries({ queryKey });
  await qc.invalidateQueries({ queryKey });
}
