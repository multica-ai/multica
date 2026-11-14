"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, Check, CircleDot } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { deduplicateInboxItems, inboxListOptions } from "@multica/core/inbox/queries";
import { useArchiveInbox, useMarkInboxRead, useMarkInboxUnread } from "@multica/core/inbox/mutations";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import type { InboxItem } from "@multica/core/types";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { inboxRowHref } from "../../inbox/components/inbox-display";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const EMPTY_INBOX: InboxItem[] = [];

/**
 * The Inbox's actions in the header of an issue opened from the Work panel's
 * Inbox section, while the section still lists it. Archiving opens the next
 * notification, so the Inbox can be worked through without leaving the page.
 */
export function IssueInboxActions({ issueId }: { issueId: string }) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const fromInbox = useInboxSelectionStore((s) => s.sidebarIssueByWorkspace[wsId] === issueId);
  const rememberSidebarIssue = useInboxSelectionStore((s) => s.rememberSidebarIssue);
  const { data: raw = EMPTY_INBOX } = useQuery({ ...inboxListOptions(wsId), enabled: fromInbox });
  const rows = useMemo(() => deduplicateInboxItems(raw), [raw]);
  const index = fromInbox ? rows.findIndex((row) => row.issue_id === issueId) : -1;
  const item = index >= 0 ? rows[index]! : null;
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const archive = useArchiveInbox();

  const failed = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);

  const archiveItem = () => {
    if (!item) return;
    // Rows run newest first: move on to the older one, else the newer one.
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    archive.mutate(item.id, {
      onSuccess: () => toast.success(t(($) => $.toasts.archived)),
      onError: failed(t(($) => $.errors.archive_failed)),
    });
    if (!next) return;
    rememberSidebarIssue(wsId, next.issue_id ?? null);
    push(inboxRowHref(p, next));
  };

  const archiveRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    archiveRef.current = item ? archiveItem : null;
  });
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      if (!shortcutMatchesEvent(getShortcut("archiveInboxItem"), event)) return;
      const run = archiveRef.current;
      if (!run) return;
      event.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  if (!item) return null;
  const unread = item.read !== true;

  return (
    <>
      {unread ? (
        <IconButton
          label={t(($) => $.context_menu.mark_read)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => markRead.mutate(item.id, { onError: failed(t(($) => $.errors.mark_read_failed)) })}
        >
          <Check />
        </IconButton>
      ) : (
        <IconButton
          label={t(($) => $.context_menu.mark_unread)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => markUnread.mutate(item.id, { onError: failed(t(($) => $.errors.mark_unread_failed)) })}
        >
          <CircleDot />
        </IconButton>
      )}
      <IconButton
        label={t(($) => $.context_menu.archive)}
        shortcut={<ActionShortcutKeycaps action="archiveInboxItem" />}
        tooltipSide="bottom"
        className="text-muted-foreground"
        onClick={archiveItem}
      >
        <Archive />
      </IconButton>
      <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />
    </>
  );
}
