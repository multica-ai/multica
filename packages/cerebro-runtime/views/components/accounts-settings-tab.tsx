"use client";

import { useState } from "react";
import { KeyRound, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { CerebroAccount } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  useCerebroAccountsList,
  useDeleteCerebroAccount,
} from "./use-cerebro-accounts";
import {
  formatTokens,
  remainingBarColorClass,
  remainingPct,
  usedPct5h,
  usedPct7d,
} from "./account-usage";

/**
 * Settings → Konti tab (JEH-999). Workspace-wide list of cerebro_account
 * rows with a per-row Delete affordance. The list is the moved-here home
 * of "all accounts in the workspace" — the runtime-detail card now shows
 * only the runtime's own account.
 *
 * FIR-3118: each row shows remaining usage for the next 5 hours and the
 * rest of the week, and links to the account detail page (full usage
 * overview + statistics over time).
 */
export function AccountsSettingsTab() {
  const { data: accounts, isLoading } = useCerebroAccountsList();
  const deleteMut = useDeleteCerebroAccount();
  const wsPaths = useWorkspacePaths();
  const [pendingDelete, setPendingDelete] = useState<CerebroAccount | null>(null);

  const handleConfirmDelete = () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    deleteMut.mutate(target.id, {
      onSuccess: () => {
        toast.success(`Deleted ${target.login_identity}`);
        setPendingDelete(null);
      },
      onError: (e) => {
        toast.error(
          e instanceof Error ? e.message : "Failed to delete account",
        );
      },
    });
  };

  return (
    <section className="space-y-4">
      <header className="space-y-1">
        <h2 className="text-base font-semibold">Accounts</h2>
        <p className="text-sm text-muted-foreground">
          Login identities that daemon runtimes in the workspace are authenticated as. The daemon registers accounts automatically when a runtime starts up.
        </p>
      </header>

      <div className="rounded-lg border">
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Workspace accounts
          </span>
          <span className="text-xs text-muted-foreground">
            {accounts ? accounts.length : 0}
          </span>
        </div>

        {isLoading ? (
          <div className="px-4 py-8 text-center">
            <p className="text-xs text-muted-foreground">Loading…</p>
          </div>
        ) : !accounts || accounts.length === 0 ? (
          <div className="flex flex-col items-center px-4 py-8 text-center">
            <KeyRound className="h-6 w-6 text-muted-foreground/40" />
            <p className="mt-2 text-sm text-muted-foreground">
              No accounts registered yet
            </p>
          </div>
        ) : (
          <ul className="divide-y" aria-label="Workspace accounts">
            {accounts.map((acc) => (
              <li key={acc.id} className="flex items-center gap-3 px-4 py-3">
                <AppLink
                  href={wsPaths.accountDetail(acc.id)}
                  className="flex min-w-0 flex-1 items-center gap-3 rounded-sm transition-colors hover:text-foreground"
                >
                  <KeyRound className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">
                      {acc.login_identity}
                    </div>
                    <div className="text-xs uppercase tracking-wide text-muted-foreground">
                      {acc.provider}
                    </div>
                  </div>
                  <div className="hidden shrink-0 items-center gap-4 sm:flex">
                    <UsageMeter label="Next 5h" usedPct={usedPct5h(acc)} tokens={acc.tokens_5h} />
                    <UsageMeter label="This week" usedPct={usedPct7d(acc)} tokens={acc.tokens_7d} />
                  </div>
                </AppLink>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setPendingDelete(acc)}
                  aria-label={`Delete account ${acc.login_identity}`}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(v) => {
          if (!v && !deleteMut.isPending) setPendingDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete account?</AlertDialogTitle>
            <AlertDialogDescription>
              Delete <strong>{pendingDelete?.login_identity}</strong>?
              Runtimes pointing to this account will show &quot;Account unknown&quot;
              until the daemon reports a new one.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMut.isPending}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleConfirmDelete}
              disabled={deleteMut.isPending}
            >
              {deleteMut.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

/**
 * Compact remaining-usage meter for one rolling window. The primary signal
 * is how much of the window is LEFT (usagepal-style): big "% left" value and
 * a bar that drains as the account is used up. The token total we measured
 * ourselves is the secondary line underneath.
 */
function UsageMeter({
  label,
  usedPct,
  tokens,
}: {
  label: string;
  usedPct: number | null;
  tokens: number;
}) {
  const left = remainingPct(usedPct);
  return (
    <div className="w-32" aria-label={`${label} usage`}>
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
      <div className="mt-0.5 text-right text-[10px] tabular-nums text-muted-foreground">
        {formatTokens(tokens)} tok used
      </div>
    </div>
  );
}
