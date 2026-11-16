"use client";

import { useLayoutEffect, useMemo } from "react";
import { inboxRowKey, keepOpenInboxRow } from "@multica/core/inbox/open-row";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import type { InboxItem } from "@multica/core/types";

/**
 * `rows` with the open notification's row kept in place once something other
 * than the user's own Inbox action takes it out (`keepOpenInboxRow`). `list`
 * names the list the place is kept in; lists that show the same rows share it.
 * `filters` are the ones `rows` were filtered with, compared by identity.
 *
 * `enabled: false` neither holds nor keeps, for a caller whose open issue was
 * not opened from this list.
 */
export function useKeptOpenInboxRow(
  wsId: string,
  list: string,
  rows: InboxItem[],
  notifications: readonly InboxItem[],
  openKey: string | null,
  filters: object,
  enabled = true,
) {
  const index = enabled && openKey ? rows.findIndex((row) => inboxRowKey(row) === openKey) : -1;
  const item = index >= 0 ? rows[index] : undefined;
  const held = useInboxSelectionStore((s) => (enabled ? s.heldRows[`${wsId}:${list}`] : undefined));
  const left = useInboxSelectionStore((s) => !!openKey && s.leftRowKeyByWorkspace[wsId] === openKey);
  const holdRow = useInboxSelectionStore((s) => s.holdRow);
  useLayoutEffect(() => {
    if (wsId && openKey && item) holdRow(wsId, list, { key: openKey, item, index, filters });
  }, [wsId, list, openKey, item, index, filters, holdRow]);
  return useMemo(
    () => (left ? { rows, kept: null } : keepOpenInboxRow(rows, notifications, held, openKey, filters)),
    [left, rows, notifications, held, openKey, filters],
  );
}
