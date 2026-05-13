"use client";

// Pulls in cerebro-types so the augment.ts module-augmentation widens
// RuntimeDevice with current_account_id for this file (JEH-999).
import "@multica/cerebro-types";
import { KeyRound, PauseCircle, TimerReset, Sparkles } from "lucide-react";
import type { CerebroAccount } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types/agent";
import { useCerebroAccount } from "./use-cerebro-accounts";

interface RuntimeAccountsCardProps {
  runtime: AgentRuntime;
}


/**
 * Runtime-detail rail card showing the cerebro_account this runtime is
 * authenticated as. Three states:
 *   - loading: shimmer placeholder
 *   - account known: provider + login_identity row plus JEH-998 status badges
 *     and usage bar, and JEH-881 availability count
 *   - account unknown (current_account_id null or the id doesn't resolve
 *     to a workspace account): "Konto ukendt — daemon har ikke rapporteret
 *     endnu" empty state.
 */
export function RuntimeAccountsCard({ runtime }: RuntimeAccountsCardProps) {
  const { account, isLoading } = useCerebroAccount(runtime.current_account_id);

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <span className="text-xs font-semibold">Konto</span>
      </div>
      {isLoading ? (
        <div className="px-4 py-6 text-center">
          <p className="text-xs text-muted-foreground">Indlæser…</p>
        </div>
      ) : !account ? (
        <div className="flex flex-col items-center px-4 py-6 text-center">
          <KeyRound className="h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">
            Konto ukendt — daemon har ikke rapporteret endnu
          </p>
        </div>
      ) : (
        <ul>
          <AccountRow account={account} />
        </ul>
      )}
    </div>
  );
}

function AccountRow({ account }: { account: CerebroAccount }) {
  const throttled = isThrottled(account.throttled_until);
  const pausedManual = account.paused_manual === true;
  const extraSpend = account.extra_spend_on === true;
  const pct = clampPct(account.usage_window_pct);

  return (
    <li className="px-4 py-2.5">
      <div className="flex items-center gap-2">
        <KeyRound className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-medium">
            {account.login_identity}
          </div>
          <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
            {account.provider}
          </div>
        </div>
        <StatusBadges
          throttled={throttled}
          pausedManual={pausedManual}
          extraSpend={extraSpend}
        />
      </div>
      {pct !== null && (
        <div className="mt-2 flex items-center gap-2">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
            <div
              className={usageBarColor(pct)}
              style={{ width: `${pct}%` }}
            />
          </div>
          <span className="text-[11px] tabular-nums text-muted-foreground">
            {Math.round(pct)}%
          </span>
        </div>
      )}
    </li>
  );
}

function StatusBadges({
  throttled,
  pausedManual,
  extraSpend,
}: {
  throttled: boolean;
  pausedManual: boolean;
  extraSpend: boolean;
}) {
  if (!throttled && !pausedManual && !extraSpend) return null;
  return (
    <div className="flex shrink-0 items-center gap-1">
      {throttled && (
        <Badge tone="warning" icon={<TimerReset className="h-3 w-3" />} label="Throttled" />
      )}
      {pausedManual && (
        <Badge tone="muted" icon={<PauseCircle className="h-3 w-3" />} label="Pauset" />
      )}
      {extraSpend && (
        <Badge tone="info" icon={<Sparkles className="h-3 w-3" />} label="Extra" />
      )}
    </div>
  );
}

function Badge({
  tone,
  icon,
  label,
}: {
  tone: "warning" | "muted" | "info";
  icon: React.ReactNode;
  label: string;
}) {
  const cls =
    tone === "warning"
      ? "bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"
      : tone === "info"
        ? "bg-blue-100 text-blue-900 dark:bg-blue-900/40 dark:text-blue-200"
        : "bg-muted text-muted-foreground";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide ${cls}`}
      title={label}
    >
      {icon}
      <span>{label}</span>
    </span>
  );
}

function isThrottled(until: string | null | undefined): boolean {
  if (!until) return false;
  const t = Date.parse(until);
  if (Number.isNaN(t)) return false;
  return t > Date.now();
}

function clampPct(v: number | null | undefined): number | null {
  if (v === null || v === undefined) return null;
  if (Number.isNaN(v)) return null;
  if (v < 0) return 0;
  if (v > 100) return 100;
  return v;
}

function usageBarColor(pct: number): string {
  if (pct >= 90) return "h-full bg-red-500";
  if (pct >= 75) return "h-full bg-amber-500";
  return "h-full bg-emerald-500";
}
