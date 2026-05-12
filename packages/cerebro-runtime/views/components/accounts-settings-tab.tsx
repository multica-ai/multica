"use client";

import { useState } from "react";
import { KeyRound, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { CerebroAccount } from "@multica/core/api";
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

/**
 * Settings → Konti tab (JEH-999). Workspace-wide list of cerebro_account
 * rows with a per-row Delete affordance. The list is the moved-here home
 * of "all accounts in the workspace" — the runtime-detail card now shows
 * only the runtime's own account.
 */
export function AccountsSettingsTab() {
  const { data: accounts, isLoading } = useCerebroAccountsList();
  const deleteMut = useDeleteCerebroAccount();
  const [pendingDelete, setPendingDelete] = useState<CerebroAccount | null>(null);

  const handleConfirmDelete = () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    deleteMut.mutate(target.id, {
      onSuccess: () => {
        toast.success(`Slettede ${target.login_identity}`);
        setPendingDelete(null);
      },
      onError: (e) => {
        toast.error(
          e instanceof Error ? e.message : "Kunne ikke slette kontoen",
        );
      },
    });
  };

  return (
    <section className="space-y-4">
      <header className="space-y-1">
        <h2 className="text-base font-semibold">Konti</h2>
        <p className="text-sm text-muted-foreground">
          Login-identiteter som daemon-runtimes i workspacet er authentificeret
          som. Daemon registrerer konti automatisk når en runtime starter op.
        </p>
      </header>

      <div className="rounded-lg border">
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Workspace-konti
          </span>
          <span className="text-xs text-muted-foreground">
            {accounts ? accounts.length : 0}
          </span>
        </div>

        {isLoading ? (
          <div className="px-4 py-8 text-center">
            <p className="text-xs text-muted-foreground">Indlæser…</p>
          </div>
        ) : !accounts || accounts.length === 0 ? (
          <div className="flex flex-col items-center px-4 py-8 text-center">
            <KeyRound className="h-6 w-6 text-muted-foreground/40" />
            <p className="mt-2 text-sm text-muted-foreground">
              Ingen konti registreret endnu
            </p>
          </div>
        ) : (
          <ul className="divide-y" aria-label="Workspace-konti">
            {accounts.map((acc) => (
              <li
                key={acc.id}
                className="flex items-center gap-3 px-4 py-3"
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
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setPendingDelete(acc)}
                  aria-label={`Slet konto ${acc.login_identity}`}
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
            <AlertDialogTitle>Slet konto?</AlertDialogTitle>
            <AlertDialogDescription>
              Vil du slette <strong>{pendingDelete?.login_identity}</strong>?
              Runtimes der peger på denne konto vil vise &quot;Konto ukendt&quot;
              indtil daemon rapporterer en ny.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMut.isPending}>
              Annullér
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleConfirmDelete}
              disabled={deleteMut.isPending}
            >
              {deleteMut.isPending ? "Sletter…" : "Slet"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
