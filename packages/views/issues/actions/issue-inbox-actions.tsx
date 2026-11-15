"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlarmClock, Archive, ChevronDown, CircleCheck } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { useArchiveInbox, type InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import { findSnoozedInboxIssue, snoozedInboxOptions } from "@multica/core/inbox/queries";
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
import { InboxSnoozeMenu } from "../../inbox/components/inbox-snooze-menu";
import { formatSnoozeTime } from "../../inbox/components/inbox-snooze-time";
import { useInboxSnooze } from "../../inbox/components/use-inbox-snooze";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useSidebarInboxIssue } from "../../inbox/components/use-sidebar-inbox-issue";
import { UnsubscribeArchiveButton } from "../../inbox/components/unsubscribe-archive-button";
import { useUnsubscribeArchive } from "../../inbox/components/use-unsubscribe-archive";
import { useNavigation } from "../../navigation";
import { useLocale, useT } from "../../i18n";

/**
 * The Inbox's actions in the header of an issue opened from the Work panel's
 * Inbox section, while the section still lists it. Marking it done,
 * snoozing, archiving or unsubscribing opens the next notification the
 * section lists, so the Inbox can be worked through without leaving the page.
 *
 * A snoozed issue shows when it comes back instead, wherever it is opened,
 * with the way to bring it back now or change the time.
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
  const { fromInbox } = useSidebarInboxIssue(wsId, issueId);
  const rememberSidebarIssue = useInboxSelectionStore((s) => s.rememberSidebarIssue);
  const { rows } = useFilteredInboxRows(wsId, issueId, fromInbox);
  const index = fromInbox ? rows.findIndex((row) => row.issue_id === issueId) : -1;
  const item = index >= 0 ? rows[index]! : null;
  const { mutateAsync: archiveAsync } = useArchiveInbox();
  const unsubscribeArchive = useUnsubscribeArchive();
  const { snooze, unsnooze } = useInboxSnooze();
  const { data: snoozedList } = useQuery(snoozedInboxOptions(wsId));
  const snoozed = item ? undefined : findSnoozedInboxIssue(snoozedList, issueId);
  const [snoozeOpen, setSnoozeOpen] = useState(false);

  const failed = (fallback: string) => (err: unknown) =>
    toast.error(err instanceof Error && err.message ? err.message : fallback);

  // Rows run newest first: move on to the older one, else the newer one.
  const openNext = (next: (typeof rows)[number] | null) => {
    if (!next) return;
    rememberSidebarIssue(wsId, next.issue_id ?? null);
    push(inboxRowHref(p, next));
  };

  // Toasts follow the request's promise: the next issue's page can replace
  // this header first, and React Query drops per-call callbacks with it.
  const archiveItem = () => {
    if (!item) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    archiveAsync(item.id).then(
      () => toast.success(t(($) => $.toasts.archived)),
      failed(t(($) => $.errors.archive_failed)),
    );
    openNext(next);
  };

  const unsubscribeItem = (scope: InboxUnsubscribeScope) => {
    if (!item?.issue_id) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    unsubscribeArchive({ id: item.id, issueId: item.issue_id }, scope);
    openNext(next);
  };

  const snoozeItem = (until: Date) => {
    if (!item) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    snooze(item, until);
    openNext(next);
  };

  // Done archives the issue's notifications on the server; archiving here as
  // well takes the row out at once instead of when that event arrives.
  const markDone = () => {
    if (!item || !onMarkDone) return;
    const next = rows[index + 1] ?? rows[index - 1] ?? null;
    onMarkDone(() => {
      archiveAsync(item.id).catch(failed(t(($) => $.errors.archive_failed)));
      openNext(next);
    });
  };

  const archiveRef = useRef<(() => void) | null>(null);
  const snoozeRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    archiveRef.current = item ? archiveItem : null;
    snoozeRef.current = item
      ? () => setSnoozeOpen(true)
      : snoozed
        ? () => unsnooze(snoozed.item_id)
        : null;
  });
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      const run = shortcutMatchesEvent(getShortcut("archiveInboxItem"), event)
        ? archiveRef.current
        : shortcutMatchesEvent(getShortcut("snoozeInboxItem"), event)
          ? snoozeRef.current
          : null;
      if (!run) return;
      event.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  if (!item) {
    if (!snoozed) return null;
    return (
      <SnoozedChip
        until={new Date(snoozed.snoozed_until)}
        open={snoozeOpen}
        onOpenChange={setSnoozeOpen}
        onSnooze={(until) => snooze({ id: snoozed.item_id }, until)}
        onUnsnooze={() => unsnooze(snoozed.item_id)}
      />
    );
  }

  return (
    <>
      <UnsubscribeArchiveButton issueId={issueId} userId={item.recipient_id} onConfirm={unsubscribeItem} />
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
      <InboxSnoozeMenu
        open={snoozeOpen}
        onOpenChange={setSnoozeOpen}
        onSnooze={snoozeItem}
        trigger={
          <IconButton
            label={t(($) => $.snooze.action)}
            shortcut={<ActionShortcutKeycaps action="snoozeInboxItem" />}
            tooltipSide="bottom"
            className="text-muted-foreground"
          >
            <AlarmClock />
          </IconButton>
        }
      />
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

/** When a snoozed issue comes back, with the way to bring it back now or later. */
function SnoozedChip({
  until,
  open,
  onOpenChange,
  onSnooze,
  onUnsnooze,
}: {
  until: Date;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSnooze: (until: Date) => void;
  onUnsnooze: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const label = t(($) => $.snooze.back_at, { time: formatSnoozeTime(until, new Date(), locale) });
  return (
    <>
      <InboxSnoozeMenu
        open={open}
        onOpenChange={onOpenChange}
        snoozed
        onSnooze={onSnooze}
        onUnsnooze={onUnsnooze}
        trigger={
          <button
            type="button"
            title={label}
            className={cn(
              "inline-flex h-7 max-w-72 min-w-0 items-center gap-1.5 rounded-md border border-border px-2.5 text-caption text-foreground",
              "hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-accent",
            )}
          >
            <AlarmClock className="size-3.5 shrink-0" aria-hidden="true" />
            <span className="min-w-0 truncate max-sm:hidden">{label}</span>
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>
        }
      />
      <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />
    </>
  );
}
