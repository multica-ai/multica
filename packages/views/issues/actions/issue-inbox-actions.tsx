"use client";

import { useMemo, type ReactNode, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlarmClock, ChevronDown } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import type { LastInboxCleared } from "@multica/core/inbox/selection-store";
import type { InboxItem } from "@multica/core/types";
import { findSnoozedInboxIssue, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { nextInboxRow } from "@multica/core/inbox/open-row";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { InboxArchiveButton, InboxBar, InboxBarButton, InboxBarStatus, InboxOpenBar } from "../../inbox/components/inbox-bar";
import { inboxRowHref } from "../../inbox/components/inbox-display";
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
  clearedLabelForItem,
  useArchiveInboxWithUndo,
  useLeaveInboxRow,
  useRestoreInbox,
} from "../../inbox/components/use-leave-inbox-row";
import { useBuiltinViewNavigation } from "../../issue-views/builtin-views";
import { useNavigation } from "../../navigation";
import { useLocale, useT } from "../../i18n";

/**
 * The Inbox bar of an issue opened from the Work panel's Inbox section, while
 * the section still lists it: why it is here, `issueActions` (its status,
 * assignee and quick actions), then the notification's own. Snoozing,
 * archiving or unsubscribing opens the next notification the section lists,
 * so the Inbox can be worked through without leaving the page.
 *
 * Once the notification leaves the Inbox on its own (the issue reached Done,
 * another window archived it, an action from this bar archived it), the
 * section keeps the row in place and the bar says so instead, with the
 * issue's actions still there, an undo and the way on to the next one.
 *
 * A snoozed issue's bar says when it comes back instead, wherever it is
 * opened, with the way to bring it back now or change the time.
 */
export function IssueInboxBar({ issueId, issueActions }: { issueId: string; issueActions?: ReactNode }) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const builtinNav = useBuiltinViewNavigation();
  const guest = useIsGuest(wsId);
  const { fromInbox } = useSidebarInboxIssue(wsId, issueId);
  const { rows, kept, filtered } = useFilteredInboxRows(wsId, issueId, fromInbox);
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
  const activeSnoozedCount = useMemo(() => {
    const now = Date.now();
    return (snoozedList ?? []).filter((entry) => new Date(entry.snoozed_until).getTime() > now).length;
  }, [snoozedList]);
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
  const cause = useMoveOnAfterAutoArchive({ settled, next, onNext: moveOn });

  const archiveRef = useRef<(() => void) | null>(null);
  const snoozeRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    // Archived already, E moves on (to the next notification or Inbox Zero).
    archiveRef.current = item ? archiveItem : settled ? moveOn : null;
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
    const inboxCleared = !next && !filtered;
    return (
      <InboxKeptActions
        label={keptText.notice(settled, cause)}
        issueActions={settled.reason === "archived" ? issueActions : undefined}
        onUndo={() => restore(settled.item.id, { quiet: true })}
        next={next}
        onNext={moveOn}
        inboxCleared={inboxCleared}
        snoozedCount={inboxCleared ? activeSnoozedCount : 0}
        onSnoozedClick={
          inboxCleared
            ? () => {
                builtinNav.prepare("snoozed");
                push(builtinNav.hrefFor("snoozed"));
              }
            : undefined
        }
        onGoToMyWork={
          inboxCleared && !guest
            ? () => {
                builtinNav.prepare("my_work");
                push(builtinNav.hrefFor("my_work"));
              }
            : undefined
        }
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
