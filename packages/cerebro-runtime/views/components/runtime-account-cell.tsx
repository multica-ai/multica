"use client";

// Pulls in cerebro-types so the augment.ts module-augmentation widens
// RuntimeDevice with current_account_id (JEH-999).
import "@multica/cerebro-types";
import { KeyRound } from "lucide-react";
import type { AgentRuntime } from "@multica/core/types/agent";
import { useCerebroAccount } from "./use-cerebro-accounts";

// Runtime-list "Account" column cell. Shows the cerebro_account login identity
// the daemon currently authenticates as, plus a small provider badge underneath.
// Mirrors the runtime-detail RuntimeAccountsCard fallback ("Konto ukendt") so
// users see the same state vocabulary on the list and the detail view.
export function RuntimeAccountCell({ runtime }: { runtime: AgentRuntime }) {
  const isCloud = runtime.runtime_mode === "cloud";
  // Cloud runtimes auth via Firtal Gateway, not a cerebro_account row — skip
  // the lookup and match the old CliCell behaviour (em-dash, no "Konto ukendt").
  const { account, isLoading } = useCerebroAccount(
    isCloud ? null : runtime.current_account_id,
  );

  if (isCloud || isLoading) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  if (!account) {
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground/60">
        <KeyRound className="h-3 w-3 shrink-0" />
        <span className="truncate">Konto ukendt</span>
      </span>
    );
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <KeyRound className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="truncate text-xs font-medium">
          {account.login_identity}
        </span>
        <span className="truncate text-[10px] uppercase tracking-wide text-muted-foreground">
          {account.provider}
        </span>
      </span>
    </span>
  );
}
