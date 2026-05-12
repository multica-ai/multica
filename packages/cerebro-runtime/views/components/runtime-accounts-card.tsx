"use client";

// Pulls in cerebro-types so the augment.ts module-augmentation widens
// RuntimeDevice with current_account_id for this file (JEH-999).
import "@multica/cerebro-types";

import { KeyRound } from "lucide-react";
import type { AgentRuntime } from "@multica/core/types/agent";
import { useCerebroAccount } from "./use-cerebro-accounts";

interface RuntimeAccountsCardProps {
  runtime: AgentRuntime;
}

/**
 * Runtime-detail rail card showing the cerebro_account this runtime is
 * authenticated as. Three states:
 *   - loading: shimmer placeholder
 *   - account known: provider + login_identity row
 *   - account unknown (current_account_id null or the id doesn't resolve
 *     to a workspace account): "Konto ukendt — daemon har ikke rapporteret
 *     endnu" empty state. JEH-997 will populate current_account_id from
 *     the daemon heartbeat; until then every runtime renders the unknown
 *     state on purpose.
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
          </li>
        </ul>
      )}
    </div>
  );
}
