"use client";

import { type ReactNode, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlarmClock, ChevronDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import type { InboxItem } from "@multica/core/types";
import { findSnoozedInboxIssue, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { nextInboxRow } from "@multica/core/inbox/open-row";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { InboxArchiveButton, InboxBar, InboxBarButton, InboxBarStatus, InboxOpenBar } from "../../inbox/components/inbox-bar";
import { InboxKeptActions, InboxNextButton, useKeptInboxRowText } from "../../inbox/components/inbox-kept-actions";
import { InboxSnoozeMenu } from "../../inbox/components/inbox-snooze-menu";
import { formatSnoozeTime } from "../../inbox/components/inbox-snooze-time";
import { useInboxSnooze } from "../../inbox/components/use-inbox-snooze";
import { useFilteredInboxRows } from "../../inbox/components/use-filtered-inbox-rows";
import { useSidebarInboxIssue } from "../../inbox/components/use-sidebar-inbox-issue";
import { UnsubscribeArchiveButton } from "../../inbox/components/unsubscribe-archive-button";
import { useUnsubscribeArchive } from "../../inbox/components/use-unsubscribe-archive";
import { useMoveOnAfterAutoArchive } from "../../inbox/components/use-move-on-after-auto-archive";
import {
  useArchiveInboxWithUndo,
  useLeaveInboxRow,
  useRestoreInbox,
} from "../../inbox/components/use-leave-inbox-row";
import { useLocale, useT } from "../../i18n";

/**
 * The Inbox bar of an issue opened from the Work panel's Inbox section, while
 * the section still lists it: why it is here, `issueActions` (its status,
 * assignee and quick actions), then the notification's own. Snoozing,
 * archiving or unsubscribing opens the next notification the section lists,
 * so the Inbox can be worked through without leaving the page.
 *
 * Once the notification leaves the Inbox on its own (the issue reached Done,
 * another window archived it), the section keeps the row in place and the bar
 * says so instead, with an undo and the way on to the next notification.
 *
 * A snoozed issue's bar says when it comes back instead, wherever it is
 * opened, with the way to bring it back now or change the time.
 */
export function IssueInboxBar({ issueId, issueActions }: { issueId: string; issueActions?: ReactNode }) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { fromInbox } = useSidebarInboxIssue(wsId, issueId);
  const { rows, kept } = useFilteredInboxRows(wsId, issueId, fromInbox);
  const index = fromInbox ? rows.findIndex((row) => row.issue_id === issueId) : -1;
  const row = index >= 0 ? rows[index]! : null;
  // A row kept after it left the Inbox on its own is no longer there to act on.
  const settled = kept && kept.reason !== "filtered" ? kept : null;
  const item = settled ? null : row;
  const next = row ? nextInboxRow(rows, issueId) : null;
  const leave = useLeaveInboxRow();
  const archiveWithUndo = useArchiveInboxWithUndo();
  const restore = useRestoreInbox();
  const unsubscribeArchive = useUnsubscribeArchive();
  const { snooze, unsnooze } = useInboxSnooze();
  const { data: snoozedList } = useQuery(snoozedInboxOptions(wsId));
  const snoozed = item ? undefined : findSnoozedInboxIssue(snoozedList, issueId);
  const keptText = useKeptInboxRowText(wsId);
  const [snoozeOpen, setSnoozeOpen] = useState(false);

  const archiveItem = () => {
    if (!item) return;
    archiveWithUndo(item.id, issueId);
    leave(issueId, next);
  };

  const unsubscribeItem = (scope: InboxUnsubscribeScope) => {
    if (!item?.issue_id) return;
    unsubscribeArchive({ id: item.id, issueId: item.issue_id }, scope);
    leave(issueId, next);
  };

  const snoozeItem = (until: Date) => {
    if (!item) return;
    snooze(item, until);
    leave(issueId, next);
  };

  const moveOn = () => leave(issueId, next);
  useMoveOnAfterAutoArchive({ settled, next, onNext: moveOn });

  const archiveRef = useRef<(() => void) | null>(null);
  const snoozeRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    // Archived already, E moves on as archiving would have.
    archiveRef.current = item ? archiveItem : settled && next ? moveOn : null;
    snoozeRef.current = item
      ? () => setSnoozeOpen(true)
      : snoozed
        ? () => unsnooze(snoozed.item_id, issueId)
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
    if (!settled) return null;
    return (
      <InboxKeptActions
        label={keptText.notice(settled)}
        onUndo={() => restore(settled.item.id, { quiet: true })}
        next={next}
        onNext={moveOn}
      />
    );
  }

  return (
    <InboxOpenBar item={item} issueActions={issueActions}>
      <UnsubscribeArchiveButton issueId={issueId} userId={item.recipient_id} onConfirm={unsubscribeItem} />
      <InboxSnoozeMenu
        open={snoozeOpen}
        onOpenChange={setSnoozeOpen}
        onSnooze={snoozeItem}
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
        trigger={
          <Button size="sm" variant="ghost" className="shrink-0 text-muted-foreground">
            {t(($) => $.snooze.change_time)}
            <ChevronDown className="size-3" />
          </Button>
        }
      />
      {next && <InboxNextButton next={next} onNext={onNext} />}
    </InboxBar>
  );
}
