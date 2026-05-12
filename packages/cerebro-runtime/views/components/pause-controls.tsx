// Cerebro pause/resume UI for the runtime detail topbar and list row menu.
// Lives in cerebro-runtime so the upstream runtime views import a single
// named component instead of growing pause-aware code paths.

"use client";

// Pulls in cerebro-types so the augment.ts module-augmentation widens
// RuntimeDevice with paused_at / unpause_at / pause_reason for this file.
import "@multica/cerebro-types";

import { useEffect, useState } from "react";
import { Pause, Play } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { AgentRuntime } from "@multica/core/types/agent";
import { toast } from "sonner";
import { usePauseRuntime, useUnpauseRuntime } from "../use-pause-mutations";

interface PauseControlsProps {
  runtime: AgentRuntime;
  workspaceId: string;
  /** Compact = icon-only button; default = labeled button with text. */
  compact?: boolean;
}

/**
 * Pause / Resume button. Shows Pause when the runtime is unpaused and
 * Resume when paused. Disabled while the mutation is in flight.
 */
export function PauseRuntimeButton({ runtime, workspaceId, compact }: PauseControlsProps) {
  const pauseMut = usePauseRuntime(workspaceId);
  const unpauseMut = useUnpauseRuntime(workspaceId);
  const isPaused = !!runtime.paused_at;
  const inFlight = pauseMut.isPending || unpauseMut.isPending;

  const onClick = async () => {
    try {
      if (isPaused) {
        await unpauseMut.mutateAsync({ runtimeId: runtime.id });
        toast.success("Runtime resumed");
      } else {
        await pauseMut.mutateAsync({ runtimeId: runtime.id });
        toast.success("Runtime paused");
      }
    } catch (err) {
      toast.error(`${isPaused ? "Resume" : "Pause"} failed: ${(err as Error).message}`);
    }
  };

  const Icon = isPaused ? Play : Pause;
  const label = isPaused ? "Resume" : "Pause";

  if (compact) {
    return (
      <Button
        variant="ghost"
        size="icon"
        disabled={inFlight}
        onClick={onClick}
        aria-label={label}
        title={label}
      >
        <Icon className="h-4 w-4" />
      </Button>
    );
  }

  return (
    <Button variant="outline" size="sm" disabled={inFlight} onClick={onClick}>
      <Icon className="h-3.5 w-3.5 mr-1.5" />
      {label}
    </Button>
  );
}

interface PauseBannerProps {
  runtime: AgentRuntime;
}

/**
 * Pause-state banner with a live "Auto-resume in 2h 14m" countdown. Renders
 * nothing when the runtime is not paused. The countdown ticks every 30s
 * (no need for sub-minute precision — the sweeper itself runs at 30s).
 */
export function PauseBanner({ runtime }: PauseBannerProps) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!runtime.paused_at) return;
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, [runtime.paused_at]);

  if (!runtime.paused_at) return null;

  const reason = runtime.pause_reason || "manual";
  const remaining = runtime.unpause_at ? formatRemaining(runtime.unpause_at, now) : null;

  return (
    <div className="flex items-center gap-2 rounded-md border border-brand/30 bg-brand/5 px-3 py-2 text-sm text-brand">
      <Pause className="h-4 w-4 shrink-0" />
      <div className="flex-1">
        <span className="font-medium">Paused</span>
        <span className="text-brand/70"> — {reason}</span>
        {remaining ? (
          <span className="text-brand/70"> · auto-resume in {remaining}</span>
        ) : (
          <span className="text-brand/70"> · resumes when manually unpaused</span>
        )}
      </div>
    </div>
  );
}

/**
 * Renders a short "2h 14m" / "30s" / "now" string for the gap between
 * `until` (an ISO timestamp) and `now` (a Date.now() millis value). Used
 * by the pause banner countdown.
 */
function formatRemaining(until: string, nowMs: number): string {
  const ms = new Date(until).getTime() - nowMs;
  if (ms <= 0) return "now";
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.round(totalSeconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) return remMinutes ? `${hours}h ${remMinutes}m` : `${hours}h`;
  const days = Math.floor(hours / 24);
  const remHours = hours % 24;
  return remHours ? `${days}d ${remHours}h` : `${days}d`;
}
