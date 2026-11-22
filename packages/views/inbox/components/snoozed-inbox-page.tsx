"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlarmClock, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { snoozedInboxOptions } from "@multica/core/inbox/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { cachedIssueRouteRef } from "@multica/core/issues/canonical-id";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { SnoozedInboxIssue } from "@multica/core/types";
import { CollectionPageHeader, CollectionPageState } from "../../layout/collection-page";
import {
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
  type CollectionGroup,
} from "../../layout/collection-list";
import { StatusIcon } from "../../issues/components";
import { useBuiltinViewLabel } from "../../issue-views/builtin-views";
import { useLocale, useT } from "../../i18n";
import { InboxSnoozeMenu } from "./inbox-snooze-menu";
import { formatSnoozeTime } from "./inbox-snooze-time";
import { useInboxSnooze } from "./use-inbox-snooze";

const DAY_MS = 24 * 60 * 60 * 1000;

function startOfDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/** The day an issue comes back on: today, tomorrow, then the weekday and date. */
function returnDayLabel(day: Date, now: Date, locale: string): string {
  const days = Math.round((startOfDay(day) - startOfDay(now)) / DAY_MS);
  const label =
    days <= 1
      ? new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(Math.max(days, 0), "day")
      : new Intl.DateTimeFormat(locale, {
          weekday: "long",
          month: "short",
          day: "numeric",
          ...(day.getFullYear() === now.getFullYear() ? {} : { year: "numeric" as const }),
        }).format(day);
  return label.charAt(0).toLocaleUpperCase(locale) + label.slice(1);
}

/**
 * The issues the viewer snoozed in the Inbox, soonest back first and grouped
 * by the day they come back. Opening one leaves it snoozed and unread.
 */
export function SnoozedInboxPage() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const qc = useQueryClient();
  const locale = useLocale();
  const title = useBuiltinViewLabel()("snoozed");
  const { data: snoozed, isLoading } = useQuery(snoozedInboxOptions(wsId));
  const { snooze, unsnooze } = useInboxSnooze();

  const groups = useMemo<CollectionGroup<SnoozedInboxIssue>[]>(() => {
    const now = new Date();
    const byDay = new Map<number, SnoozedInboxIssue[]>();
    for (const entry of snoozed ?? []) {
      const until = new Date(entry.snoozed_until);
      if (until.getTime() <= now.getTime()) continue;
      const day = startOfDay(until);
      const rows = byDay.get(day);
      if (rows) rows.push(entry);
      else byDay.set(day, [entry]);
    }
    return [...byDay.entries()]
      .sort(([a], [b]) => a - b)
      .map(([day, rows]) => ({
        key: String(day),
        label: returnDayLabel(new Date(day), now, locale),
        count: rows.length,
        rows,
      }));
  }, [snoozed, locale]);

  const columns: CollectionColumn<SnoozedInboxIssue>[] = [
    {
      key: "issue",
      cell: (row) => <SnoozedIssueName wsId={wsId} issueId={row.issue_id} />,
    },
    {
      key: "updates",
      header: t(($) => $.snooze.col_updates),
      width: 160,
      cell: (row) =>
        row.updates > 0 ? (
          <span className="truncate">{t(($) => $.snooze.updates, { count: row.updates })}</span>
        ) : (
          <CollectionEmptyValue />
        ),
    },
    {
      key: "back",
      header: t(($) => $.snooze.col_back),
      width: 136,
      align: "end",
      core: true,
      className: "tabular-nums",
      cell: (row) => formatSnoozeTime(new Date(row.snoozed_until), new Date(), locale),
    },
    {
      key: "actions",
      width: 128,
      align: "end",
      core: true,
      interactive: true,
      cell: (row) => (
        <SnoozedRowActions
          onUnsnooze={() => unsnooze(row.item_id, row.issue_id)}
          onSnooze={(until) => snooze({ id: row.item_id }, until)}
        />
      ),
    },
  ];

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader icon={AlarmClock} title={title} />
      {isLoading ? (
        <div className="space-y-2 p-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      ) : (
        <CollectionList
          label={title}
          columns={columns}
          groups={groups}
          getRowId={(row) => row.issue_id}
          // Older servers send no identifier. The cache supplies it when the
          // issue was loaded before this list last rendered; else the UUID.
          getRowHref={(row) =>
            p.issueDetail(
              row.issue_identifier
                ? issueRouteRef({ id: row.issue_id, identifier: row.issue_identifier })
                : cachedIssueRouteRef(qc, wsId, row.issue_id),
            )
          }
          empty={
            <CollectionPageState
              icon={AlarmClock}
              title={t(($) => $.snooze.view_empty)}
              description={t(($) => $.snooze.view_empty_description)}
            />
          }
        />
      )}
    </div>
  );
}

function SnoozedIssueName({ wsId, issueId }: { wsId: string; issueId: string }) {
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  if (!issue) return <Skeleton className="h-4 w-48" />;
  return (
    <CollectionNameCell
      icon={
        <StatusIcon
          status={issue.status}
          category={categoryOf(issue.status)}
          color={colorOf(issue.status)}
          icon={iconOf(issue.status)}
          className="size-3.5 shrink-0"
        />
      }
      title={issue.title}
      badges={<span className="shrink-0 text-caption text-muted-foreground tabular-nums">{issue.identifier}</span>}
    />
  );
}

/** Bring back now, or snooze until another time; shown while the row is hovered. */
function SnoozedRowActions({
  onUnsnooze,
  onSnooze,
}: {
  onUnsnooze: () => void;
  onSnooze: (until: Date) => void;
}) {
  const { t } = useT("inbox");
  const [open, setOpen] = useState(false);
  return (
    <span
      className="flex items-center gap-1 opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100 has-data-popup-open:opacity-100 [@media(pointer:coarse)]:opacity-100"
    >
      <Button size="xs" variant="ghost" className="text-muted-foreground" onClick={onUnsnooze}>
        <Undo2 />
        {t(($) => $.snooze.unsnooze)}
      </Button>
      <InboxSnoozeMenu
        open={open}
        onOpenChange={setOpen}
        onSnooze={onSnooze}
        trigger={
          <Button size="icon-xs" variant="ghost" className="text-muted-foreground" aria-label={t(($) => $.snooze.change_time)}>
            <AlarmClock />
          </Button>
        }
      />
    </span>
  );
}
