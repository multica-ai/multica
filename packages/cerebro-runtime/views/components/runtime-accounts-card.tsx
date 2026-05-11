"use client";

import { KeyRound } from "lucide-react";
import { useCerebroAccountsList } from "./use-cerebro-accounts";

export function RuntimeAccountsCard() {
  const { data: accounts, isLoading } = useCerebroAccountsList();

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <span className="text-xs font-semibold">Konti</span>
        <span className="text-xs text-muted-foreground">
          {accounts ? accounts.length : 0}
        </span>
      </div>
      {isLoading ? (
        <div className="px-4 py-6 text-center">
          <p className="text-xs text-muted-foreground">Indlæser…</p>
        </div>
      ) : !accounts || accounts.length === 0 ? (
        <div className="flex flex-col items-center px-4 py-6 text-center">
          <KeyRound className="h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">
            Ingen konti registreret endnu
          </p>
        </div>
      ) : (
        <ul className="divide-y">
          {accounts.map((acc) => (
            <li key={acc.id} className="flex items-center gap-2 px-4 py-2">
              <KeyRound className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium">
                  {acc.login_identity}
                </div>
                <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  {acc.provider}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
