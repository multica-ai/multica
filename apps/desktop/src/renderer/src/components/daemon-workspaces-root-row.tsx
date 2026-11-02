import { useCallback, useEffect, useState } from "react";
import { AlertCircle } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { SettingsRow } from "@multica/views/settings";
import { toast } from "sonner";
import { useT } from "@multica/views/i18n";
import type {
  DaemonStatus,
  WorkspacesRootInfo,
} from "../../../shared/daemon-types";
import { useLocalDaemonActivity } from "./daemon-activity";

/**
 * Settings row for the daemon's workspaces root (where task execution
 * environments live). Desktop-only.
 *
 * The interaction order is deliberate and load-bearing: clicking "Change…"
 * must surface the risk confirmation (GC sweeps the root, no migration of the
 * old tree, daemon restart interrupts active runs) BEFORE the native folder
 * picker opens, so the user knows to pick a dedicated empty folder. Only a
 * confirmed picker selection writes the config (via the bundled CLI) and
 * restarts the daemon.
 */
export function DaemonWorkspacesRootRow({ status }: { status: DaemonStatus }) {
  const { t } = useT("settings");
  const [info, setInfo] = useState<WorkspacesRootInfo | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [confirmMode, setConfirmMode] = useState<"change" | "reset" | null>(
    null,
  );
  const [applying, setApplying] = useState(false);
  const { affectedTasks } = useLocalDaemonActivity(status.daemonId);

  // Refetch when the daemon profile lands: right after launch there is no
  // Desktop-owned profile yet and the read returns null, so the row would
  // otherwise stay on the placeholder until the next mount.
  const profile = status.profile ?? null;
  useEffect(() => {
    let cancelled = false;
    window.daemonAPI
      .getWorkspacesRoot()
      .then((result) => {
        if (cancelled) return;
        setInfo(result);
        setLoaded(true);
      })
      .catch(() => {
        if (cancelled) return;
        setInfo(null);
        setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [profile]);

  const applyPath = useCallback(
    async (path: string) => {
      setApplying(true);
      try {
        const result = await window.daemonAPI.setWorkspacesRoot(path);
        if (!result.success) {
          toast.error(
            t(($) => $.desktop.daemon.workspaces_root_update_failed),
            { description: result.error },
          );
          return;
        }
        const restart = await window.daemonAPI.restart();
        // The config is persisted either way, so refresh the displayed path
        // even when the restart fails — the new path applies on next start.
        const fresh = await window.daemonAPI.getWorkspacesRoot().catch(
          () => null,
        );
        if (fresh) setInfo(fresh);
        if (!restart.success) {
          toast.error(t(($) => $.desktop.daemon.restart_failed), {
            description: restart.error,
          });
          return;
        }
        toast.success(t(($) => $.desktop.daemon.workspaces_root_updated), {
          description: t(($) => $.desktop.daemon.restarting_description),
        });
      } finally {
        setApplying(false);
      }
    },
    [t],
  );

  // Confirming the risk dialog closes it and only THEN opens the native
  // folder picker — that ordering is the point of the confirmation.
  const handleConfirmChange = useCallback(async () => {
    setConfirmMode(null);
    const picked = await window.desktopAPI.pickDirectory();
    // Cancelling the picker writes nothing and does not restart the daemon.
    if (!picked.ok || !picked.path) {
      if (picked.reason && picked.reason !== "cancelled") {
        toast.error(
          picked.error ??
            t(($) => $.desktop.daemon.workspaces_root_update_failed),
        );
      }
      return;
    }
    void applyPath(picked.path);
  }, [applyPath, t]);

  const handleConfirmReset = useCallback(() => {
    setConfirmMode(null);
    void applyPath("");
  }, [applyPath]);

  // Editing is read-only when an env var pins the root (a config write would
  // silently lose to it) or when the daemon runs outside the app's control —
  // mirroring the lifecycle toggles above this row. See #3916.
  const externallyManaged = status.externallyManaged === true;
  const managedNote =
    info?.envOverride != null
      ? t(($) => $.desktop.daemon.workspaces_root_env_managed)
      : externallyManaged
        ? t(($) => $.desktop.daemon.managed_externally)
        : null;
  const changeDisabled = !loaded || !info || managedNote !== null || applying;

  return (
    <>
      <SettingsRow
        label={t(($) => $.desktop.daemon.workspaces_root_title)}
        description={
          <>
            <span
              className="block truncate font-mono"
              title={info ? info.effectivePath : undefined}
            >
              {!loaded
                ? t(($) => $.desktop.daemon.cli_checking)
                : (info?.effectivePath ?? "—")}
            </span>
            {managedNote ? (
              <span className="mt-0.5 block">{managedNote}</span>
            ) : null}
          </>
        }
      >
        <div className="flex flex-wrap items-center justify-end gap-1.5">
          <Button
            variant="outline"
            size="sm"
            disabled={changeDisabled}
            onClick={() => setConfirmMode("change")}
          >
            {t(($) => $.desktop.daemon.workspaces_root_change)}
          </Button>
          {info?.customPath ? (
            <Button
              variant="ghost"
              size="sm"
              disabled={changeDisabled}
              onClick={() => setConfirmMode("reset")}
            >
              {t(($) => $.desktop.daemon.workspaces_root_reset)}
            </Button>
          ) : null}
        </div>
      </SettingsRow>

      <WorkspacesRootConfirmDialog
        mode={confirmMode}
        affectedCount={affectedTasks.length}
        onOpenChange={(open) => {
          if (!open) setConfirmMode(null);
        }}
        onConfirmChange={() => void handleConfirmChange()}
        onConfirmReset={handleConfirmReset}
      />
    </>
  );
}

/**
 * Pre-picker risk confirmation for changing / resetting the workspaces root.
 * Styled after StopConfirmDialog in daemon-runtime-card.tsx: the GC risk is
 * change-only (a fresh root must be a dedicated empty folder), while the
 * no-migration and restart-interruption risks apply to both flows.
 */
function WorkspacesRootConfirmDialog({
  mode,
  affectedCount,
  onOpenChange,
  onConfirmChange,
  onConfirmReset,
}: {
  mode: "change" | "reset" | null;
  affectedCount: number;
  onOpenChange: (open: boolean) => void;
  onConfirmChange: () => void;
  onConfirmReset: () => void;
}) {
  const { t } = useT("settings");

  const risks: string[] = [];
  if (mode === "change") {
    risks.push(
      t(($) => $.desktop.daemon.workspaces_root_risk_gc),
      t(($) => $.desktop.daemon.workspaces_root_risk_no_migration),
      affectedCount > 0
        ? t(($) => $.desktop.daemon.workspaces_root_risk_restart_active, {
            count: affectedCount,
          })
        : t(($) => $.desktop.daemon.workspaces_root_risk_restart),
    );
  } else if (mode === "reset") {
    risks.push(
      t(($) => $.desktop.daemon.workspaces_root_reset_risk_no_migration),
      affectedCount > 0
        ? t(($) => $.desktop.daemon.workspaces_root_reset_risk_restart_active, {
            count: affectedCount,
          })
        : t(($) => $.desktop.daemon.workspaces_root_reset_risk_restart),
    );
  }

  return (
    <Dialog open={mode !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-body font-semibold">
              {mode === "reset"
                ? t(($) => $.desktop.daemon.workspaces_root_reset_confirm_title)
                : t(
                    ($) => $.desktop.daemon.workspaces_root_change_confirm_title,
                  )}
            </DialogTitle>
          </DialogHeader>
        </div>
        <DialogDescription
          render={<div />}
          className="text-caption leading-relaxed"
        >
          <ul className="list-disc space-y-2 pl-4">
            {risks.map((risk) => (
              <li key={risk}>{risk}</li>
            ))}
          </ul>
        </DialogDescription>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.desktop.daemon.cancel)}
          </Button>
          <Button
            onClick={mode === "reset" ? onConfirmReset : onConfirmChange}
          >
            {mode === "reset"
              ? t(($) => $.desktop.daemon.workspaces_root_reset_confirm)
              : t(($) => $.desktop.daemon.workspaces_root_confirm_pick)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
