"use client";

import type { ReactNode } from "react";
import { Archive, ArrowDown, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { InboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useStatusLabel } from "../../issues/utils/status-label";
import { formatSnoozeTime } from "./inbox-snooze-time";
import { getInboxDisplayTitle, isAutopilotQuotaNotice } from "./inbox-display";
import { InboxBar, InboxBarSeparator, InboxBarStatus } from "./inbox-bar";

/**
 * How a kept row (`keepOpenInboxRow`) says why it is settled: `line` in its
 * place in the list, `notice` beside the open notification.
 */
export function useKeptInboxRowText(wsId: string) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const { categoryOf } = useIssueStatuses(wsId);
  const statusLabelOf = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  // Finishing the issue is what archives its notifications on its own, so the
  // status says why where it can.
  const finished = (item: InboxItem) => {
    const status = item.issue_status;
    if (!status) return null;
    const category = categoryOf(status);
    return category === "done" || category === "closed" ? statusLabelOf(status) : null;
  };
  const backAt = (item: InboxItem) =>
    item.snoozed_until
      ? t(($) => $.snooze.back_at, { time: formatSnoozeTime(new Date(item.snoozed_until), new Date(), locale) })
      : t(($) => $.kept.archived);
  const did = (action: InboxBarAction) => {
    switch (action.type) {
      case "status":
        return t(($) => $.kept.changed_status, { status: statusLabelOf(action.status) });
      case "assignee":
        return action.assignee
          ? t(($) => $.kept.reassigned, { name: getActorName(action.assignee.type, action.assignee.id) })
          : t(($) => $.kept.unassigned);
      case "quick_action":
        return t(($) => $.kept.ran_quick_action, { name: action.name });
    }
  };
  return {
    line: (kept: KeptInboxRow) => {
      if (kept.reason === "filtered") return t(($) => $.kept.filtered);
      const why = kept.reason === "snoozed" ? backAt(kept.item) : (finished(kept.item) ?? t(($) => $.kept.archived));
      return `${why} · ${t(($) => $.kept.leaves)}`;
    },
    /** `cause` is the bar action that archived it (`useMoveOnAfterAutoArchive`). */
    notice: (kept: KeptInboxRow, cause?: InboxBarAction | null) => {
      if (kept.reason === "snoozed") return backAt(kept.item);
      const why = (cause && kept.reason === "archived" ? did(cause) : null) ?? finished(kept.item);
      return why ? `${why} · ${t(($) => $.kept.notification_archived)}` : t(($) => $.kept.notification_archived);
    },
  };
}

/**
 * What the open notification's bar offers once it has left the Inbox on its
 * own (its issue reached Done, another window archived it) and stays on
 * screen until the user moves on: what happened, the issue's own actions
 * when it was archived, so the next change needs no trip to its properties,
 * then the way to undo it and the next notification.
 */
export function InboxKeptActions({
  label,
  icon,
  issueActions,
  onUndo,
  next,
  onNext,
}: {
  label: ReactNode;
  /** Defaults to the archive icon. */
  icon?: ReactNode;
  issueActions?: ReactNode;
  onUndo?: () => void;
  /** The notification `onNext` opens; without one there is nothing to move on to. */
  next: InboxItem | null;
  onNext: () => void;
}) {
  const { t } = useT("inbox");
  return (
    <InboxBar settled>
      <InboxBarStatus icon={icon ?? <Archive />} label={label} />
      {issueActions}
      {issueActions && (onUndo || next) && <InboxBarSeparator />}
      {onUndo && (
        <Button size="sm" variant="ghost" className="shrink-0 text-muted-foreground" onClick={onUndo}>
          <Undo2 />
          {t(($) => $.toasts.undo)}
        </Button>
      )}
      {next && <InboxNextButton next={next} onNext={onNext} />}
    </InboxBar>
  );
}

/** Opens the next notification, named in its tooltip; E does the same. */
export function InboxNextButton({ next, onNext }: { next: InboxItem; onNext: () => void }) {
  const { t } = useT("inbox");
  const title = isAutopilotQuotaNotice(next.type) ? t(($) => $.types[next.type]) : getInboxDisplayTitle(next);
  return (
    <TooltipHint label={t(($) => $.kept.next_tooltip, { title })} side="bottom">
      <Button size="sm" variant="outline" className="shrink-0" onClick={onNext}>
        {t(($) => $.kept.next)}
        <ArrowDown />
        <span className="flex @max-[30rem]/inbox-bar:hidden">
          <ActionShortcutKeycaps action="archiveInboxItem" />
        </span>
      </Button>
    </TooltipHint>
  );
}
