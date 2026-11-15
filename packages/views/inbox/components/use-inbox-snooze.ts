"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { useSnoozeInbox, useUnsnoozeInbox } from "@multica/core/inbox/mutations";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

/**
 * Snooze and bring back, with the toasts every surface shows alike. A snooze
 * can be undone from its toast, which returns the issue as read or unread as
 * it was before.
 */
export function useInboxSnooze() {
  const { t } = useT("inbox");
  const locale = useLocale();
  const snoozeMutation = useSnoozeInbox();
  const unsnoozeMutation = useUnsnoozeInbox();
  const snoozeMutate = snoozeMutation.mutate;
  const unsnoozeMutate = unsnoozeMutation.mutate;

  const failed = useCallback(
    (fallback: string) => (err: unknown) =>
      toast.error(err instanceof Error && err.message ? err.message : fallback),
    [],
  );

  const snooze = useCallback(
    (item: { id: string; read?: boolean }, until: Date) => {
      const wasRead = item.read === true;
      snoozeMutate(
        { id: item.id, until },
        {
          onSuccess: () =>
            toast.success(t(($) => $.snooze.toast_snoozed, { time: formatSnoozeTime(until, new Date(), locale) }), {
              action: {
                label: t(($) => $.snooze.undo),
                onClick: () =>
                  unsnoozeMutate(
                    { id: item.id, read: wasRead },
                    { onError: failed(t(($) => $.snooze.unsnooze_failed)) },
                  ),
              },
            }),
          onError: failed(t(($) => $.snooze.failed)),
        },
      );
    },
    [snoozeMutate, unsnoozeMutate, failed, t, locale],
  );

  const unsnooze = useCallback(
    (itemId: string) =>
      unsnoozeMutate(
        { id: itemId },
        {
          onSuccess: () => toast.success(t(($) => $.snooze.toast_unsnoozed)),
          onError: failed(t(($) => $.snooze.unsnooze_failed)),
        },
      ),
    [unsnoozeMutate, failed, t],
  );

  return { snooze, unsnooze };
}
