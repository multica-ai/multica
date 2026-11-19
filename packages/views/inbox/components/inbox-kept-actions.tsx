"use client";

import type { ReactNode } from "react";
import { Archive, ArrowDown, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { cn } from "@multica/ui/lib/utils";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import type { InboxItem } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useStatusLabel } from "../../issues/utils/status-label";
import { formatSnoozeTime } from "./inbox-snooze-time";
import { getInboxDisplayTitle, isAutopilotQuotaNotice } from "./inbox-display";

/**
 * How a kept row (`keepOpenInboxRow`) says why it is settled: `line` in its
 * place in the list, `notice` beside the open notification.
 */
export function useKeptInboxRowText(wsId: string) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const { categoryOf } = useIssueStatuses(wsId);
  const statusLabelOf = useStatusLabel(wsId);
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
  return {
    line: (kept: KeptInboxRow) => {
      if (kept.reason === "filtered") return t(($) => $.kept.filtered);
      const why = kept.reason === "snoozed" ? backAt(kept.item) : (finished(kept.item) ?? t(($) => $.kept.archived));
      return `${why} · ${t(($) => $.kept.leaves)}`;
    },
    notice: (kept: KeptInboxRow) => {
      if (kept.reason === "snoozed") return backAt(kept.item);
      const status = finished(kept.item);
      return status ? `${status} · ${t(($) => $.kept.notification_archived)}` : t(($) => $.kept.notification_archived);
    },
  };
}

/**
 * What the open notification offers once it has left the Inbox on its own
 * (its issue reached Done, another window archived it) and stays on screen
 * until the user moves on: what happened, the way to undo it, and the next
 * notification. `header` sits in the issue header's actions; `strip` is a bar
 * under it, for a phone, whose header has no room.
 */
export function InboxKeptActions({
  label,
  icon,
  onUndo,
  next,
  onNext,
  layout,
}: {
  label: ReactNode;
  /** Defaults to the archive icon. */
  icon?: ReactNode;
  onUndo?: () => void;
  /** The notification `onNext` opens; without one there is nothing to move on to. */
  next: InboxItem | null;
  onNext: () => void;
  layout: "header" | "strip";
}) {
  const { t } = useT("inbox");
  const undo = onUndo && (
    <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onUndo}>
      <Undo2 />
      {t(($) => $.toasts.undo)}
    </Button>
  );
  const nextButton = next && <InboxNextButton next={next} onNext={onNext} />;
  const status = (
    <span className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
      <span aria-hidden className="flex shrink-0 [&_svg]:size-3.5">
        {icon ?? <Archive />}
      </span>
      <span className="truncate">{label}</span>
    </span>
  );

  if (layout === "strip") {
    return (
      <div
        role="status"
        className={cn("flex h-10 shrink-0 items-center gap-2 border-b bg-muted/40", PAGE_GUTTER)}
      >
        <span className="flex min-w-0 flex-1">{status}</span>
        {undo}
        {nextButton}
      </div>
    );
  }
  return (
    <>
      <span role="status" className="mr-1 flex min-w-0">
        {status}
      </span>
      {undo}
      {nextButton}
      <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />
    </>
  );
}

/** Opens the next notification, named in its tooltip; E does the same. */
export function InboxNextButton({ next, onNext }: { next: InboxItem; onNext: () => void }) {
  const { t } = useT("inbox");
  const title = isAutopilotQuotaNotice(next.type) ? t(($) => $.types[next.type]) : getInboxDisplayTitle(next);
  return (
    <TooltipHint
      label={t(($) => $.kept.next_tooltip, { title })}
      shortcut={<ActionShortcutKeycaps action="archiveInboxItem" />}
      side="bottom"
    >
      <Button size="sm" variant="outline" onClick={onNext}>
        {t(($) => $.kept.next)}
        <ArrowDown />
      </Button>
    </TooltipHint>
  );
}
