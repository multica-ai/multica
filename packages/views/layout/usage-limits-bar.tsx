"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw, RotateCcw, TriangleAlert } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  groupUsageLimitAccounts,
  isUsageLimitWarning,
  runtimeListOptions,
  usageLimitBarWindows,
  usageLimitPace,
  type UsageLimitAccount,
  type UsageLimitWindow,
} from "@multica/core/runtimes";
import { useRefreshUsageLimits } from "@multica/core/runtimes/mutations";
import { ProviderLogo } from "../runtimes/components/provider-logo";
import { useT } from "../i18n";
import { useTimeAgo } from "../i18n/use-time-ago";

const MINUTES_PER_DAY = 24 * 60;
const MINUTES_PER_WEEK = 7 * MINUTES_PER_DAY;

/**
 * Plan limits of the agent CLI logins behind the workspace's runtimes, one
 * entry per login. Renders nothing until a daemon has reported limits.
 */
export function UsageLimitsBar({ className }: { className?: string }) {
  const workspace = useCurrentWorkspace();
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(workspace?.id ?? ""),
    enabled: Boolean(workspace),
  });
  const accounts = useMemo(() => groupUsageLimitAccounts(runtimes), [runtimes]);
  const { t } = useT("layout");

  if (accounts.length === 0) return null;

  const providerCounts = new Map<string, number>();
  for (const account of accounts) {
    providerCounts.set(account.provider, (providerCounts.get(account.provider) ?? 0) + 1);
  }
  const deviceName = (account: UsageLimitAccount) =>
    (providerCounts.get(account.provider) ?? 0) > 1 ? account.devices.join(", ") : "";
  const accountName = (account: UsageLimitAccount) => {
    const provider = account.provider.charAt(0).toUpperCase() + account.provider.slice(1);
    const device = deviceName(account);
    return device ? `${provider} · ${device}` : provider;
  };

  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={t(($) => $.usage_limits.title)}
            className={cn(
              "flex h-6 min-w-0 shrink-0 items-center gap-5 overflow-hidden px-3 text-caption whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              className,
            )}
          />
        }
      >
        {accounts.map((account) => (
          <AccountSummary key={account.key} account={account} device={deviceName(account)} />
        ))}
      </PopoverTrigger>
      <PopoverContent side="top" align="start" className="w-80">
        <UsageLimitsDetails accounts={accounts} accountName={accountName} />
      </PopoverContent>
    </Popover>
  );
}

function AccountSummary({ account, device }: { account: UsageLimitAccount; device: string }) {
  const windows = usageLimitBarWindows(account.windows);
  return (
    <span className={cn("inline-flex items-center gap-3", account.stale && "opacity-60")}>
      <span className="inline-flex items-center gap-1.5">
        <ProviderLogo provider={account.provider} className="h-3 w-3" />
        {device && <span>{device}</span>}
        {account.stale && <TriangleAlert className="h-3 w-3 text-warning" />}
      </span>
      {windows.length > 0 ? windows.map((window) => (
        <WindowSummary key={window.id} window={window} forecast={!account.stale} />
      )) : <span>—</span>}
    </span>
  );
}

function WindowSummary({ window, forecast }: { window: UsageLimitWindow; forecast: boolean }) {
  const { t, i18n } = useT("layout");
  const windowName = useWindowName();
  const pace = usageLimitPace(window);
  const runsOutAt = forecast ? pace?.runsOutAt : null;
  const resetsAt = window.resets_at ? new Date(window.resets_at) : null;
  const name = windowName(window, { short: true });
  const details = [
    resetsAt && t(($) => $.usage_limits.resets_at, { time: resetsAt.toLocaleString(i18n.language) }),
    pace && t(($) => $.usage_limits.elapsed, { percent: Math.round(pace.elapsedPercent) }),
    runsOutAt && t(($) => $.usage_limits.runs_out_in, { time: formatDuration(runsOutAt - Date.now(), t) }),
  ].filter(Boolean);
  return (
    <span className="inline-flex items-center gap-1.5 tabular-nums" title={details.join("\n")}>
      <span>{window.label ? `${name} ${window.label}` : name}</span>
      <PaceMeter window={window} elapsedPercent={pace?.elapsedPercent} className="w-12" />
      <span className={cn("font-medium", isUsageLimitWarning(window) ? "text-warning" : "text-foreground")}>
        {Math.round(window.used_percent)}%
      </span>
      {runsOutAt && (
        <span className="font-medium text-warning">
          {t(($) => $.usage_limits.out_in, { time: formatDuration(runsOutAt - Date.now(), t) })}
        </span>
      )}
      {resetsAt && (
        <span className="inline-flex items-center gap-0.5">
          <RotateCcw className="h-2.5 w-2.5" />
          {formatDuration(resetsAt.getTime() - Date.now(), t)}
        </span>
      )}
    </span>
  );
}

function PaceMeter({
  window,
  elapsedPercent,
  className,
}: {
  window: UsageLimitWindow;
  elapsedPercent?: number;
  className?: string;
}) {
  const windowName = useWindowName();
  const percent = Math.min(100, Math.max(0, window.used_percent));
  return (
    <span
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(percent)}
      aria-label={windowName(window)}
      className={cn("relative block h-1 shrink-0 rounded-full bg-foreground/10", className)}
    >
      <span
        className={cn("absolute inset-y-0 left-0 rounded-full", isUsageLimitWarning(window) ? "bg-warning" : "bg-primary")}
        style={{ width: `${percent}%` }}
      />
      {elapsedPercent !== undefined && (
        <span
          aria-hidden
          className="absolute -inset-y-0.5 w-0.5 -translate-x-1/2 rounded-full bg-foreground"
          style={{ left: `${elapsedPercent}%` }}
        />
      )}
    </span>
  );
}

function UsageLimitsDetails({
  accounts,
  accountName,
}: {
  accounts: UsageLimitAccount[];
  accountName: (account: UsageLimitAccount) => string;
}) {
  const { t } = useT("layout");
  const timeAgo = useTimeAgo();
  const refresh = useRefreshUsageLimits();
  const refreshRuntimeIds = accounts.flatMap((account) => account.refreshRuntimeIds);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-label font-medium">{t(($) => $.usage_limits.title)}</span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={t(($) => $.usage_limits.refresh)}
          title={t(($) => $.usage_limits.refresh)}
          disabled={refreshRuntimeIds.length === 0 || refresh.isPending}
          aria-busy={refresh.isPending}
          onClick={() => refresh.mutate([...new Set(refreshRuntimeIds)])}
        >
          <RefreshCw className={cn(refresh.isPending && "animate-spin")} />
        </Button>
      </div>
      {accounts.map((account) => (
        <section key={account.key} className="flex flex-col gap-2">
          <div className="flex items-center gap-1.5 text-caption">
            <ProviderLogo provider={account.provider} className="h-3.5 w-3.5" />
            <span className="font-medium">{accountName(account)}</span>
            {account.plan && <span className="text-muted-foreground capitalize">{account.plan}</span>}
          </div>
          {account.stale && (
            <p className="flex items-start gap-1.5 text-caption text-warning">
              <TriangleAlert className="mt-0.5 h-3 w-3 shrink-0" />
              <span className="min-w-0 break-words">
                {account.updatedAt
                  ? t(($) => $.usage_limits.stale, { time: timeAgo(account.updatedAt) })
                  : t(($) => $.usage_limits.never_checked)}
                {account.error && ` · ${account.error}`}
              </span>
            </p>
          )}
          {account.windows.map((window) => (
            <WindowRow key={window.id} window={window} forecast={!account.stale} />
          ))}
        </section>
      ))}
    </div>
  );
}

function WindowRow({ window, forecast }: { window: UsageLimitWindow; forecast: boolean }) {
  const { t, i18n } = useT("layout");
  const windowName = useWindowName();
  const warning = isUsageLimitWarning(window);
  const pace = usageLimitPace(window);
  const runsOutAt = forecast ? pace?.runsOutAt : null;
  const resetsAt = window.resets_at ? new Date(window.resets_at) : null;
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2 text-caption">
        <span>{window.label ? `${windowName(window)} · ${window.label}` : windowName(window)}</span>
        <span className={cn("tabular-nums", warning && "font-medium text-warning")}>{Math.round(window.used_percent)}%</span>
      </div>
      <PaceMeter window={window} elapsedPercent={pace?.elapsedPercent} className="w-full" />
      <div className="flex items-baseline justify-between gap-2 text-micro">
        {resetsAt && (
          <span className="text-muted-foreground" title={resetsAt.toLocaleString(i18n.language)}>
            {t(($) => $.usage_limits.resets_in, { time: formatDuration(resetsAt.getTime() - Date.now(), t) })}
          </span>
        )}
        {runsOutAt && (
          <span className="font-medium text-warning">
            {t(($) => $.usage_limits.runs_out_in, { time: formatDuration(runsOutAt - Date.now(), t) })}
          </span>
        )}
      </div>
    </div>
  );
}

function useWindowName() {
  const { t } = useT("layout");
  return (window: Pick<UsageLimitWindow, "window_minutes" | "id">, { short = false } = {}): string => {
    const minutes = window.window_minutes;
    if (minutes === MINUTES_PER_WEEK && !short) return t(($) => $.usage_limits.window_week);
    if (minutes > 0 && minutes % MINUTES_PER_DAY === 0) {
      return t(($) => $.usage_limits.window_days, { count: minutes / MINUTES_PER_DAY });
    }
    if (minutes > 0) return t(($) => $.usage_limits.window_hours, { count: Math.max(1, Math.round(minutes / 60)) });
    return window.id;
  };
}

function formatDuration(milliseconds: number, t: ReturnType<typeof useT<"layout">>["t"]): string {
  const totalMinutes = Math.max(1, Math.ceil(milliseconds / 60_000));
  const days = Math.floor(totalMinutes / MINUTES_PER_DAY);
  const hours = Math.floor((totalMinutes % MINUTES_PER_DAY) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return t(($) => $.usage_limits.duration_days_hours, { days, hours });
  if (hours > 0) return t(($) => $.usage_limits.duration_hours_minutes, { hours, minutes });
  return t(($) => $.usage_limits.duration_minutes, { minutes });
}
