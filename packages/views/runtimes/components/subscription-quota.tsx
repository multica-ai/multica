"use client";

import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  requestRuntimeSubscriptionQuota,
  runtimeKeys,
  runtimeSubscriptionQuotaOptions,
  type SubscriptionQuotaSnapshot,
} from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useLocale, useT } from "../../i18n";

const snapshotMaxAgeMs = 15 * 60_000;

function quotaWindows(snapshot: SubscriptionQuotaSnapshot) {
  return [
    { kind: "primary", window: snapshot.primary },
    { kind: "secondary", window: snapshot.secondary },
  ].flatMap(({ kind, window }) => (window ? [{ kind, window }] : []));
}

function quotaIsStale(snapshot: SubscriptionQuotaSnapshot, now: number) {
  return (
    now - snapshot.observedAt * 1000 >= snapshotMaxAgeMs ||
    (snapshot.limitResetsAt !== undefined && snapshot.limitResetsAt * 1000 <= now) ||
    quotaWindows(snapshot).some(({ window }) =>
      window.resetsAt !== undefined && window.resetsAt * 1000 <= now,
    )
  );
}

function formatWindowDuration(minutes: number, locale: string) {
  const [unit, amount] = minutes % 1440 === 0
    ? ["day", minutes / 1440] as const
    : minutes % 60 === 0
      ? ["hour", minutes / 60] as const
      : ["minute", minutes] as const;
  return new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "short" }).format(amount);
}

function formatUsed(percent: number, locale: string) {
  return new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 1 }).format(percent / 100);
}

export function canShowSubscriptionQuota(runtime: AgentRuntime, userId: string | undefined) {
  return !!userId && runtime.owner_id === userId &&
    (runtime.provider === "codex" || runtime.provider === "claude" || runtime.provider === "dsh") &&
    runtime.runtime_mode === "local" && !runtime.profile_id;
}

export function SubscriptionQuotaSummary({ snapshot, now }: {
  snapshot?: SubscriptionQuotaSnapshot;
  now: number;
}) {
  const locale = useLocale();
  const { t } = useT("runtimes");
  if (!snapshot) return <span className="text-muted-foreground">—</span>;
  if (quotaIsStale(snapshot, now)) {
    return <span className="text-muted-foreground">{t(($) => $.quota.stale)}</span>;
  }
  if (snapshot.exhausted) return <span>{t(($) => $.quota.exhausted)}</span>;
  return (
    <span className="tabular-nums">
      {quotaWindows(snapshot).map(({ window }) => window.windowDurationMins
        ? `${formatWindowDuration(window.windowDurationMins, locale)} ${formatUsed(window.usedPercent, locale)}`
        : formatUsed(window.usedPercent, locale),
      ).join(" · ")}
    </span>
  );
}

type SubscriptionQuotaCardProps = {
  runtime: AgentRuntime;
  wsId: string;
  now: number;
  online: boolean;
};

export function SubscriptionQuotaCard(props: SubscriptionQuotaCardProps) {
  return props.runtime.provider === "codex"
    ? <CodexQuotaCard {...props} />
    : <ObservedQuotaCard runtime={props.runtime} now={props.now} />;
}

function ObservedQuotaCard({ runtime, now }: { runtime: AgentRuntime; now: number }) {
  const { t } = useT("runtimes");
  const snapshot = runtime.subscription_quota;
  return (
    <div className="mb-4 space-y-3 rounded-lg border border-border p-4">
      <h3 className="text-sm font-medium">
        {t(($) => $.quota.title, { provider: runtime.provider === "claude" ? "Claude Code" : "DSH" })}
      </h3>
      {snapshot && <QuotaSnapshot snapshot={snapshot} now={now} />}
      <p className="text-xs text-muted-foreground">
        {runtime.provider === "dsh" ? t(($) => $.quota.dsh_observed) : t(($) => $.quota.claude_observed)}
      </p>
    </div>
  );
}

function CodexQuotaCard({ runtime, wsId, now, online }: SubscriptionQuotaCardProps) {
  const { t } = useT("runtimes");
  const qc = useQueryClient();
  const quota = useQuery(runtimeSubscriptionQuotaOptions(wsId, runtime.id));
  const refresh = useMutation({
    mutationFn: (ifStale: boolean) => requestRuntimeSubscriptionQuota(runtime.id, ifStale),
    onSuccess: async (report) => {
      const key = runtimeKeys.subscriptionQuota(wsId, runtime.id);
      await qc.cancelQueries({ queryKey: key });
      qc.setQueryData(key, report);
    },
  });
  const status = quota.data?.status;
  const isRunning = status === "pending";
  const unsupported = status === "unsupported";
  const autoRequested = useRef(false);
  const requestRefresh = refresh.mutate;

  useEffect(() => {
    if (online && quota.isSuccess && !unsupported && !autoRequested.current) {
      autoRequested.current = true;
      if (!isRunning) requestRefresh(true);
    }
  }, [online, quota.isSuccess, isRunning, unsupported, requestRefresh]);

  const snapshot = quota.data ? quota.data.snapshot : runtime.subscription_quota;
  useEffect(() => {
    if ((status === "complete" || status === "unavailable") &&
        snapshot?.observedAt !== runtime.subscription_quota?.observedAt) {
      void qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    }
  }, [qc, runtime.subscription_quota?.observedAt, snapshot?.observedAt, status, wsId]);

  const busy = refresh.isPending || (isRunning && !quota.isError);

  return (
    <div className="mb-4 space-y-3 rounded-lg border border-border p-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium">{t(($) => $.quota.title, { provider: "Codex" })}</h3>
        <Button size="sm" variant="outline" disabled={!online || busy || unsupported || quota.isPending} onClick={() => requestRefresh(false)}>
          {busy ? t(($) => $.quota.refreshing) : t(($) => $.quota.refresh)}
        </Button>
      </div>
      {snapshot && <QuotaSnapshot snapshot={snapshot} now={now} />}
      <p role="status" className="text-xs text-muted-foreground empty:hidden">
        {unsupported ? t(($) => $.quota.upgrade_daemon)
          : !online ? t(($) => $.quota.offline)
          : busy ? t(($) => $.quota.refreshing)
          : status === "timeout" ? t(($) => $.quota.timeout)
          : quota.isError || refresh.isError || status === "failed" ? t(($) => $.quota.failed)
          : status === "unavailable" ? t(($) => $.quota.unavailable)
          : !snapshot ? t(($) => $.quota.empty)
          : null}
      </p>
    </div>
  );
}

function QuotaSnapshot({ snapshot, now }: { snapshot: SubscriptionQuotaSnapshot; now: number }) {
  const locale = useLocale();
  const { t } = useT("runtimes");
  const stale = quotaIsStale(snapshot, now);
  const dateFormat = new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" });
  return (
    <>
      {snapshot.exhausted && <p className="text-sm">{t(($) => $.quota.exhausted)}</p>}
      {snapshot.limitResetsAt !== undefined && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.quota.resets, { time: dateFormat.format(snapshot.limitResetsAt * 1000) })}
        </p>
      )}
      {(snapshot.primary || snapshot.secondary) && (
        <div className="grid gap-3 sm:grid-cols-2">
          {quotaWindows(snapshot).map(({ kind, window }) => {
            const label = window.windowDurationMins
              ? formatWindowDuration(window.windowDurationMins, locale)
              : kind === "primary" ? t(($) => $.quota.primary) : t(($) => $.quota.secondary);
            const used = formatUsed(window.usedPercent, locale);
            return (
              <div key={kind} className="space-y-1.5">
                <div className="flex justify-between gap-2 text-sm">
                  <span>{label}</span>
                  <span className="tabular-nums">{t(($) => $.quota.used, { percent: used })}</span>
                </div>
                <progress
                  aria-label={label}
                  className="block h-2 w-full accent-primary"
                  max={100}
                  value={window.usedPercent}
                />
                {window.resetsAt !== undefined && (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.quota.resets, { time: dateFormat.format(window.resetsAt * 1000) })}
                  </p>
                )}
              </div>
            );
          })}
        </div>
      )}
      <p className="text-xs text-muted-foreground">
        {t(($) => $.quota.observed, { time: dateFormat.format(snapshot.observedAt * 1000) })}
        {stale && ` · ${t(($) => $.quota.stale)}`}
      </p>
    </>
  );
}
