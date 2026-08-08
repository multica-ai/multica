import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { inboxKeys } from "./queries";
import { inboxV2Keys, isGroupRow, type InboxRow } from "./rows";
import type { InboxGroupPage } from "../api/schemas";

/**
 * Inbox writes, routed by what the row actually is.
 *
 * A row carrying `group` is a v2 group and its write goes to the group
 * endpoints; anything else is a legacy item. The page never has to know which
 * generation it is on — it hands over the row it is acting on and the routing
 * happens here, in one place, rather than at every call site.
 *
 * Every mutation takes the ROW, not an id. An id alone cannot say which
 * endpoint owns it, and a group id sent to a v1 endpoint would 404 on
 * something that exists.
 *
 * None of these return the server's row. The caches are patched optimistically
 * and re-pulled on settle, so the response would only ever be a third source of
 * truth for state two already describe.
 */

/** What an optimistic patch has to put back if the request fails. */
interface RollbackContext {
  key: readonly unknown[];
  previous: unknown;
}

function rollback(qc: QueryClient, ctx: RollbackContext | undefined) {
  if (!ctx || ctx.previous === undefined) return;
  qc.setQueryData(ctx.key, ctx.previous);
}

function patchGroupPage(
  page: InboxGroupPage | undefined,
  id: string,
  patch: (item: InboxGroupPage["items"][number]) => InboxGroupPage["items"][number],
): InboxGroupPage | undefined {
  if (!page) return page;
  return { ...page, items: page.items.map((i) => (i.id === id ? patch(i) : i)) };
}

function dropFromGroupPage(
  page: InboxGroupPage | undefined,
  id: string,
): InboxGroupPage | undefined {
  if (!page) return page;
  return { ...page, items: page.items.filter((i) => i.id !== id) };
}

/**
 * One row mutation.
 *
 * `send` performs the write, `patch` applies the optimistic change to the one
 * cache entry that holds the row. Both branches are written once here rather
 * than four times below, which is what keeps a v1 fix from silently missing its
 * v2 counterpart.
 */
function useRowMutation(options: {
  send: (row: InboxRow) => Promise<unknown>;
  cacheKey: (row: InboxRow, wsId: string) => readonly unknown[];
  patchV1: (rows: InboxRow[] | undefined, id: string) => InboxRow[] | undefined;
  patchV2: (page: InboxGroupPage | undefined, id: string) => InboxGroupPage | undefined;
  invalidateSummary?: boolean;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<void, Error, InboxRow, RollbackContext>({
    mutationFn: async (row) => {
      await options.send(row);
    },
    onMutate: async (row) => {
      const scope = isGroupRow(row) ? inboxV2Keys.all(wsId) : inboxKeys.all(wsId);
      await qc.cancelQueries({ queryKey: scope });
      const key = options.cacheKey(row, wsId);
      const previous = qc.getQueryData(key);
      if (isGroupRow(row)) {
        qc.setQueryData<InboxGroupPage>(key, (old) => options.patchV2(old, row.id));
      } else {
        qc.setQueryData<InboxRow[]>(key, (old) => options.patchV1(old, row.id));
      }
      return { key, previous };
    },
    onError: (_err, _row, ctx) => rollback(qc, ctx),
    onSettled: (_data, _err, row) => {
      qc.invalidateQueries({
        queryKey: isGroupRow(row) ? inboxV2Keys.all(wsId) : inboxKeys.all(wsId),
      });
      if (options.invalidateSummary) {
        // The switcher dot is server-computed and cannot be patched here.
        qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
      }
    },
  });
}

/**
 * Mark a row read.
 *
 * The v2 call reports what the client actually SAW — the row's own seq and
 * state version. Those two are what let the server tell "the user read this"
 * from "an automatic read fired before the user marked it unread and arrived
 * after it". v1 can express neither, which is why marking something unread
 * there could be silently undone a moment later.
 */
export function useInboxRowMarkRead() {
  return useRowMutation({
    send: (row) =>
      isGroupRow(row)
        ? api.markInboxGroupRead(row.id, {
            seq: row.group?.seq ?? 0,
            stateVersion: row.group?.stateVersion ?? 0,
          })
        : api.markInboxRead(row.id),
    cacheKey: (row, wsId) =>
      isGroupRow(row) ? inboxV2Keys.list(wsId) : inboxKeys.list(wsId),
    patchV1: (rows, id) => rows?.map((r) => (r.id === id ? { ...r, read: true } : r)),
    patchV2: (page, id) => patchGroupPage(page, id, (i) => ({ ...i, unread: false })),
  });
}

/** Mark a row unread — durable, and outranking the next automatic read. */
export function useInboxRowMarkUnread() {
  return useRowMutation({
    send: (row) =>
      isGroupRow(row) ? api.markInboxGroupUnread(row.id) : api.markInboxUnread(row.id),
    cacheKey: (row, wsId) =>
      isGroupRow(row) ? inboxV2Keys.list(wsId) : inboxKeys.list(wsId),
    patchV1: (rows, id) => rows?.map((r) => (r.id === id ? { ...r, read: false } : r)),
    patchV2: (page, id) => patchGroupPage(page, id, (i) => ({ ...i, unread: true })),
    invalidateSummary: true,
  });
}

/**
 * Archive.
 *
 * v2 drops the row from the page outright — a group is archived or it is not,
 * and the two lists are mutually exclusive by construction. v1 can only flip
 * the boolean, because its list holds every event and the archived view filters
 * on it.
 */
export function useInboxRowArchive() {
  return useRowMutation({
    send: (row) =>
      isGroupRow(row) ? api.archiveInboxGroup(row.id) : api.archiveInbox(row.id),
    cacheKey: (row, wsId) =>
      isGroupRow(row) ? inboxV2Keys.list(wsId) : inboxKeys.list(wsId),
    patchV1: (rows, id) => rows?.map((r) => (r.id === id ? { ...r, archived: true } : r)),
    patchV2: (page, id) => dropFromGroupPage(page, id),
    invalidateSummary: true,
  });
}

export function useInboxRowUnarchive() {
  return useRowMutation({
    send: (row) =>
      isGroupRow(row) ? api.unarchiveInboxGroup(row.id) : api.unarchiveInbox(row.id),
    cacheKey: (row, wsId) =>
      isGroupRow(row) ? inboxV2Keys.archived(wsId) : inboxKeys.archived(wsId),
    patchV1: (rows, id) => rows?.map((r) => (r.id === id ? { ...r, archived: false } : r)),
    patchV2: (page, id) => dropFromGroupPage(page, id),
    invalidateSummary: true,
  });
}

export type InboxBatchOp =
  | "mark-all-read"
  | "archive-all"
  | "archive-all-read"
  | "archive-completed";

/**
 * The four bulk operations.
 *
 * `grouped` decides the endpoint rather than a row, because a batch has no row
 * to read it from. Not optimistic: the endpoints report how many rows they
 * touched, the client cannot predict that number, and guessing would make the
 * count flicker when the real one lands.
 */
export function useInboxRowBatch(grouped: boolean) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<void, Error, InboxBatchOp>({
    mutationFn: async (op) => {
      if (grouped) {
        switch (op) {
          case "mark-all-read":
            await api.markAllInboxGroupsRead();
            return;
          case "archive-all":
            await api.archiveAllInboxGroups();
            return;
          case "archive-all-read":
            await api.archiveAllReadInboxGroups();
            return;
          case "archive-completed":
            await api.archiveCompletedInboxGroups();
            return;
        }
      }
      switch (op) {
        case "mark-all-read":
          await api.markAllInboxRead();
          return;
        case "archive-all":
          await api.archiveAllInbox();
          return;
        case "archive-all-read":
          await api.archiveAllReadInbox();
          return;
        case "archive-completed":
          await api.archiveCompletedInbox();
          return;
      }
    },
    onSettled: () => {
      // Both generations, whichever ran: a batch on one side moves rows the
      // other side's cache is still holding, and the readiness fallback can
      // flip between them on the very next request.
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: inboxV2Keys.all(wsId) });
      qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
    },
  });
}

/** Every inbox write the page needs, in one call. */
export function useInboxRowActions(grouped: boolean) {
  return {
    markRead: useInboxRowMarkRead(),
    markUnread: useInboxRowMarkUnread(),
    archive: useInboxRowArchive(),
    unarchive: useInboxRowUnarchive(),
    batch: useInboxRowBatch(grouped),
  };
}
