import { useState, useEffect, useCallback } from "react";
import { Activity, Play, Server } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { DaemonPanel } from "./daemon-panel";
import type { DaemonStatus } from "../../../shared/daemon-types";
import { DAEMON_STATE_COLORS, DAEMON_STATE_LABELS, formatUptime } from "../../../shared/daemon-types";

export function DaemonStatusBar() {
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [panelOpen, setPanelOpen] = useState(false);
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    window.daemonAPI.getStatus().then((s) => setStatus(s));
    const unsub = window.daemonAPI.onStatusChange((s) => {
      setStatus(s);
      if (s.state !== "starting") setStarting(false);
    });
    return unsub;
  }, []);

  const handleStart = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      setStarting(true);
      const result = await window.daemonAPI.start();
      if (!result.success) {
        setStarting(false);
        toast.error("Failed to start daemon", { description: result.error });
      }
    },
    [],
  );

  const agentCount = status.agents?.length ?? 0;
  const uptime = formatUptime(status.uptime);
  const subtitle =
    status.state === "running" && (agentCount > 0 || uptime)
      ? [agentCount > 0 ? `${agentCount} agent${agentCount > 1 ? "s" : ""}` : null, uptime || null]
          .filter(Boolean)
          .join(" · ")
      : null;

  return (
    <>
      <div className="mb-1">
        <button
          type="button"
          onClick={() => setPanelOpen(true)}
          className={cn(
            "flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left",
            "text-muted-foreground hover:bg-sidebar-accent/70 transition-colors",
          )}
        >
          <Server className="size-4 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className={cn("size-1.5 shrink-0 rounded-full", DAEMON_STATE_COLORS[status.state])} />
              <span className="truncate text-sm font-medium leading-tight text-sidebar-foreground">
                {DAEMON_STATE_LABELS[status.state]}
              </span>
            </div>
            {subtitle && (
              <p className="truncate text-xs text-muted-foreground leading-tight mt-0.5 ml-3">
                {subtitle}
              </p>
            )}
          </div>
          {status.state === "stopped" && (
            <Button
              variant="ghost"
              size="icon-sm"
              className="shrink-0"
              onClick={handleStart}
              disabled={starting}
            >
              {starting ? (
                <Activity className="size-3.5 animate-pulse" />
              ) : (
                <Play className="size-3.5" />
              )}
            </Button>
          )}
        </button>
      </div>

      <DaemonPanel open={panelOpen} onOpenChange={setPanelOpen} status={status} />
    </>
  );
}
