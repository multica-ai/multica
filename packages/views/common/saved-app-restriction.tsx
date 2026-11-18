"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useT } from "../i18n";

/** A one-time recovery action, shown only while saved restrictions apply. */
export function SavedAppRestriction({ kind, onRestore }: {
  kind: "account" | "agent" | "catalog";
  onRestore?: () => Promise<unknown>;
}) {
  const { t } = useT("settings");
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const action = kind === "catalog"
    ? t(($) => $.connected_apps.saved_access.open_catalog)
    : t(($) => $.connected_apps.saved_access.allow_actions);

  return (
    <div className="flex flex-wrap items-center gap-3 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-caption">
      <p className="min-w-0 flex-1">{t(($) => $.connected_apps.saved_access[kind])}</p>
      {onRestore ? (
        <>
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>{action}</Button>
          <AlertDialog open={open} onOpenChange={(value) => !pending && setOpen(value)}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{action}</AlertDialogTitle>
                <AlertDialogDescription>
                  {kind === "catalog"
                    ? t(($) => $.connected_apps.saved_access.catalog_confirm)
                    : kind === "agent"
                      ? t(($) => $.connected_apps.saved_access.agent_confirm)
                      : t(($) => $.connected_apps.saved_access.account_confirm)}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={pending}>{t(($) => $.connected_apps.saved_access.cancel)}</AlertDialogCancel>
                <Button disabled={pending} aria-busy={pending} onClick={async () => {
                  setPending(true);
                  try {
                    await onRestore();
                    setOpen(false);
                  } catch {
                    toast.error(t(($) => $.connected_apps.toast_failed));
                  } finally {
                    setPending(false);
                  }
                }}>{action}</Button>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </>
      ) : <span className="text-muted-foreground">{t(($) => $.connected_apps.saved_access.admin_required)}</span>}
    </div>
  );
}
