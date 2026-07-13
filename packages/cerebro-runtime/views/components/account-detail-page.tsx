"use client";

// FIR-3118: account detail page — full usage overview for one workspace
// account (Settings → Accounts → click a row). Shows remaining usage for
// the 5-hour and weekly windows (provider-reported via the daemon's OAuth
// usage probe) plus token-usage statistics over the last 7 days.
import { ArrowLeft, KeyRound, PauseCircle, TimerReset } from "lucide-react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { CerebroAccount } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "@multica/views/navigation";
import {
  useCerebroAccount,
  useCerebroAccountUsageHistory,
} from "./use-cerebro-accounts";
import {
  formatResetsIn,
  formatTokens,
  remainingBarColorClass,
  remainingPct,
  usedPct5h,
  usedPct7d,
} from "./account-usage";

export function AccountDetailPage({ accountId }: { accountId: string }) {
  const { account, isLoading } = useCerebroAccount(accountId);
  const wsPaths = useWorkspacePaths();
  const backHref = `${wsPaths.settings()}?tab=accounts`;

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-6">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </div>
    );
  }
  if (!account) {
    return (
      <div className="mx-auto w-full max-w-3xl space-y-4 px-4 py-6">
        <BackLink href={backHref} />
        <div className="flex flex-col items-center rounded-lg border px-4 py-10 text-center">
          <KeyRound className="h-6 w-6 text-muted-foreground/40" />
          <p className="mt-2 text-sm text-muted-foreground">Account not found</p>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6 px-4 py-6">
      <BackLink href={backHref} />

      <header className="flex items-center gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-muted/40">
          <KeyRound className="h-5 w-5 text-muted-foreground" />
        </div>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-lg font-semibold">
            {account.login_identity}
          </h1>
          <p className="text-xs uppercase tracking-wide text-muted-foreground">
            {account.provider}
          </p>
        </div>
        <StatusBadge account={account} />
      </header>

      <div className="grid gap-4 sm:grid-cols-2">
        <UsageWindowCard
          title="Next 5 hours"
          usedPct={usedPct5h(account)}
          resetsAt={account.usage_5h_resets_at}
          tokens={account.tokens_5h}
          tokensLabel="tokens used last 5 hours"
        />
        <UsageWindowCard
          title="This week"
          usedPct={usedPct7d(account)}
          resetsAt={account.usage_7d_resets_at}
          tokens={account.tokens_7d}
          tokensLabel="tokens used last 7 days"
        />
      </div>

      <UsageHistoryChart accountId={accountId} />

      <section className="rounded-lg border">
        <div className="border-b px-4 py-2.5">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Details
          </h2>
        </div>
        <dl className="divide-y">
          <DetailRow label="Runtimes">
            {account.runtime_count > 0
              ? `${account.available_runtime_count} of ${account.runtime_count} available`
              : "No runtimes linked"}
          </DetailRow>
          <DetailRow label="Throttled until">
            {formatTimestamp(account.throttled_until) ?? "Not throttled"}
          </DetailRow>
          <DetailRow label="Extra spend">
            {account.extra_spend_on === true ? "Enabled" : "Off"}
          </DetailRow>
          <DetailRow label="Manually paused">
            {account.paused_manual === true ? "Yes" : "No"}
          </DetailRow>
          <DetailRow label="Registered">
            {formatTimestamp(account.created_at) ?? "—"}
          </DetailRow>
        </dl>
      </section>
    </div>
  );
}

function BackLink({ href }: { href: string }) {
  return (
    <AppLink
      href={href}
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      <ArrowLeft className="h-3.5 w-3.5" />
      Accounts
    </AppLink>
  );
}

/**
 * One rolling-window card: big "% left" number, a bar that drains as the
 * window is used up (usagepal-style — the fill IS what's left), reset
 * countdown and the exact token total we measured. When the provider hasn't
 * reported a window percentage the card still shows the token total.
 */
function UsageWindowCard({
  title,
  usedPct,
  resetsAt,
  tokens,
  tokensLabel,
}: {
  title: string;
  usedPct: number | null;
  resetsAt: string | null;
  tokens: number;
  tokensLabel: string;
}) {
  const left = remainingPct(usedPct);
  const resetsIn = formatResetsIn(resetsAt);
  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {title}
        </h2>
        {resetsIn && (
          <span className="text-[11px] text-muted-foreground">{resetsIn}</span>
        )}
      </div>
      <div className="mt-2 text-2xl font-semibold tabular-nums">
        {left !== null ? `${Math.round(left)}% left` : "No data yet"}
      </div>
      {left !== null && (
        <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full ${remainingBarColorClass(left)}`}
            style={{ width: `${left}%` }}
          />
        </div>
      )}
      <p className="mt-2 text-xs tabular-nums text-muted-foreground">
        {formatTokens(tokens)} {tokensLabel}
      </p>
    </div>
  );
}

/** Hourly token usage over the last 7 days. */
function UsageHistoryChart({ accountId }: { accountId: string }) {
  const { data: history, isLoading } = useCerebroAccountUsageHistory(accountId);
  const series = (history ?? []).map((b) => ({
    hour: formatBucket(b.bucket),
    Tokens: b.tokens,
  }));
  const total = (history ?? []).reduce((s, b) => s + b.tokens, 0);

  return (
    <section className="rounded-lg border">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Token usage — last 7 days
        </h2>
        {history && history.length > 0 && (
          <span className="text-xs tabular-nums text-muted-foreground">
            {formatTokens(total)} total
          </span>
        )}
      </div>
      <div className="p-4">
        {isLoading ? (
          <div className="h-40 animate-pulse rounded bg-muted" />
        ) : series.length === 0 ? (
          <p className="flex h-40 items-center justify-center text-xs text-muted-foreground">
            No usage recorded yet.
          </p>
        ) : (
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={series} margin={{ top: 4, right: 8, bottom: 0, left: -8 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
              <XAxis
                dataKey="hour"
                tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }}
                axisLine={false}
                tickLine={false}
                minTickGap={24}
              />
              <YAxis
                tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }}
                axisLine={false}
                tickLine={false}
                allowDecimals={false}
                tickFormatter={(v: number) => formatTokens(v)}
              />
              <Tooltip
                contentStyle={{
                  fontSize: 12,
                  background: "hsl(var(--background))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: 6,
                }}
              />
              <Bar dataKey="Tokens" fill="hsl(var(--primary))" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>
    </section>
  );
}

function StatusBadge({ account }: { account: CerebroAccount }) {
  switch (account.status) {
    case "available":
      return <Badge className="bg-emerald-100 text-emerald-900 dark:bg-emerald-900/40 dark:text-emerald-200" label="Available" />;
    case "throttled":
      return (
        <Badge
          className="bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"
          icon={<TimerReset className="h-3 w-3" />}
          label="Throttled"
        />
      );
    case "paused":
      return (
        <Badge
          className="bg-muted text-muted-foreground"
          icon={<PauseCircle className="h-3 w-3" />}
          label="Paused"
        />
      );
    case "no_runtime":
      return <Badge className="bg-muted text-muted-foreground" label="No runtime" />;
    default:
      // Enum drift from a newer server downgrades to a generic badge.
      return <Badge className="bg-muted text-muted-foreground" label="Unknown" />;
  }
}

function Badge({
  className,
  icon,
  label,
}: {
  className: string;
  icon?: React.ReactNode;
  label: string;
}) {
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-[10px] font-medium uppercase tracking-wide ${className}`}
    >
      {icon}
      <span>{label}</span>
    </span>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4 px-4 py-2.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-xs font-medium">{children}</dd>
    </div>
  );
}

function formatTimestamp(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return null;
  return new Date(t).toLocaleString("en-GB", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatBucket(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return new Date(t).toLocaleString("en-GB", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}
