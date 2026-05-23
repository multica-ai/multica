import { useState, useEffect, useCallback, useMemo } from "react";
import {
  AlertCircle,
  Play,
  Square,
  RotateCw,
  Activity,
  ScrollText,
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
import { DaemonPanel } from "./daemon-panel";
import type { DaemonStatus } from "../../../shared/daemon-types";
import { DAEMON_STATE_LABELS } from "../../../shared/daemon-types";

/**
 * Desktop-only controls for the daemon embedded in this Electron app. The
 * shared runtimes page renders this inside the selected local machine header.
 */
export function DaemonRuntimeActions() {
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
      toast.error("Failed to start daemon", { description: result.error });
    }
  }, []);

  const performStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    if (!result.success) {
      toast.error("Failed to stop daemon", { description: result.error });
    }
  }, []);

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
      toast.error("Failed to restart daemon", { description: result.error });
      return;
    }
    toast.success("Restarting daemon", {
      description: "Runtimes will be back online in a few seconds.",
    });
  }, []);

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
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {isRunning && (
          <>
            <Button size="sm" variant="ghost" onClick={() => setPanelOpen(true)}>
              <ScrollText className="size-3.5 mr-1.5" />
              View logs
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleRestart}
              disabled={actionLoading}
            >
              <RotateCw className="size-3.5 mr-1.5" />
              Restart
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={handleStopClick}
              disabled={actionLoading}
            >
              <Square className="size-3.5 mr-1.5" />
              Stop
            </Button>
          </>
        )}

        {isStopped && (
          <Button size="sm" onClick={handleStart} disabled={actionLoading}>
            {actionLoading ? (
              <Activity className="size-3.5 mr-1.5 animate-pulse" />
            ) : (
              <Play className="size-3.5 mr-1.5" />
            )}
            Start
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
            Retry setup
          </Button>
        )}

        {(isTransitioning || isInstalling) && (
          <Button size="sm" variant="outline" disabled>
            <Activity className="size-3.5 mr-1.5 animate-pulse" />
            {DAEMON_STATE_LABELS[status.state]}
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
  const plural = affectedCount === 1 ? "" : "s";
  const verb = affectedCount === 1 ? "is" : "are";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-start gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-sm font-semibold">
              Stop daemon with {affectedCount} active task{plural}?
            </DialogTitle>
            <DialogDescription className="text-xs leading-relaxed">
              {affectedCount} task{plural} {verb} currently running on this
              device. Stopping now will interrupt {affectedCount === 1 ? "it" : "them"}{" "}
              — affected tasks get marked <strong>failed</strong> once the
              timeout hits. The daemon won&apos;t auto-restart.
            </DialogDescription>
          </DialogHeader>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onConfirm}>
            Stop daemon
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
