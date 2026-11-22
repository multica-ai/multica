"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { type InboxBarAction, takeInboxAutoArchive, takeInboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import { isEditableShortcutTarget } from "@multica/core/shortcuts";
import type { InboxItem, Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import { toastInboxUndo } from "./inbox-undo-toast";
import { useRestoreInbox } from "./use-leave-inbox-row";

/**
 * Brief confirmation beat before auto-advancing after an issue action
 * auto-archived the open notification (MUL-9865). Long enough to see the
 * status/comment take effect and read the settled InboxBar cause, short enough
 * to keep hands-free triage moving; any interaction cancels it immediately.
 */
export const AUTO_ARCHIVE_ADVANCE_DELAY_MS = 1_400;

export interface AutoAdvanceBuffer {
  /** Total countdown length in milliseconds, driving the 2px progress line. */
  durationMs: number;
  /** Cancel the countdown and stay on the settled issue. */
  cancel: () => void;
}

/**
 * Opens the next notification after a brief, cancellable buffer once the open
 * one is archived because the user handled its issue (`takeInboxAutoArchive`),
 * when their `after_auto_archive` preference says "next", and confirms it with
 * a toast that can undo it. Otherwise the row stays in place, settled, as
 * `keepOpenInboxRow` keeps it.
 *
 * Only the focused window moves on: acting in another window or tab must not
 * pull this one away from what it shows.
 *
 * Returns the Inbox bar action that archived the settled row (when taken in
 * this window) and the active `autoAdvance` buffer while the countdown runs.
 */
export function useMoveOnAfterAutoArchive({
  settled,
  next,
  onNext,
}: {
  settled: KeptInboxRow | null;
  next: InboxItem | null;
  onNext: () => void;
}): { cause: InboxBarAction | null; autoAdvance: AutoAdvanceBuffer | null } {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const movesOn = notificationPreferenceValue(data?.preferences ?? {}, "after_auto_archive") === "next";
  const restore = useRestoreInbox();
  const qc = useQueryClient();
  const archivedKey = settled?.reason === "archived" ? settled.key : null;
  const [cause, setCause] = useState<{ key: string; action: InboxBarAction } | null>(null);
  const [pendingKey, setPendingKey] = useState<string | null>(null);

  const latest = useRef({ settled, next, onNext, movesOn });
  useLayoutEffect(() => {
    latest.current = { settled, next, onNext, movesOn };
  });

  // Before paint, so the settled bar shows the real action cause and starts
  // the 2px progress hairline on its very first frame.
  useLayoutEffect(() => {
    if (!archivedKey) {
      setPendingKey(null);
      return;
    }
    const { settled: row, movesOn: moves } = latest.current;
    const own = takeInboxAutoArchive(archivedKey);
    const action = takeInboxBarAction(archivedKey);
    const shown = own && action && reflects(qc.getQueryData(issueDetailOptions(wsId, archivedKey).queryKey), action);
    setCause(shown ? { key: archivedKey, action } : null);
    if (!own || !moves || !row || !document.hasFocus()) {
      setPendingKey(null);
      return;
    }
    setPendingKey(archivedKey);
  }, [archivedKey, wsId, qc]);

  const cancel = useCallback(() => {
    setPendingKey(null);
  }, []);

  const activeKey = pendingKey && pendingKey === archivedKey ? archivedKey : null;

  // Run the 1.4s countdown and cancel immediately on any user interaction in
  // the page (Escape, scrolling, clicking outside the InboxBar, or focusing an
  // editable field) so auto-advance never pulls the rug out mid-reading.
  useEffect(() => {
    if (!activeKey) return;
    const timer = window.setTimeout(() => {
      const { settled: row, onNext: moveOn } = latest.current;
      setPendingKey(null);
      if (!row || row.key !== activeKey || !document.hasFocus()) return;
      moveOn();
      toastInboxUndo(t(($) => $.toasts.archived), {
        label: t(($) => $.toasts.undo),
        undo: () => {
          const store = useInboxSelectionStore.getState();
          store.returnRow(wsId, activeKey);
          store.clearLastCleared(wsId, activeKey);
          void restore(row.item.id, { quiet: true });
        },
      });
    }, AUTO_ARCHIVE_ADVANCE_DELAY_MS);

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setPendingKey(null);
      }
    };
    const onWheel = () => setPendingKey(null);
    const onPointerDown = (event: PointerEvent) => {
      const el = event.target instanceof Element ? event.target : null;
      if (el?.closest("[data-inbox-bar]")) return;
      setPendingKey(null);
    };
    const onFocusIn = (event: FocusEvent) => {
      if (isEditableShortcutTarget(event.target)) setPendingKey(null);
    };

    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("wheel", onWheel, { passive: true });
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("focusin", onFocusIn, true);

    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("wheel", onWheel);
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("focusin", onFocusIn, true);
    };
  }, [activeKey, wsId, restore, t]);

  const autoAdvance = useMemo<AutoAdvanceBuffer | null>(
    () => (activeKey ? { durationMs: AUTO_ARCHIVE_ADVANCE_DELAY_MS, cancel } : null),
    [activeKey, cancel],
  );

  return {
    cause: cause && cause.key === archivedKey ? cause.action : null,
    autoAdvance,
  };
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
