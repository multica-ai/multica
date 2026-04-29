"use client";

import { useState, useEffect } from "react";
import { useTimerStore } from "@multica/core/time-entries/timer-store";
import { useNavigation } from "../navigation";
import { useCurrentWorkspace } from "@multica/core/paths";

function formatCompact(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}` : `${m}:${pad(s)}`;
}

export function SidebarTimerIndicator() {
  const timer = useTimerStore((s) => s.activeTimer);
  const [elapsed, setElapsed] = useState(0);
  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();

  useEffect(() => {
    if (!timer) {
      setElapsed(0);
      return;
    }
    const update = () => setElapsed(Date.now() - timer.startedAt);
    update();
    const interval = setInterval(update, 1000);
    return () => clearInterval(interval);
  }, [timer]);

  if (!timer) return null;

  const handleClick = () => {
    if (workspace) {
      navigation.push(`/${workspace.slug}/issues/${timer.issueId}`);
    }
  };

  return (
    <button
      onClick={handleClick}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent/70"
    >
      {/* Pulsing dot */}
      <span className="relative flex size-2 shrink-0">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-red-400 opacity-75" />
        <span className="relative inline-flex size-2 rounded-full bg-red-500" />
      </span>
      <span className="font-mono tabular-nums text-muted-foreground">
        {formatCompact(elapsed)}
      </span>
      <span className="truncate text-foreground font-medium">
        {timer.issueIdentifier}
      </span>
    </button>
  );
}
