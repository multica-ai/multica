"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { cachedInboxSnoozedUntil, useSnoozeInbox, useUnsnoozeInbox } from "@multica/core/inbox/mutations";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

/**
 * Snooze and bring back, with the toasts every surface shows alike. A snooze
 * can be undone from its toast: a first snooze returns the issue as read or
 * unread as it was before, a change of time puts back the previous time.
 *
 * Snoozing the open notification leaves its row (`useLeaveInboxRow`); an
 * undo, a failed snooze or bringing it back returns the row, so a row the user
 * stayed on can be kept in place again.
 *
 * The toasts follow the request's promise rather than per-call mutation
 * callbacks, which React Query drops once the calling component unmounts, as
 * it does when snoozing opens the next issue.
 */
export function useInboxSnooze() {
  const { t } = useT("inbox");
  const locale = useLocale();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { mutateAsync: snoozeAsync } = useSnoozeInbox();
  const { mutateAsync: unsnoozeAsync } = useUnsnoozeInbox();

  const returnRow = useCallback(
    (key: string) => useInboxSelectionStore.getState().returnRow(wsId, key),
    [wsId],
  );

  const failed = useCallback(
    (fallback: string) => (err: unknown) => {
      toast.error(err instanceof Error && err.message ? err.message : fallback);
    },
    [],
  );

  const snooze = useCallback(
    (item: { id: string; issue_id?: string | null; read?: boolean }, until: Date) => {
      const wasRead = item.read === true;
      const previous = cachedInboxSnoozedUntil(qc, wsId, item.id);
      const key = item.issue_id ?? item.id;
      const undo = () => {
        returnRow(key);
        if (previous && previous.getTime() > Date.now()) {
          snoozeAsync({ id: item.id, until: previous }).catch(failed(t(($) => $.snooze.failed)));
        } else {
          unsnoozeAsync({ id: item.id, read: wasRead }).catch(failed(t(($) => $.snooze.unsnooze_failed)));
        }
      };
      snoozeAsync({ id: item.id, until }).then(
        () =>
          toast.success(t(($) => $.snooze.toast_snoozed, { time: formatSnoozeTime(until, new Date(), locale) }), {
            action: { label: t(($) => $.snooze.undo), onClick: undo },
          }),
        (err: unknown) => {
          returnRow(key);
          failed(t(($) => $.snooze.failed))(err);
        },
      );
    },
    [qc, wsId, snoozeAsync, unsnoozeAsync, returnRow, failed, t, locale],
  );

  /** `key` is the row of the issue coming back, which snoozing it may have left. */
  const unsnooze = useCallback(
    (itemId: string, key: string) => {
      returnRow(key);
      unsnoozeAsync({ id: itemId }).then(
        () => toast.success(t(($) => $.snooze.toast_unsnoozed)),
        failed(t(($) => $.snooze.unsnooze_failed)),
      );
    },
    [unsnoozeAsync, returnRow, failed, t],
  );

  return { snooze, unsnooze };
}
