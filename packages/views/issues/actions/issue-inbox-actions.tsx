"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { Archive, Check, CircleCheck, CircleDot } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useArchiveInbox, useMarkInboxRead, useMarkInboxUnread } from "@multica/core/inbox/mutations";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { inboxRowHref } from "../../inbox/components/inbox-display";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

/**
 * The Inbox's actions in the header of an issue opened from the Work panel's
 * Inbox section, while the section still lists it. Marking it done or
 * archiving opens the next notification the section lists, so the Inbox can
 * be worked through without leaving the page.
 *
 * `onMarkDone` sets the issue done and calls back once that is saved; it is
 * offered while the issue is not done or closed.
 */
export function IssueInboxActions({
  issueId,
  onMarkDone,
}: {
  issueId: string;
  onMarkDone?: (onSaved: () => void) => void;
}) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const fromInbox = useInboxSelectionStore((s) => s.sidebarIssueByWorkspace[wsId] === issueId);
  const rememberSidebarIssue = useInboxSelectionStore((s) => s.rememberSidebarIssue);
  const { rows } = useFilteredInboxRows(wsId, issueId, fromInbox);
  const index = fromInbox ? rows.findIndex((row) => row.issue_id === issueId) : -1;
  const item = index >= 0 ? rows[index]! : null;
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const archive = useArchiveInbox();

  const failed = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);

  // Rows run newest first: move on to the older one, else the newer one.
  const openNext = (next: (typeof rows)[number] | null) => {
    if (!next) return;
    rememberSidebarIssue(wsId, next.issue_id ?? null);
    push(inboxRowHref(p, next));
  };

  const archiveItem = () => {
    if (!item) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    archive.mutate(item.id, {
      onSuccess: () => toast.success(t(($) => $.toasts.archived)),
      onError: failed(t(($) => $.errors.archive_failed)),
    });
    openNext(next);
  };

  // Done archives the issue's notifications on the server; archiving here as
  // well takes the row out at once instead of when that event arrives.
  const markDone = () => {
    if (!item || !onMarkDone) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    onMarkDone(() => {
      archive.mutate(item.id, { onError: failed(t(($) => $.errors.archive_failed)) });
      openNext(next);
    });
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
      {onMarkDone && (
        <IconButton
          label={tIssues(($) => $.detail.mark_done_tooltip)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={markDone}
        >
          <CircleCheck />
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
