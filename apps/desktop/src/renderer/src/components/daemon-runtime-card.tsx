import { useState, useEffect, useCallback } from "react";
import {
  Play,
  Square,
  RotateCw,
  Server,
  Activity,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { DaemonPanel } from "./daemon-panel";
import type { DaemonStatus } from "../../../shared/daemon-types";
import { DAEMON_STATE_COLORS, DAEMON_STATE_LABELS, formatUptime } from "../../../shared/daemon-types";

export function DaemonRuntimeCard() {
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [panelOpen, setPanelOpen] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);

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

  const handleStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    if (!result.success) {
      toast.error("Failed to stop daemon", { description: result.error });
    }
  }, []);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.restart();
    if (!result.success) {
      toast.error("Failed to restart daemon", { description: result.error });
    }
  }, []);

  const isTransitioning = status.state === "starting" || status.state === "stopping";
  const isRunning = status.state === "running";
  const isStopped = status.state === "stopped" || status.state === "cli_not_found";

  const stopPropagation = (e: React.MouseEvent) => e.stopPropagation();

  return (
    <>
      <div
        role="button"
        tabIndex={0}
        onClick={() => setPanelOpen(true)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setPanelOpen(true);
          }
        }}
        className="border-b px-4 py-3 cursor-pointer transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:bg-muted/40"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-muted">
              <Server className="size-4 text-muted-foreground" />
            </div>
            <div>
              <h3 className="text-sm font-medium">Local Daemon</h3>
              <div className="flex items-center gap-1.5 mt-0.5">
                <span className={cn("size-1.5 rounded-full", DAEMON_STATE_COLORS[status.state])} />
                <span className="text-xs text-muted-foreground">{DAEMON_STATE_LABELS[status.state]}</span>
                {isRunning && status.uptime && (
                  <>
                    <span className="text-xs text-muted-foreground">·</span>
                    <span className="text-xs text-muted-foreground">{formatUptime(status.uptime)}</span>
                  </>
                )}
                {isRunning && status.agents && status.agents.length > 0 && (
                  <>
                    <span className="text-xs text-muted-foreground">·</span>
                    <span className="text-xs text-muted-foreground">{status.agents.join(", ")}</span>
                  </>
                )}
              </div>
            </div>
          </div>

          <div
            className="flex items-center gap-1.5 shrink-0"
            onClick={stopPropagation}
          >
            {isStopped && (
              <Button
                size="sm"
                variant="outline"
                onClick={handleStart}
                disabled={actionLoading || status.state === "cli_not_found"}
              >
                {actionLoading ? (
                  <Activity className="size-3.5 mr-1.5 animate-pulse" />
                ) : (
                  <Play className="size-3.5 mr-1.5" />
                )}
                Start
              </Button>
            )}
            {isRunning && (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={handleRestart}
                  disabled={actionLoading}
                >
                  <RotateCw className="size-3.5 mr-1.5" />
                  Restart
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleStop}
                  disabled={actionLoading}
                >
                  <Square className="size-3.5 mr-1.5" />
                  Stop
                </Button>
              </>
            )}
            {isTransitioning && (
              <Button size="sm" variant="outline" disabled>
                <Activity className="size-3.5 mr-1.5 animate-pulse" />
                {DAEMON_STATE_LABELS[status.state]}
              </Button>
            )}
          </div>
        </div>
      </div>

      <DaemonPanel open={panelOpen} onOpenChange={setPanelOpen} status={status} />
    </>
  );
}
