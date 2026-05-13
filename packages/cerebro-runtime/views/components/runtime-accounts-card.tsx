"use client";

// Pulls in cerebro-types so the augment.ts module-augmentation widens
// RuntimeDevice with current_account_id for this file (JEH-999).
import "@multica/cerebro-types";

import { KeyRound } from "lucide-react";
import { useState, useEffect } from "react";
import type { AgentRuntime } from "@multica/core/types/agent";
import type { CerebroAccount } from "@multica/core/api";
import { useCerebroAccount } from "./use-cerebro-accounts";

interface RuntimeAccountsCardProps {
  runtime: AgentRuntime;
}

type AccountStatus = CerebroAccount["status"];

function StatusDot({ status, unpauseAt }: { status: AccountStatus; unpauseAt: string | null }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (status !== "throttled") return;
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, [status]);

  if (status === "available") {
    return (
      <span className="flex items-center gap-1 text-[11px] font-medium text-emerald-600">
        <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
        Ledig
      </span>
    );
  }
  if (status === "throttled" && unpauseAt) {
    return (
      <span className="flex items-center gap-1 text-[11px] font-medium text-amber-600">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
        Throttlet — {formatRemaining(unpauseAt, now)}
      </span>
    );
  }
  if (status === "paused") {
    return (
      <span className="flex items-center gap-1 text-[11px] font-medium text-rose-600">
        <span className="h-1.5 w-1.5 rounded-full bg-rose-500" />
        Pauset
      </span>
    );
  }
  return null;
}

function formatRemaining(until: string, nowMs: number): string {
  const ms = new Date(until).getTime() - nowMs;
  if (ms <= 0) return "snart";
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.round(totalSeconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) return remMinutes ? `${hours}t ${remMinutes}m` : `${hours}t`;
  const days = Math.floor(hours / 24);
  const remHours = hours % 24;
  return remHours ? `${days}d ${remHours}t` : `${days}d`;
}

/**
 * Runtime-detail rail card showing the cerebro_account this runtime is
 * authenticated as. Shows availability status alongside identity.
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
          <li className="flex items-center gap-2 px-4 py-2">
            <KeyRound className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium">
                {account.login_identity}
              </div>
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                {account.provider}
              </div>
            </div>
            {account.status !== "no_runtime" && (
              <StatusDot
                status={account.status}
                unpauseAt={account.nearest_unpause_at}
              />
            )}
          </li>
          {account.runtime_count > 0 && (
            <li className="border-t px-4 py-1.5">
              <span className="text-[11px] text-muted-foreground">
                {account.available_runtime_count}/{account.runtime_count} runtimes ledige
              </span>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
