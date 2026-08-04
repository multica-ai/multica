"use client";

import "@multica/cerebro-types";
import { KeyRound } from "lucide-react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  formatResetsIn,
  formatTokens,
  remainingBarColorClass,
  remainingPct,
  usedPct5h,
  usedPct7d,
} from "./account-usage";
import { useCerebroAccount } from "./use-cerebro-accounts";

const FLAG = "cerebro_agent_profile_card_details";

export function AgentProfileSettings({ agent }: { agent: Agent }) {
  const enabled = useFeatureFlag(FLAG);
  if (!enabled) {
    return <MetaRow label="Model" value={agent.model || "Default"} mono />;
  }

  const thinking = agent.thinking_level?.trim() || "Follow CLI config";
  const concurrency = `${agent.max_concurrent_tasks} ${agent.max_concurrent_tasks === 1 ? "task" : "tasks"}`;

  return (
    <>
      <MetaRow label="Model" value={agent.model || "Default"} mono />
      <MetaRow label="Thinking" value={thinking} pill />
      <MetaRow label="Concurrency" value={concurrency} pill />
    </>
  );
}

export function AgentProfileAccount({
  runtime,
}: {
  runtime: AgentRuntime | null;
}) {
  const enabled = useFeatureFlag(FLAG);
  return enabled ? <EnabledAgentProfileAccount runtime={runtime} /> : null;
}

function EnabledAgentProfileAccount({
  runtime,
}: {
  runtime: AgentRuntime | null;
}) {
  const { account, isLoading } = useCerebroAccount(runtime?.current_account_id);
  if (isLoading || !account) return null;

  return (
    <div className="mt-1 space-y-2" aria-label="Agent account status and usage">
      <div className="flex min-w-0 items-center gap-1.5 text-xs">
        <KeyRound className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate font-medium" title={account.login_identity}>
          {account.login_identity}
        </span>
        <span className="shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">
          {account.provider}
        </span>
        <AccountStatusBadge status={account.status} />
      </div>
      <UsageMeter
        label="Next 5h"
        usedPct={usedPct5h(account)}
        tokens={account.tokens_5h}
        resetsAt={account.usage_5h_resets_at}
      />
      <UsageMeter
        label="This week"
        usedPct={usedPct7d(account)}
        tokens={account.tokens_7d}
        resetsAt={account.usage_7d_resets_at}
      />
    </div>
  );
}

function MetaRow({
  label,
  value,
  mono,
  pill,
}: {
  label: string;
  value: string;
  mono?: boolean;
  pill?: boolean;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <span
        className={`min-w-0 truncate ${mono ? "font-mono text-[11px]" : ""} ${pill ? "rounded-md bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground" : ""}`}
        title={value}
      >
        {value}
      </span>
    </div>
  );
}

function AccountStatusBadge({ status }: { status: string }) {
  const [label, className] = accountStatusConfig(status);

  return (
    <span className={`ml-auto shrink-0 rounded px-1.5 py-0.5 text-[9px] font-medium uppercase tracking-wide ${className}`}>
      {label}
    </span>
  );
}

function accountStatusConfig(status: string): [string, string] {
  switch (status) {
    case "available":
      return ["Available", "bg-emerald-100 text-emerald-900 dark:bg-emerald-900/40 dark:text-emerald-200"];
    case "throttled":
      return ["Throttled", "bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"];
    case "paused":
      return ["Paused", "bg-muted text-muted-foreground"];
    case "no_runtime":
      return ["No runtime", "bg-muted text-muted-foreground"];
    default:
      return ["Unknown", "bg-muted text-muted-foreground"];
  }
}

function UsageMeter({
  label,
  usedPct,
  tokens,
  resetsAt,
}: {
  label: string;
  usedPct: number | null;
  tokens: number;
  resetsAt: string | null;
}) {
  const left = remainingPct(usedPct);
  const resetsIn = formatResetsIn(resetsAt);

  return (
    <div aria-label={`${label} usage`}>
      <div className="flex items-center justify-between text-[11px]">
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular-nums font-medium">
          {left !== null ? `${Math.round(left)}% left` : "No data yet"}
        </span>
      </div>
      <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
        {left !== null && (
          <div
            className={`h-full ${remainingBarColorClass(left)}`}
            style={{ width: `${left}%` }}
          />
        )}
      </div>
      <div className="mt-0.5 flex items-center justify-between gap-2 text-[10px] tabular-nums text-muted-foreground">
        <span>{formatTokens(tokens)} tok used</span>
        {resetsIn && <span>{resetsIn}</span>}
      </div>
    </div>
  );
}
