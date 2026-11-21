"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { takeInboxAutoArchive } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import type { InboxItem } from "@multica/core/types";
import { useT } from "../../i18n";
import { useRestoreInbox } from "./use-leave-inbox-row";

/**
 * Opens the next notification once the open one is archived because the user
 * handled its issue (`takeInboxAutoArchive`), when their `after_auto_archive`
 * preference says "next", and confirms it with a toast that can undo it.
 * Otherwise the row stays in place, settled, as `keepOpenInboxRow` keeps it.
 *
 * Only the focused window moves on: acting in another window or tab must not
 * pull this one away from what it shows.
 */
export function useMoveOnAfterAutoArchive({
  settled,
  next,
  onNext,
}: {
  settled: KeptInboxRow | null;
  next: InboxItem | null;
  onNext: () => void;
}) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const movesOn = notificationPreferenceValue(data?.preferences ?? {}, "after_auto_archive") === "next";
  const restore = useRestoreInbox();
  const archivedKey = settled?.reason === "archived" ? settled.key : null;

  const latest = useRef({ settled, next, onNext, movesOn });
  useEffect(() => {
    latest.current = { settled, next, onNext, movesOn };
  });

  useEffect(() => {
    if (!archivedKey) return;
    const { settled: row, next: nextRow, onNext: moveOn, movesOn: moves } = latest.current;
    if (!takeInboxAutoArchive(archivedKey)) return;
    if (!moves || !row || !nextRow || !document.hasFocus()) return;
    moveOn();
    toast.success(t(($) => $.toasts.archived), {
      action: {
        label: t(($) => $.toasts.undo),
        onClick: () => {
          useInboxSelectionStore.getState().returnRow(wsId, archivedKey);
          void restore(row.item.id, { quiet: true });
        },
      },
    });
  }, [archivedKey, wsId, restore, t]);
}
