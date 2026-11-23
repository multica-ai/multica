"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  type InboxAutoArchiveReason,
  type InboxBarAction,
  takeInboxAutoArchiveReason,
  takeInboxBarAction,
} from "@multica/core/inbox/auto-archive";
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
 * the user handled its issue (`takeInboxAutoArchiveReason`), when their
 * `after_auto_archive` preference says "next", and confirms it with a toast
 * that can undo it. Triggering an agent (`reason === "agent_triggered"`) always
 * stays in place so the user sees the run start and returns to the Inbox when
 * the agent finishes. Otherwise the row stays in place, settled, as
 * `keepOpenInboxRow` keeps it.
 *
 * Only the focused window moves on: acting in another window or tab must not
 * pull this one away from what it shows.
 *
 * Returns the Inbox bar action that archived the settled row (when taken in
 * this window) and the user-caused auto-archive reason, if any.
 */
export function useMoveOnAfterAutoArchive({
  settled,
  next,
  onNext,
}: {
  settled: KeptInboxRow | null;
  next: InboxItem | null;
  onNext: () => void;
}): { cause: InboxBarAction | null; reason: InboxAutoArchiveReason | null } {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const movesOn = notificationPreferenceValue(data?.preferences ?? {}, "after_auto_archive") === "next";
  const restore = useRestoreInbox();
  const qc = useQueryClient();
  const archivedKey = settled?.reason === "archived" ? settled.key : null;
  const [settledMeta, setSettledMeta] = useState<{
    key: string;
    action: InboxBarAction | null;
    reason: InboxAutoArchiveReason | null;
  } | null>(null);

  const latest = useRef({ settled, next, onNext, movesOn });
  useLayoutEffect(() => {
    latest.current = { settled, next, onNext, movesOn };
  });

  // Before paint, so the settled bar never flashes before the next issue opens.
  useLayoutEffect(() => {
    if (!archivedKey) return;
    const { settled: row, onNext: moveOn, movesOn: moves } = latest.current;
    const reason = takeInboxAutoArchiveReason(archivedKey);
    const own = reason !== null;
    const action = takeInboxBarAction(archivedKey);
    const shown = own && action && reflects(qc.getQueryData(issueDetailOptions(wsId, archivedKey).queryKey), action);
    setSettledMeta({ key: archivedKey, action: shown ? action : null, reason });
    if (!own || reason === "agent_triggered" || !moves || !row || !document.hasFocus()) return;
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

  if (!settledMeta || settledMeta.key !== archivedKey) {
    return { cause: null, reason: null };
  }
  return { cause: settledMeta.action, reason: settledMeta.reason };
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
