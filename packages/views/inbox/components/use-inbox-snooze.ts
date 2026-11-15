"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { cachedInboxSnoozedUntil, useSnoozeInbox, useUnsnoozeInbox } from "@multica/core/inbox/mutations";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

/**
 * Snooze and bring back, with the toasts every surface shows alike. A snooze
 * can be undone from its toast: a first snooze returns the issue as read or
 * unread as it was before, a change of time puts back the previous time.
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

  const failed = useCallback(
    (fallback: string) => (err: unknown) => {
      toast.error(err instanceof Error && err.message ? err.message : fallback);
    },
    [],
  );

  const snooze = useCallback(
    (item: { id: string; read?: boolean }, until: Date) => {
      const wasRead = item.read === true;
      const previous = cachedInboxSnoozedUntil(qc, wsId, item.id);
      const undo = () => {
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
        failed(t(($) => $.snooze.failed)),
      );
    },
    [qc, wsId, snoozeAsync, unsnoozeAsync, failed, t, locale],
  );

  const unsnooze = useCallback(
    (itemId: string) => {
      unsnoozeAsync({ id: itemId }).then(
        () => toast.success(t(($) => $.snooze.toast_unsnoozed)),
        failed(t(($) => $.snooze.unsnooze_failed)),
      );
    },
    [unsnoozeAsync, failed, t],
  );

  return { snooze, unsnooze };
}
