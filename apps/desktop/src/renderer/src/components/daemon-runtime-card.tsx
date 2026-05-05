import { useState, useEffect, useCallback, useMemo } from "react";
import {
  AlertCircle,
  Play,
  Square,
  RotateCw,
  Server,
  Activity,
  ScrollText,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions } from "@multica/core/runtimes";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardAction,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import { useT } from "@multica/i18n/react";
import { DaemonPanel } from "./daemon-panel";
import type { DaemonStatus } from "../../../shared/daemon-types";
import {
  DAEMON_STATE_COLORS,
  type DaemonActionResult,
  getDaemonStateKeys,
  daemonStateDescKey,
  formatUptime,
} from "../../../shared/daemon-types";

function daemonErrorDescription(
  result: DaemonActionResult,
  t: (key: string) => string,
): string | undefined {
  if (!result.errorCode) return result.error;
  return t(`error_${result.errorCode}`);
}

/**
 * Header card on the desktop Runtimes page that surfaces the daemon embedded
 * in this Electron app. The same daemon process registers N runtimes with the
 * server (one per detected CLI), which appear in the runtime list below — so
 * this card is the parent control surface for "what's running on this Mac".
 *
 * Why this lives only on desktop: web users don't have an embedded daemon;
 * they bring their own (CLI-launched or remote VM) and just see runtimes in
 * the list. The `desktop-runtimes-page` wrapper is the only mount point.
 */
export function DaemonRuntimeCard() {
  const t = useT("desktop");
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [panelOpen, setPanelOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);

  const wsId = useWorkspaceId();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // Snapshot also includes each agent's latest terminal; the filter below
  // drops anything that isn't running/dispatched, so terminal rows pass
  // through harmlessly.
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  // Set of runtime IDs registered by THIS daemon (one per detected CLI).
  // Used both to count "how many CLIs am I contributing" and to figure
  // out which active tasks would be impacted by a Stop.
  const localRuntimeIds = useMemo(() => {
    if (!status.daemonId) return new Set<string>();
    return new Set(
      runtimes
        .filter((r) => r.daemon_id === status.daemonId)
        .map((r) => r.id),
    );
  }, [runtimes, status.daemonId]);

  const runtimeCount = localRuntimeIds.size;

  // Tasks that are actually doing work on this daemon right now —
  // running or dispatched. Queued tasks haven't claimed a runtime yet,
  // so stopping the daemon won't break them (they'll wait for any
  // available daemon). The number drives the Stop-confirmation dialog.
  const affectedTasks = useMemo(
    () =>
      snapshot.filter(
        (task) =>
          localRuntimeIds.has(task.runtime_id) &&
          (task.status === "running" || task.status === "dispatched"),
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
      toast.error(t("toast_failed_start"), {
        description: daemonErrorDescription(result, t),
      });
    }
  }, [t]);

  // The actual stop call, separated from the click handler so we can call
  // it both from the direct path (no active tasks) and from the confirm
  // dialog's confirm button.
  const performStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    if (!result.success) {
      toast.error(t("toast_failed_stop"), {
        description: daemonErrorDescription(result, t),
      });
    }
  }, [t]);

  // Click on the Stop button. If there's nothing running, just stop;
  // otherwise pop a confirm dialog explaining the blast radius.
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
      toast.error(t("toast_failed_restart"), {
        description: daemonErrorDescription(result, t),
      });
      return;
    }
    // Success feedback — the daemon takes a few seconds to come back online,
    // and the only other UI signal is the state badge flipping briefly. A
    // toast confirms the click was received and tells the user what to expect.
    toast.success(t("toast_restarting"), {
      description: t("toast_restarting_desc"),
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

  const isRunning = status.state === "running";
  const isStopped = status.state === "stopped";
  const isCliMissing = status.state === "cli_not_found";
  const isTransitioning =
    status.state === "starting" || status.state === "stopping";
  const isInstalling = status.state === "installing_cli";

  return (
    <>
      <Card size="sm">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Server className="size-4 text-muted-foreground" />
            {t("local_daemon")}
            <span className="inline-flex items-center gap-1.5 rounded-md border bg-background px-1.5 py-0.5 text-xs font-normal">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  DAEMON_STATE_COLORS[status.state],
                )}
              />
              <span
                className={cn(
                  "tabular-nums",
                  isRunning ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {t(getDaemonStateKeys(status.state))}
              </span>
              {isRunning && status.uptime && (
                <span className="text-muted-foreground">
                  · {formatUptime(status.uptime)}
                </span>
              )}
            </span>
          </CardTitle>
          <CardDescription>
            {(() => { const { key, params } = daemonStateDescKey(status.state, runtimeCount); return t(key, params); })()}
          </CardDescription>
          <CardAction className="self-center">
            <div className="flex items-center gap-1.5">
              {isRunning && (
                <>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setPanelOpen(true)}
                  >
                    <ScrollText className="size-3.5 mr-1.5" />
                    {t("view_logs")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleRestart}
                    disabled={actionLoading}
                  >
                    <RotateCw className="size-3.5 mr-1.5" />
                    {t("restart")}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={handleStopClick}
                    disabled={actionLoading}
                  >
                    <Square className="size-3.5 mr-1.5" />
                    {t("stop")}
                  </Button>
                </>
              )}

              {isStopped && (
                <Button
                  size="sm"
                  onClick={handleStart}
                  disabled={actionLoading}
                >
                  {actionLoading ? (
                    <Activity className="size-3.5 mr-1.5 animate-pulse" />
                  ) : (
                    <Play className="size-3.5 mr-1.5" />
                  )}
                  {t("start")}
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
                  {t("retry_setup")}
                </Button>
              )}

              {(isTransitioning || isInstalling) && (
                <Button size="sm" variant="outline" disabled>
                  <Activity className="size-3.5 mr-1.5 animate-pulse" />
                  {t(getDaemonStateKeys(status.state))}
                </Button>
              )}
            </div>
          </CardAction>
        </CardHeader>
      </Card>

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
  const t = useT("desktop");
  const tc = useT("common");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-sm font-semibold">
              {t("stop_confirm_title", { count: affectedCount })}
            </DialogTitle>
            <DialogDescription className="text-xs leading-relaxed">
              {t("stop_confirm_desc", { count: affectedCount })}
            </DialogDescription>
          </DialogHeader>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {tc("cancel")}
          </Button>
          <Button variant="destructive" onClick={onConfirm}>
            {t("stop_daemon")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
