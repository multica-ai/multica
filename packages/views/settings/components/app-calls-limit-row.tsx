"use client";

import { useQuery } from "@tanstack/react-query";
import type { AppCallRow } from "@multica/core/billing";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2, RefreshCw } from "@multica/ui/icons";
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from "@multica/ui/components/ui/progress";
import { useLocale, useT } from "../../i18n";
import { SettingsRow } from "./settings-layout";

function formatResetAt(value: string | null, locale: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}

/**
 * Billing's row for the monthly app call limit (MUL-10047): this month's
 * calls against the workspace's limit and, for owners and admins, who made
 * them.
 */
export function AppCallsLimitRow({
  wsId,
  row,
  retrying,
  onRetry,
}: {
  wsId: string;
  row: AppCallRow;
  retrying: boolean;
  onRetry: () => void;
}) {
  const { t } = useT("billing");
  const locale = useLocale();
  const numberFormatter = new Intl.NumberFormat(locale);
  const members = row.kind === "metered" ? row.members : [];
  const memberQuery = useQuery({ ...memberListOptions(wsId), enabled: members.length > 0 });
  const nameOf = (userId: string | null) => {
    if (userId === null) return t(($) => $.workspace.limits.app_calls_no_person);
    return memberQuery.data?.find((m) => m.user_id === userId)?.name ?? t(($) => $.workspace.limits.app_calls_former_member);
  };

  if (row.kind === "unavailable") {
    return (
      <SettingsRow
        label={t(($) => $.workspace.limits.app_calls)}
        description={t(($) => $.workspace.limits.app_calls_description)}
      >
        <div className="flex flex-col gap-2 sm:items-end">
          {row.limit !== null ? (
            <span className="tabular-nums">
              {t(($) => $.workspace.limits.per_month, { count: row.limit })}
            </span>
          ) : null}
          <span className="text-caption text-muted-foreground">
            {t(($) => $.workspace.limits.usage_unavailable)}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={t(($) => $.workspace.actions.retry_app_calls)}
            aria-busy={retrying}
            disabled={retrying}
            onClick={onRetry}
          >
            {retrying ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : <RefreshCw />}
            {t(($) => $.workspace.actions.retry)}
          </Button>
        </div>
      </SettingsRow>
    );
  }

  const resetAt = formatResetAt(row.resetAt, locale);
  return (
    <SettingsRow
      label={t(($) => $.workspace.limits.app_calls)}
      description={t(($) => $.workspace.limits.app_calls_description)}
    >
      <div className="w-full max-w-72 space-y-2">
        <Progress
          value={row.limit <= 0 ? 100 : Math.min(100, (row.used / row.limit) * 100)}
          aria-label={t(($) => $.workspace.limits.app_calls_label)}
        >
          <ProgressLabel>
            {row.reached
              ? t(($) => $.workspace.limits.reached)
              : t(($) => $.workspace.limits.current_usage)}
          </ProgressLabel>
          <ProgressValue>
            {() =>
              t(($) => $.workspace.limits.usage_total, {
                total: numberFormatter.format(row.used),
                limit: numberFormatter.format(row.limit),
              })
            }
          </ProgressValue>
        </Progress>
        {resetAt ? (
          <p className="text-caption text-muted-foreground tabular-nums">
            {t(($) => $.workspace.limits.resets_at, { date: resetAt })}
          </p>
        ) : null}
        {members.length > 0 ? (
          <ul
            className="space-y-1 pt-1 text-caption text-muted-foreground"
            aria-label={t(($) => $.workspace.limits.app_calls_by_member)}
          >
            {members.map((m) => (
              <li key={m.userId ?? "none"} className="flex justify-between gap-3">
                <span className="truncate">{nameOf(m.userId)}</span>
                <span className="shrink-0 tabular-nums">{numberFormatter.format(m.calls)}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </SettingsRow>
  );
}
