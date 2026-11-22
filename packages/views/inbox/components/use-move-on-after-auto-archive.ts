"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { type InboxBarAction, takeInboxAutoArchive, takeInboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import type { InboxItem, Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import { toastInboxUndo } from "./inbox-undo-toast";
import { useRestoreInbox } from "./use-leave-inbox-row";

/**
 * Opens the next notification right away once the open one is archived because
 * the user handled its issue (`takeInboxAutoArchive`), when their
 * `after_auto_archive` preference says "next", and confirms it with a toast
 * that can undo it. Otherwise the row stays in place, settled, as
 * `keepOpenInboxRow` keeps it.
 *
 * Only the focused window moves on: acting in another window or tab must not
 * pull this one away from what it shows.
 *
 * Returns the Inbox bar action that archived the settled row, when taken in
 * this window.
 */
export function useMoveOnAfterAutoArchive({
  settled,
  next,
  onNext,
}: {
  settled: KeptInboxRow | null;
  next: InboxItem | null;
  onNext: () => void;
}): InboxBarAction | null {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const movesOn = notificationPreferenceValue(data?.preferences ?? {}, "after_auto_archive") === "next";
  const restore = useRestoreInbox();
  const qc = useQueryClient();
  const archivedKey = settled?.reason === "archived" ? settled.key : null;
  const [cause, setCause] = useState<{ key: string; action: InboxBarAction } | null>(null);

  const latest = useRef({ settled, next, onNext, movesOn });
  useLayoutEffect(() => {
    latest.current = { settled, next, onNext, movesOn };
  });

  // Before paint, so the settled bar never flashes before the next issue opens.
  useLayoutEffect(() => {
    if (!archivedKey) return;
    const { settled: row, onNext: moveOn, movesOn: moves } = latest.current;
    const own = takeInboxAutoArchive(archivedKey);
    const action = takeInboxBarAction(archivedKey);
    const shown = own && action && reflects(qc.getQueryData(issueDetailOptions(wsId, archivedKey).queryKey), action);
    setCause(shown ? { key: archivedKey, action } : null);
    if (!own || !moves || !row || !document.hasFocus()) return;
    moveOn();
    toastInboxUndo(t(($) => $.toasts.archived), {
      label: t(($) => $.toasts.undo),
      undo: () => {
        const store = useInboxSelectionStore.getState();
        store.returnRow(wsId, archivedKey);
        store.clearLastCleared(wsId, archivedKey);
        void restore(row.item.id, { quiet: true });
      },
    });
  }, [archivedKey, wsId, restore, t, qc]);

  return cause && cause.key === archivedKey ? cause.action : null;
}

/**
 * Whether the issue shows the change the bar asked for. Agent assignments
 * confirm first, and one dismissed there never happened.
 */
function reflects(issue: Issue | undefined, action: InboxBarAction): boolean {
  if (!issue) return true;
  switch (action.type) {
    case "status":
      return issue.status === action.status;
    case "assignee":
      return (issue.assignee_id ?? null) === (action.assignee?.id ?? null);
    case "quick_action":
      return true;
  }
}
