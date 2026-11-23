"use client";

import { useQuery } from "@tanstack/react-query";
import { AlarmClock } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { SnoozedInboxIssue } from "@multica/core/types";
import { StatusIcon } from "../../issues/components";
import { useLocale, useT } from "../../i18n";
import { formatSnoozeTime } from "./inbox-snooze-time";

/**
 * The Snoozed tab on a phone's Inbox page: each snoozed issue and when it
 * comes back, soonest first. Opening one leaves it snoozed and unread.
 */
export function SnoozedInboxList({
  wsId,
  rows,
  selectedKey,
  onSelect,
}: {
  wsId: string;
  rows: readonly SnoozedInboxIssue[];
  selectedKey: string;
  onSelect: (issueId: string) => void;
}) {
  const { t } = useT("inbox");
  if (rows.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center text-muted-foreground">
        <AlarmClock className="mb-3 h-8 w-8 text-faint-foreground" />
        <p className="text-body">{t(($) => $.snooze.view_empty)}</p>
        <p className="mt-1 text-caption">{t(($) => $.snooze.view_empty_description)}</p>
      </div>
    );
  }
  return (
    <ul className="flex-1 min-h-0 overflow-y-auto space-y-px p-2">
      {rows.map((entry) => (
        <li key={entry.issue_id}>
          <SnoozedInboxRow
            wsId={wsId}
            entry={entry}
            selected={entry.issue_id === selectedKey}
            onSelect={() => onSelect(entry.issue_id)}
          />
        </li>
      ))}
    </ul>
  );
}

function SnoozedInboxRow({
  wsId,
  entry,
  selected,
  onSelect,
}: {
  wsId: string;
  entry: SnoozedInboxIssue;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const { data: issue } = useQuery(issueDetailOptions(wsId, entry.issue_id));
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const backAt = t(($) => $.snooze.back_at, { time: formatSnoozeTime(new Date(entry.snoozed_until), new Date(), locale) });
  return (
    <button
      type="button"
      aria-current={selected ? "true" : undefined}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-md px-2 py-2.5 text-left outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring",
        selected ? "bg-accent" : "hover:bg-accent/50",
      )}
    >
      <span className="flex size-7 shrink-0 items-center justify-center">
        {issue ? (
          <StatusIcon
            status={issue.status}
            category={categoryOf(issue.status)}
            color={colorOf(issue.status)}
            icon={iconOf(issue.status)}
            className="size-4"
          />
        ) : (
          <Skeleton className="size-4 rounded-full" />
        )}
      </span>
      <span className="min-w-0 flex-1">
        {issue ? (
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-body">{issue.title}</span>
            <span className="shrink-0 text-caption tabular-nums text-muted-foreground">{issue.identifier}</span>
          </span>
        ) : (
          <Skeleton className="h-4 w-3/4" />
        )}
        <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <AlarmClock aria-hidden className="size-3 shrink-0" />
          <span className="truncate">
            {backAt}
            {entry.updates > 0 && ` · ${t(($) => $.snooze.updates, { count: entry.updates })}`}
          </span>
        </span>
      </span>
    </button>
  );
}
