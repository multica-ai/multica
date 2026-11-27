"use client";

import { useCallback } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { undoArchiveInboxBatch, useArchiveInbox, useUnarchiveInbox } from "@multica/core/inbox/mutations";
import { inboxRowKey } from "@multica/core/inbox/open-row";
import { inboxKeys } from "@multica/core/inbox/queries";
import { useInboxSelectionStore, type LastInboxCleared } from "@multica/core/inbox/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { getInboxDisplayTitle, inboxRowHref } from "./inbox-display";
import { toastInboxUndo } from "./inbox-undo-toast";

/** Short label for the Inbox Zero undo line: issue identifier when present, else title. */
export function clearedLabelForItem(item: InboxItem): string {
  return item.issue_identifier || getInboxDisplayTitle(item);
}

/** Collect one notification ID per inbox row matched by a batch archive predicate. */
export function collectBatchArchiveIds(
  qc: QueryClient,
  wsId: string,
  archives: (item: InboxItem) => boolean,
): string[] {
  const taken = new Map<string, string>();
  for (const item of qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId)) ?? []) {
    if (!item.archived && archives(item) && !taken.has(inboxRowKey(item))) taken.set(inboxRowKey(item), item.id);
  }
  return [...taken.values()];
}

/**
 * Leave the open notification for `next`, as every Inbox action on it does:
 * archiving, snoozing or unsubscribing it from the Inbox bar, its sidebar row
 * or that row's card, or a batch archive that takes it. The row leaves its
 * lists for good rather than staying in place (`keepOpenInboxRow`). When
 * nothing is left to open, the Inbox page shows the Inbox Zero (or
 * filtered-empty) state, carrying the cleared entry for inline undo.
 */
export function useLeaveInboxRow() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const leaveRow = useInboxSelectionStore((s) => s.leaveRow);
  const recordLastCleared = useInboxSelectionStore((s) => s.recordLastCleared);
  const recordTransition = useInboxSelectionStore((s) => s.recordTransition);
  return useCallback(
    (key: string, next: InboxItem | null, cleared?: LastInboxCleared) => {
      leaveRow(wsId, key);
      if (!next) {
        recordTransition(wsId, "", "next");
        if (cleared) recordLastCleared(wsId, cleared);
        push(p.inbox());
        return;
      }
      recordTransition(wsId, inboxRowKey(next), "next");
      push(inboxRowHref(p, next));
    },
    [wsId, p, push, leaveRow, recordLastCleared, recordTransition],
  );
}

const errorMessage = (err: unknown, fallback: string) =>
  err instanceof Error && err.message ? err.message : fallback;

/**
 * Archive a notification and confirm it with a toast that can undo it, the
 * same on every surface. `key` is its row's, which the archive may have left
 * (`useLeaveInboxRow`): undoing or failing returns it, so a row the user
 * stayed on can be kept in place again. The toasts follow the request's
 * promise rather than per-call mutation callbacks, which React Query drops
 * once the calling component unmounts, as it does when archiving opens the
 * next issue.
 */
export function useArchiveInboxWithUndo() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { mutateAsync: archiveAsync } = useArchiveInbox();
  const restore = useRestoreInbox();
  return useCallback(
    (id: string, key: string) => {
      const returnRow = () => {
        const store = useInboxSelectionStore.getState();
        store.returnRow(wsId, key);
        store.clearLastCleared(wsId, key);
      };
      return archiveAsync(id).then(
        () =>
          toastInboxUndo(t(($) => $.toasts.archived), {
            label: t(($) => $.toasts.undo),
            undo: () => {
              returnRow();
              void restore(id, { quiet: true });
            },
          }),
        (err: unknown) => {
          returnRow();
          toast.error(errorMessage(err, t(($) => $.errors.archive_failed)));
        },
      );
    },
    [wsId, archiveAsync, restore, t],
  );
}

/**
 * Run a batch archive and confirm it with a toast that can undo it. `archives`
 * names the notifications the batch takes, read from the Inbox as it is when
 * the batch starts, since the server reports only how many it took. `openKey`
 * is the open row's, which the batch may have left; undoing or failing
 * returns it.
 */
export function useArchiveInboxBatchWithUndo() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  return useCallback(
    (
      archive: () => Promise<{ count: number }>,
      archives: (item: InboxItem) => boolean,
      failed: string,
      openKey: string | null,
    ) => {
      const takenIds = collectBatchArchiveIds(qc, wsId, archives);
      const returnRow = () => {
        const store = useInboxSelectionStore.getState();
        if (openKey) store.returnRow(wsId, openKey);
        if (store.lastClearedByWorkspace[wsId]?.kind === "batch_archived") {
          store.clearLastCleared(wsId);
        }
      };
      return archive().then(
        ({ count }) => {
          if (count === 0) return;
          toastInboxUndo(t(($) => $.toasts.archived_many, { count }), {
            label: t(($) => $.toasts.undo),
            undo: () => {
              returnRow();
              undoArchiveInboxBatch(qc, wsId, takenIds).catch((err: unknown) =>
                toast.error(errorMessage(err, t(($) => $.errors.unarchive_failed))),
              );
            },
          });
        },
        (err: unknown) => {
          returnRow();
          toast.error(errorMessage(err, failed));
        },
      );
    },
    [wsId, qc, t],
  );
}

/**
 * Put an archived notification back in the Inbox. `quiet` skips the toast, for
 * an undo. Resolves whether it went back; a failure is already toasted.
 */
export function useRestoreInbox() {
  const { t } = useT("inbox");
  const { mutateAsync: unarchiveAsync } = useUnarchiveInbox();
  return useCallback(
    (id: string, { quiet = false }: { quiet?: boolean } = {}): Promise<boolean> =>
      unarchiveAsync(id).then(
        () => {
          if (!quiet) toast.success(t(($) => $.toasts.unarchived));
          return true;
        },
        (err: unknown) => {
          toast.error(errorMessage(err, t(($) => $.errors.unarchive_failed)));
          return false;
        },
      ),
    [unarchiveAsync, t],
  );
}
