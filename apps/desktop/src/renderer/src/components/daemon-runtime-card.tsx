import { useState, useEffect, useCallback, useMemo } from "react";
import {
  AlertCircle,
  Play,
  Square,
  RotateCw,
  Activity,
  ScrollText,
  LogIn,
  Info,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import { useT } from "@multica/views/i18n";
import { DaemonPanel } from "./daemon-panel";
import { reauthenticateDaemon } from "../platform/daemon-reauth";
import type { DaemonStatus } from "../../../shared/daemon-types";
import { daemonStateLabel } from "./daemon-i18n";

/**
 * Desktop-only controls for the daemon embedded in this Electron app. The
 * shared runtimes page renders this inside the selected local machine header.
 */
export function DaemonRuntimeActions() {
  const { t } = useT("settings");
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [panelOpen, setPanelOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);

  const wsId = useWorkspaceId();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const localRuntimeIds = useMemo(() => {
    if (!status.daemonId) return new Set<string>();
    return new Set(
      runtimes
        .filter((r) => r.daemon_id === status.daemonId)
        .map((r) => r.id),
    );
  }, [runtimes, status.daemonId]);

  const runtimeCount = localRuntimeIds.size;

  const affectedTasks = useMemo(
    () =>
      snapshot.filter(
        (t) =>
          localRuntimeIds.has(t.runtime_id) &&
          (t.status === "running" || t.status === "dispatched"),
      ),
    [snapshot, localRuntimeIds],
  );

  useEffect(() => {
    window.daemonAPI.getStatus().then((s) => setStatus(s));
    const unsub = window.daemonAPI.onStatusChange((s) => {
      setStatus(s);
      setActionLoading(false);
    });
    return unsub;
  }, []);

  const handleStart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.start();
    if (!result.success) {
      setActionLoading(false);
      toast.error(t(($) => $.desktop.daemon.start_failed), {
        description: result.error,
      });
    }
  }, [t]);

  const performStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    if (!result.success) {
      toast.error(t(($) => $.desktop.daemon.stop_failed), {
        description: result.error,
      });
    }
  }, [t]);

  const handleStopClick = useCallback(() => {
    if (affectedTasks.length === 0) {
      void performStop();
    } else {
      setConfirmStop(true);
    }
  }, [affectedTasks.length, performStop]);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.restart();
    if (!result.success) {
      toast.error(t(($) => $.desktop.daemon.restart_failed), {
        description: result.error,
      });
      return;
    }
    toast.success(t(($) => $.desktop.daemon.restarting), {
      description: t(($) => $.desktop.daemon.restarting_description),
    });
  }, [t]);

  const handleRetryInstall = useCallback(async () => {
    setActionLoading(true);
    try {
      await window.daemonAPI.retryInstall();
    } finally {
      setActionLoading(false);
    }
  }, []);

  const handleReauth = useCallback(async () => {
    setActionLoading(true);
    await reauthenticateDaemon(t);
    // onStatusChange resets actionLoading on the next status push; reset here
    // too in case reauth logged out (unmount) or produced no status change.
    setActionLoading(false);
  }, [t]);

  const isRunning = status.state === "running";
  // The daemon runs somewhere the app can't drive (e.g. inside WSL2): the
  // lifecycle CLI acts on the host process namespace and can't reach it. Hide
  // Stop/Restart so they don't silently no-op, mirroring the Settings tab. The
  // real guard is in the main process (stopDaemon/restartDaemon); this is the
  // matching UX. See #3916.
  const externallyManaged = status.externallyManaged === true;
  const isStopped =
    status.state === "stopped" || status.state === "recovery_paused";
  const isCliMissing = status.state === "cli_not_found";
  const isAuthExpired = status.state === "auth_expired";
  const isTransitioning =
    status.state === "starting" || status.state === "stopping";
  const isInstalling = status.state === "installing_cli";

  return (
    <>
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {isRunning && (
          <>
            <Button size="sm" variant="ghost" onClick={() => setPanelOpen(true)}>
              <ScrollText className="size-3.5 mr-1.5" />
              {t(($) => $.desktop.daemon.view_logs)}
            </Button>
            {externallyManaged ? (
              <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
                <Info className="size-3.5 shrink-0" />
                {t(($) => $.desktop.daemon.managed_externally)}
              </span>
            ) : (
              <>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleRestart}
                  disabled={actionLoading}
                >
                  <RotateCw className="size-3.5 mr-1.5" />
                  {t(($) => $.desktop.daemon.restart)}
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={handleStopClick}
                  disabled={actionLoading}
                >
                  <Square className="size-3.5 mr-1.5" />
                  {t(($) => $.desktop.daemon.stop)}
                </Button>
              </>
            )}
          </>
        )}

        {isStopped && (
          <Button size="sm" onClick={handleStart} disabled={actionLoading}>
            {actionLoading ? (
              <Activity className="size-3.5 mr-1.5 animate-pulse" />
            ) : (
              <Play className="size-3.5 mr-1.5" />
            )}
            {t(($) => $.desktop.daemon.start)}
          </Button>
        )}

        {isCliMissing && (
          <Button
            size="sm"
            variant="outline"
            onClick={handleRetryInstall}
            disabled={actionLoading}
          >
            <RotateCw className="size-3.5 mr-1.5" />
            {t(($) => $.desktop.daemon.retry_setup)}
          </Button>
        )}

        {isAuthExpired && (
          <>
            <span className="inline-flex items-center gap-1.5 text-caption text-destructive">
              <AlertCircle className="size-3.5 shrink-0" />
              {t(($) => $.desktop.daemon.signin_expired)}
            </span>
            <Button size="sm" onClick={handleReauth} disabled={actionLoading}>
              {actionLoading ? (
                <Activity className="size-3.5 mr-1.5 animate-pulse" />
              ) : (
                <LogIn className="size-3.5 mr-1.5" />
              )}
              {t(($) => $.desktop.daemon.signin_again)}
            </Button>
          </>
        )}

        {(isTransitioning || isInstalling) && (
          <Button size="sm" variant="outline" disabled>
            <Activity className="size-3.5 mr-1.5 animate-pulse" />
            {daemonStateLabel(status.state, t)}
          </Button>
        )}
      </div>

      <DaemonPanel
        open={panelOpen}
        onOpenChange={setPanelOpen}
        status={status}
        runtimeCount={runtimeCount}
      />

      <StopConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        affectedCount={affectedTasks.length}
        onConfirm={() => {
          setConfirmStop(false);
          void performStop();
        }}
      />
    </>
  );
}

// ---------- Sub-components ----------

function StopConfirmDialog({
  open,
  onOpenChange,
  affectedCount,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  affectedCount: number;
  onConfirm: () => void;
}) {
  const { t } = useT("settings");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-body font-semibold">
              {t(($) => $.desktop.daemon.stop_confirm_title, {
                count: affectedCount,
              })}
            </DialogTitle>
            <DialogDescription className="text-caption leading-relaxed">
              {t(($) => $.desktop.daemon.stop_confirm_description, {
                count: affectedCount,
              })}
            </DialogDescription>
          </DialogHeader>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.desktop.daemon.cancel)}
          </Button>
          <Button variant="destructive" onClick={onConfirm}>
            {t(($) => $.desktop.daemon.stop_daemon)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
