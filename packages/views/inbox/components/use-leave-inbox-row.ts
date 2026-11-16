"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useArchiveInbox, useUnarchiveInbox } from "@multica/core/inbox/mutations";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { inboxRowHref } from "./inbox-display";

/**
 * Leave the open notification for `next`, as every Inbox action on it does:
 * marking it done, archiving, snoozing or unsubscribing it from the issue
 * header, its sidebar row or that row's card, or a batch archive that takes
 * it. The row leaves its lists for good rather than staying in place
 * (`keepOpenInboxRow`), and with nothing left to open the issue stays, lit
 * nowhere.
 */
export function useLeaveInboxRow() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const leaveRow = useInboxSelectionStore((s) => s.leaveRow);
  const rememberSidebarIssue = useInboxSelectionStore((s) => s.rememberSidebarIssue);
  return useCallback(
    (key: string, next: InboxItem | null) => {
      leaveRow(wsId, key);
      if (!next) return;
      rememberSidebarIssue(wsId, next.issue_id ?? null);
      push(inboxRowHref(p, next));
    },
    [wsId, p, push, leaveRow, rememberSidebarIssue],
  );
}

/**
 * Archive a notification and confirm it with a toast that can undo it, the
 * same on every surface. The toasts follow the request's promise rather than
 * per-call mutation callbacks, which React Query drops once the calling
 * component unmounts, as it does when archiving opens the next issue.
 */
export function useArchiveInboxWithUndo() {
  const { t } = useT("inbox");
  const { mutateAsync: archiveAsync } = useArchiveInbox();
  const restore = useRestoreInbox();
  return useCallback(
    (id: string) =>
      archiveAsync(id).then(
        () =>
          toast.success(t(($) => $.toasts.archived), {
            action: { label: t(($) => $.toasts.undo), onClick: () => restore(id, { quiet: true }) },
          }),
        (err: unknown) =>
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.errors.archive_failed)),
      ),
    [archiveAsync, restore, t],
  );
}

/** Put an archived notification back in the Inbox. `quiet` skips the toast, for an undo. */
export function useRestoreInbox() {
  const { t } = useT("inbox");
  const { mutateAsync: unarchiveAsync } = useUnarchiveInbox();
  return useCallback(
    (id: string, { quiet = false }: { quiet?: boolean } = {}) =>
      unarchiveAsync(id).then(
        () => {
          if (!quiet) toast.success(t(($) => $.toasts.unarchived));
        },
        (err: unknown) =>
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.errors.unarchive_failed)),
      ),
    [unarchiveAsync, t],
  );
}
