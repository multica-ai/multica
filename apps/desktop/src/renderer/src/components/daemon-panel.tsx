import { useState, useEffect, useRef, useCallback } from "react";
import {
  Play,
  Square,
  RotateCw,
  Server,
  ChevronDown,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";

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

interface DaemonPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: DaemonStatusInfo;
}

const LOG_LEVEL_COLORS: Record<string, string> = {
  INFO: "text-blue-400",
  WARN: "text-amber-400",
  ERROR: "text-red-400",
  DEBUG: "text-muted-foreground",
};

function colorizeLogLine(line: string): { level: string; className: string } {
  for (const [level, className] of Object.entries(LOG_LEVEL_COLORS)) {
    if (line.includes(level)) return { level, className };
  }
  return { level: "", className: "text-muted-foreground" };
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-1">
      <span className="shrink-0 text-xs text-muted-foreground">{label}</span>
      <span className="truncate text-right text-sm">{value}</span>
    </div>
  );
}

function StatusDot({ state }: { state: DaemonState }) {
  const colors: Record<DaemonState, string> = {
    running: "bg-emerald-500",
    stopped: "bg-muted-foreground/40",
    starting: "bg-amber-500 animate-pulse",
    stopping: "bg-amber-500 animate-pulse",
    cli_not_found: "bg-muted-foreground/20",
  };
  return <span className={cn("inline-block size-2 rounded-full", colors[state])} />;
}

const MAX_LOG_LINES = 500;

export function DaemonPanel({ open, onOpenChange, status }: DaemonPanelProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    window.daemonAPI.startLogStream();
    const unsub = window.daemonAPI.onLogLine((line) => {
      setLogs((prev) => {
        const next = [...prev, line];
        return next.length > MAX_LOG_LINES ? next.slice(-MAX_LOG_LINES) : next;
      });
    });

    return () => {
      unsub();
      window.daemonAPI.stopLogStream();
    };
  }, [open]);

  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(atBottom);
  }, []);

  const scrollToBottom = useCallback(() => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
      setAutoScroll(true);
    }
  }, []);

  const handleStart = useCallback(async () => {
    setActionLoading(true);
    await window.daemonAPI.start();
    setActionLoading(false);
  }, []);

  const handleStop = useCallback(async () => {
    setActionLoading(true);
    await window.daemonAPI.stop();
    setActionLoading(false);
  }, []);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    await window.daemonAPI.restart();
    setActionLoading(false);
  }, []);

  const stateLabel: Record<DaemonState, string> = {
    running: "Running",
    stopped: "Stopped",
    starting: "Starting…",
    stopping: "Stopping…",
    cli_not_found: "CLI Not Found",
  };

  const isTransitioning = status.state === "starting" || status.state === "stopping";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col sm:max-w-md">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <Server className="size-4" />
            Local Daemon
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-4 space-y-4">
          {/* Status info */}
          <div className="rounded-lg border p-3 space-y-0.5">
            <InfoRow
              label="Status"
              value={
                <span className="flex items-center gap-1.5">
                  <StatusDot state={status.state} />
                  {stateLabel[status.state]}
                </span>
              }
            />
            {status.uptime && <InfoRow label="Uptime" value={status.uptime} />}
            {status.agents && status.agents.length > 0 && (
              <InfoRow label="Agents" value={status.agents.join(", ")} />
            )}
            {status.deviceName && <InfoRow label="Device" value={status.deviceName} />}
            {status.daemonId && (
              <InfoRow
                label="Daemon ID"
                value={<span className="font-mono text-xs">{status.daemonId}</span>}
              />
            )}
            {typeof status.workspaceCount === "number" && (
              <InfoRow label="Workspaces" value={status.workspaceCount} />
            )}
            {status.pid && (
              <InfoRow
                label="PID"
                value={<span className="font-mono text-xs">{status.pid}</span>}
              />
            )}
          </div>

          {/* Actions */}
          <div className="flex gap-2">
            {status.state === "stopped" || status.state === "cli_not_found" ? (
              <Button
                size="sm"
                onClick={handleStart}
                disabled={actionLoading || status.state === "cli_not_found"}
              >
                <Play className="size-3.5 mr-1.5" />
                Start
              </Button>
            ) : (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleStop}
                  disabled={actionLoading || isTransitioning}
                >
                  <Square className="size-3.5 mr-1.5" />
                  Stop
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleRestart}
                  disabled={actionLoading || isTransitioning}
                >
                  <RotateCw className="size-3.5 mr-1.5" />
                  Restart
                </Button>
              </>
            )}
          </div>

          {/* Logs */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium">Logs</h3>
              {!autoScroll && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={scrollToBottom}
                >
                  <ChevronDown className="size-3 mr-1" />
                  Scroll to bottom
                </Button>
              )}
            </div>
            <div
              ref={logContainerRef}
              onScroll={handleLogScroll}
              className="h-64 overflow-y-auto rounded-lg border bg-muted/30 p-2 font-mono text-xs leading-relaxed"
            >
              {logs.length === 0 ? (
                <p className="text-muted-foreground/50 text-center py-8">
                  {status.state === "running"
                    ? "Waiting for logs…"
                    : "Start the daemon to see logs"}
                </p>
              ) : (
                logs.map((line, i) => {
                  const { className } = colorizeLogLine(line);
                  return (
                    <div key={i} className={cn("whitespace-pre-wrap break-all", className)}>
                      {line}
                    </div>
                  );
                })
              )}
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
