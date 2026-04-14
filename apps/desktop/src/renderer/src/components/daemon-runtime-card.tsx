import { useState, useEffect, useCallback } from "react";
import {
  Play,
  Square,
  RotateCw,
  Server,
  ScrollText,
  Activity,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { DaemonPanel } from "./daemon-panel";

type DaemonState = "running" | "stopped" | "starting" | "stopping" | "cli_not_found";

interface DaemonStatusInfo {
  state: DaemonState;
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
}

function formatUptime(uptime?: string): string {
  if (!uptime) return "—";
  const match = uptime.match(/(?:(\d+)h)?(\d+)m/);
  if (!match) return uptime;
  const h = match[1] ? `${match[1]}h ` : "";
  const m = match[2] ? `${match[2]}m` : "";
  return `${h}${m}`.trim() || uptime;
}

export function DaemonRuntimeCard() {
  const [status, setStatus] = useState<DaemonStatusInfo>({ state: "stopped" });
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
    if (!result.success) setActionLoading(false);
  }, []);

  const handleStop = useCallback(async () => {
    setActionLoading(true);
    await window.daemonAPI.stop();
  }, []);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    await window.daemonAPI.restart();
  }, []);

  const stateColor: Record<DaemonState, string> = {
    running: "bg-emerald-500",
    stopped: "bg-muted-foreground/40",
    starting: "bg-amber-500 animate-pulse",
    stopping: "bg-amber-500 animate-pulse",
    cli_not_found: "bg-muted-foreground/20",
  };

  const stateLabel: Record<DaemonState, string> = {
    running: "Running",
    stopped: "Stopped",
    starting: "Starting…",
    stopping: "Stopping…",
    cli_not_found: "CLI Not Found",
  };

  const isTransitioning = status.state === "starting" || status.state === "stopping";
  const isRunning = status.state === "running";
  const isStopped = status.state === "stopped" || status.state === "cli_not_found";

  return (
    <>
      <div className="border-b px-4 py-3">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-muted">
              <Server className="size-4 text-muted-foreground" />
            </div>
            <div>
              <h3 className="text-sm font-medium">Local Daemon</h3>
              <div className="flex items-center gap-1.5 mt-0.5">
                <span className={cn("size-1.5 rounded-full", stateColor[status.state])} />
                <span className="text-xs text-muted-foreground">{stateLabel[status.state]}</span>
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

          <div className="flex items-center gap-1.5 shrink-0">
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
                  onClick={() => setPanelOpen(true)}
                >
                  <ScrollText className="size-3.5 mr-1.5" />
                  Logs
                </Button>
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
                {stateLabel[status.state]}
              </Button>
            )}
          </div>
        </div>
      </div>

      <DaemonPanel open={panelOpen} onOpenChange={setPanelOpen} status={status} />
    </>
  );
}
