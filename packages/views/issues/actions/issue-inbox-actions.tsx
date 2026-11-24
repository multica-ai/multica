"use client";

import { useRef, type ReactNode, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlarmClock, Archive, CalendarClock } from "@multica/ui/icons";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useMarkInboxRead,
  useMarkInboxUnread,
  type InboxUnsubscribeScope,
} from "@multica/core/inbox/mutations";
import {
  useInboxSelectionStore,
  type LastInboxCleared,
} from "@multica/core/inbox/selection-store";
import type { InboxItem } from "@multica/core/types";
import { findSnoozedInboxIssue, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { nextInboxRow } from "@multica/core/inbox/open-row";
import { useWorkspacePaths } from "@multica/core/paths";
import { InboxArchiveButton, InboxBar, InboxBarButton, InboxBarStatus, InboxOpenBar } from "../../inbox/components/inbox-bar";
import { inboxRowHref } from "../../inbox/components/inbox-display";
import { InboxKeptActions, InboxNextButton, useKeptInboxRowText } from "../../inbox/components/inbox-kept-actions";
import { InboxMoreMenu } from "../../inbox/components/inbox-more-menu";
import { InboxSnoozeMenu } from "../../inbox/components/inbox-snooze-menu";
import { formatSnoozeTime } from "../../inbox/components/inbox-snooze-time";
import { useInboxShortcut } from "../../inbox/components/use-inbox-shortcut";
import { useInboxSnooze } from "../../inbox/components/use-inbox-snooze";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useUnsubscribeArchive } from "../../inbox/components/use-unsubscribe-archive";
import { useAutoArchiveCause } from "../../inbox/components/use-auto-archive-cause";
import {
  clearedLabelForItem,
  useArchiveInboxWithUndo,
  useLeaveInboxRow,
  useRestoreInbox,
} from "../../inbox/components/use-leave-inbox-row";
import { useLocale, useT } from "../../i18n";

/**
 * The Inbox page's floating group over an open issue notification:
 * `issueActions` (its status, assignee and quick actions), then the
 * notification's own. Snoozing, archiving or unsubscribing opens the next
 * notification the Work panel's Inbox section lists, so the Inbox can be
 * worked through without leaving the page.
 *
 * Once the notification leaves the Inbox on its own (the issue reached Done,
 * another window archived it, an action from this group archived it), the
 * section keeps the row in place and the group says so instead, with the
 * issue's actions still there, an undo and the way on to the next one.
 *
 * A snoozed issue's group says when it comes back instead, with the way to
 * bring it back now or change the time.
 *
 * `movingOn`: the page is still handing off to another notification, so a
 * group this issue showed stays up rather than blinking out and back in.
 */
export function IssueInboxBar({
  issueId,
  issueActions,
  movingOn = false,
  isSnoozedView = false,
}: {
  issueId: string;
  issueActions?: ReactNode;
  movingOn?: boolean;
  /** Opened from the Snoozed tab: show only its snooze bar without touching the live Inbox list. */
  isSnoozedView?: boolean;
}) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { rows, kept } = useFilteredInboxRows(wsId, isSnoozedView ? null : issueId, !isSnoozedView);
  const index = rows.findIndex((row) => row.issue_id === issueId);
  const row = index >= 0 ? rows[index]! : null;
  // A row kept after it left the Inbox on its own is no longer there to act on.
  const settled = kept && kept.reason !== "filtered" ? kept : null;
  const item = settled ? null : row;
  const next = row ? nextInboxRow(rows, issueId) : null;
  // Remember that this issue showed a group, so while the page is still
  // handing off to the next notification (`movingOn`) it stays up instead of
  // blinking out until the next issue's group mounts.
  const hadBarForIssueRef = useRef<string | null>(null);
  if (item || settled) hadBarForIssueRef.current = issueId;
  else if (!movingOn && hadBarForIssueRef.current === issueId) hadBarForIssueRef.current = null;
  const leave = useLeaveInboxRow();
  const archiveWithUndo = useArchiveInboxWithUndo();
  const restore = useRestoreInbox();
  const unsubscribeArchive = useUnsubscribeArchive();
  const markRead = useMarkInboxRead();
  const markUnread = useMarkInboxUnread();
  const { snooze, unsnooze } = useInboxSnooze();
  const { data: snoozedList } = useQuery(snoozedInboxOptions(wsId));
  const snoozed = item ? undefined : findSnoozedInboxIssue(snoozedList, issueId);
  const keptText = useKeptInboxRowText(wsId);
  const [snoozeOpen, setSnoozeOpen] = useState(false);

  const archiveItem = () => {
    if (!item) return;
    archiveWithUndo(item.id, issueId);
    leave(issueId, next, {
      kind: "archived",
      itemId: item.id,
      key: issueId,
      label: clearedLabelForItem(item),
      href: inboxRowHref(p, item),
      clearedAt: Date.now(),
    });
  };

  const unsubscribeItem = (scope: InboxUnsubscribeScope) => {
    if (!item?.issue_id) return;
    unsubscribeArchive({ id: item.id, issueId: item.issue_id }, scope);
    leave(issueId, next, {
      kind: "unsubscribed",
      itemId: item.id,
      issueId: item.issue_id,
      key: issueId,
      label: clearedLabelForItem(item),
      href: inboxRowHref(p, item),
      undoable: scope === "issue",
      clearedAt: Date.now(),
    });
  };

  const snoozeItem = (until: Date) => {
    if (!item) return;
    snooze(item, until);
    leave(issueId, next, {
      kind: "snoozed",
      itemId: item.id,
      key: issueId,
      label: clearedLabelForItem(item),
      href: inboxRowHref(p, item),
      until: until.toISOString(),
      wasRead: item.read === true,
      clearedAt: Date.now(),
    });
  };

  const moveOn = () => {
    const cleared: LastInboxCleared | undefined = settled
      ? settled.reason === "snoozed" && settled.item.snoozed_until
        ? {
            kind: "snoozed",
            itemId: settled.item.id,
            key: issueId,
            label: clearedLabelForItem(settled.item),
            href: inboxRowHref(p, settled.item),
            until: settled.item.snoozed_until,
            wasRead: settled.item.read === true,
            clearedAt: Date.now(),
          }
        : {
            kind: "archived",
            itemId: settled.item.id,
            key: issueId,
            label: clearedLabelForItem(settled.item),
            href: inboxRowHref(p, settled.item),
            clearedAt: Date.now(),
          }
      : undefined;
    leave(issueId, next, cleared);
  };
  const { cause, reason: archiveReason } = useAutoArchiveCause(settled);

  // Archived already, E moves on (to the next notification or Inbox Zero).
  useInboxShortcut("archiveInboxItem", item ? archiveItem : settled ? moveOn : null);
  useInboxShortcut(
    "snoozeInboxItem",
    item ? () => setSnoozeOpen(true) : snoozed ? () => unsnooze(snoozed.item_id, issueId) : null,
  );

  if (!item) {
    if (snoozed) {
      return (
        <SnoozedBar
          until={new Date(snoozed.snoozed_until)}
          open={snoozeOpen}
          onOpenChange={setSnoozeOpen}
          onSnooze={(until) => snooze({ id: snoozed.item_id }, until)}
          onUnsnooze={() => unsnooze(snoozed.item_id, issueId)}
          next={settled ? next : null}
          onNext={moveOn}
        />
      );
    }
    if (!settled) {
      if (movingOn && hadBarForIssueRef.current === issueId) {
        return (
          <InboxBar settled>
            <InboxBarStatus icon={<Archive />} label={t(($) => $.kept.archived)} />
          </InboxBar>
        );
      }
      return null;
    }
    // An agent at work says so in the header (`IssueAgentHeaderChip`), not here.
    const status = keptText.status(settled, cause, archiveReason === "agent_triggered");
    return (
      <InboxKeptActions
        label={status.label}
        hint={status.hint}
        issueActions={settled.reason === "archived" ? issueActions : undefined}
        onUndo={() => restore(settled.item.id, { quiet: true })}
        next={next}
        onNext={moveOn}
      />
    );
  }

  const toggleReadItem = () => {
    if (!item) return;
    const store = useInboxSelectionStore.getState();
    if (item.read === true) {
      store.markManualUnread(wsId, item.id);
      markUnread.mutate(item.id, {
        onError: (err) => {
          store.clearManualUnread(wsId, item.id);
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.errors.mark_unread_failed),
          );
        },
      });
    } else {
      store.clearManualUnread(wsId, item.id);
      markRead.mutate(item.id, {
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.errors.mark_read_failed),
          ),
      });
    }
  };

  return (
    <InboxOpenBar issueActions={issueActions}>
      <InboxMoreMenu
        issueId={issueId}
        userId={item.recipient_id}
        read={item.read === true}
        onToggleRead={toggleReadItem}
        onUnsubscribe={unsubscribeItem}
      />
      <InboxSnoozeMenu
        open={snoozeOpen}
        onOpenChange={setSnoozeOpen}
        onSnooze={snoozeItem}
        side="top"
        trigger={<InboxBarButton icon={<AlarmClock />} label={t(($) => $.snooze.action)} shortcut="snoozeInboxItem" />}
      />
      <InboxArchiveButton onClick={archiveItem} />
    </InboxOpenBar>
  );
}

/** When a snoozed issue comes back, with the way to bring it back now or later. */
function SnoozedBar({
  until,
  open,
  onOpenChange,
  onSnooze,
  onUnsnooze,
  next,
  onNext,
}: {
  until: Date;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSnooze: (until: Date) => void;
  onUnsnooze: () => void;
  /** Set while the section keeps the row it snoozed in place. */
  next: InboxItem | null;
  onNext: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const label = t(($) => $.snooze.back_at, { time: formatSnoozeTime(until, new Date(), locale) });
  return (
    <InboxBar settled>
      <InboxBarStatus icon={<AlarmClock />} label={label} />
      <InboxSnoozeMenu
        open={open}
        onOpenChange={onOpenChange}
        snoozed
        onSnooze={onSnooze}
        onUnsnooze={onUnsnooze}
        side="top"
        trigger={<InboxBarButton icon={<CalendarClock />} label={t(($) => $.snooze.change_time)} />}
      />
      {next && <InboxNextButton next={next} onNext={onNext} />}
    </InboxBar>
  );
}
