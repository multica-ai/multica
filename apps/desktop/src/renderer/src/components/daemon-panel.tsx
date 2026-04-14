import { useState, useEffect, useRef, useCallback } from "react";
import {
  Play,
  Square,
  RotateCw,
  Server,
  ChevronDown,
  X,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import type { DaemonStatus, DaemonState } from "../../../shared/daemon-types";
import { DAEMON_STATE_COLORS, DAEMON_STATE_LABELS } from "../../../shared/daemon-types";

interface DaemonPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: DaemonStatus;
}

const LOG_LEVEL_COLORS: Record<string, string> = {
  INFO: "text-info",
  WARN: "text-warning",
  ERROR: "text-destructive",
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
  return <span className={cn("inline-block size-2 rounded-full", DAEMON_STATE_COLORS[state])} />;
}

interface LogEntry {
  id: number;
  line: string;
}

const MAX_LOG_LINES = 500;
let logIdCounter = 0;

export function DaemonPanel({ open, onOpenChange, status }: DaemonPanelProps) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  // Watched workspaces — populated from the daemon when the panel opens and
  // refreshed after every toggle so the checkbox state reflects reality.
  const { data: allWorkspaces } = useQuery({
    ...workspaceListOptions(),
    enabled: open,
  });
  const [watchedIds, setWatchedIds] = useState<Set<string>>(new Set());
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const refreshWatched = useCallback(async () => {
    const state = await window.daemonAPI.listWatched().catch(() => null);
    if (state) setWatchedIds(new Set(state.watched.map((w) => w.id)));
  }, []);

  useEffect(() => {
    if (open && status.state === "running") void refreshWatched();
  }, [open, status.state, refreshWatched]);

  const handleToggleWatch = useCallback(
    async (id: string, name: string, nextChecked: boolean) => {
      setTogglingId(id);
      try {
        if (nextChecked) {
          await window.daemonAPI.watchWorkspace(id, name);
        } else {
          await window.daemonAPI.unwatchWorkspace(id);
        }
        await refreshWatched();
      } catch (err) {
        toast.error(
          nextChecked ? "Failed to watch workspace" : "Failed to unwatch workspace",
          { description: err instanceof Error ? err.message : String(err) },
        );
      } finally {
        setTogglingId(null);
      }
    },
    [refreshWatched],
  );

  useEffect(() => {
    if (!open) return;

    window.daemonAPI.startLogStream();
    const unsub = window.daemonAPI.onLogLine((line) => {
      setLogs((prev) => {
        const next = [...prev, { id: ++logIdCounter, line }];
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
    const result = await window.daemonAPI.start();
    setActionLoading(false);
    if (!result.success) {
      toast.error("Failed to start daemon", { description: result.error });
    }
  }, []);

  const handleStop = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.stop();
    setActionLoading(false);
    if (!result.success) {
      toast.error("Failed to stop daemon", { description: result.error });
    }
  }, []);

  const handleRestart = useCallback(async () => {
    setActionLoading(true);
    const result = await window.daemonAPI.restart();
    setActionLoading(false);
    if (!result.success) {
      toast.error("Failed to restart daemon", { description: result.error });
    }
  }, []);

  const isTransitioning = status.state === "starting" || status.state === "stopping";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex flex-col sm:max-w-md"
        showCloseButton={false}
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        <SheetHeader className="flex-row items-center justify-between gap-2 pr-3">
          <SheetTitle className="flex items-center gap-2">
            <Server className="size-4" />
            Local Daemon
          </SheetTitle>
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            aria-label="Close"
            className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <X className="size-4" />
          </button>
        </SheetHeader>

        <div className="flex-1 min-h-0 flex flex-col gap-4 px-4">
          <div className="shrink-0 space-y-4">
          {/* Status info */}
          <div className="rounded-lg border p-3 space-y-0.5">
            <InfoRow
              label="Status"
              value={
                <span className="flex items-center gap-1.5">
                  <StatusDot state={status.state} />
                  {DAEMON_STATE_LABELS[status.state]}
                </span>
              }
            />
            {status.uptime && <InfoRow label="Uptime" value={status.uptime} />}
            <InfoRow label="Profile" value={status.profile || "default"} />
            {status.serverUrl && (
              <InfoRow
                label="Server"
                value={
                  <span className="font-mono text-xs" title={status.serverUrl}>
                    {status.serverUrl}
                  </span>
                }
              />
            )}
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
          {status.state === "installing_cli" ? (
            <div className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
              Setting up the local runtime… this only happens the first time.
            </div>
          ) : status.state === "cli_not_found" ? (
            <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-3 space-y-2">
              <p className="text-sm">
                Couldn&apos;t download the local runtime. Check your network
                connection and try again.
              </p>
              <Button
                size="sm"
                variant="outline"
                onClick={async () => {
                  setActionLoading(true);
                  try {
                    await window.daemonAPI.retryInstall();
                  } finally {
                    setActionLoading(false);
                  }
                }}
                disabled={actionLoading}
              >
                <RotateCw className="size-3.5 mr-1.5" />
                Retry
              </Button>
            </div>
          ) : (
            <div className="flex gap-2">
              {status.state === "stopped" ? (
                <Button size="sm" onClick={handleStart} disabled={actionLoading}>
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
          )}

          {/* Watched workspaces */}
          {status.state === "running" && allWorkspaces && allWorkspaces.length > 0 && (
            <div className="space-y-2">
              <h3 className="text-sm font-medium">Watched Workspaces</h3>
              <div className="rounded-lg border divide-y max-h-48 overflow-y-auto">
                {allWorkspaces.map((ws) => {
                  const checked = watchedIds.has(ws.id);
                  const disabled = togglingId === ws.id;
                  return (
                    <label
                      key={ws.id}
                      className={cn(
                        "flex items-center gap-2.5 px-3 py-2",
                        disabled
                          ? "opacity-60 cursor-wait"
                          : "cursor-pointer hover:bg-muted/40",
                      )}
                    >
                      <Checkbox
                        checked={checked}
                        disabled={disabled}
                        onCheckedChange={(next) =>
                          handleToggleWatch(ws.id, ws.name, next === true)
                        }
                      />
                      <span className="truncate text-sm">{ws.name}</span>
                    </label>
                  );
                })}
              </div>
            </div>
          )}
          </div>

          {/* Logs — fills remaining vertical space down to the sheet bottom */}
          <div className="flex-1 min-h-0 flex flex-col gap-2 pb-4">
            <div className="flex items-center justify-between shrink-0">
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
              className="flex-1 min-h-0 overflow-y-auto rounded-lg border bg-muted/30 p-2 font-mono text-xs leading-relaxed"
            >
              {logs.length === 0 ? (
                <p className="text-muted-foreground/50 text-center py-8">
                  {status.state === "running"
                    ? "Waiting for logs…"
                    : "Start the daemon to see logs"}
                </p>
              ) : (
                logs.map((entry) => {
                  const { className } = colorizeLogLine(entry.line);
                  return (
                    <div key={entry.id} className={cn("whitespace-pre-wrap break-all", className)}>
                      {entry.line}
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
