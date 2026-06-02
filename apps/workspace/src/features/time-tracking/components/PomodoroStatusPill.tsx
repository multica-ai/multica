"use client";

import { useEffect, useState } from "react";
import { Timer } from "lucide-react";
import { Link } from "@/shared/router";
import { cn } from "@/lib/utils";
import { usePomodoroQuery } from "../hooks/use-pomodoro";
import {
  formatPomodoroTimer,
  getPomodoroHeaderLabel,
  getPomodoroRemainingSeconds,
} from "../lib/pomodoro-display";

/**
 * Renders a compact shell-level status link for the active pomodoro session.
 */
export function PomodoroStatusPill() {
  const { data: session } = usePomodoroQuery();
  const [, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    if (!session || session.status !== "running") {
      return undefined;
    }

    const intervalId = window.setInterval(() => {
      setNowMs(() => Date.now());
    }, 1000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [session]);

  if (!session || session.status !== "running") {
    return null;
  }

  const label = getPomodoroHeaderLabel(session.phase);
  const remaining = formatPomodoroTimer(getPomodoroRemainingSeconds(session));

  return (
    <Link
      href="/pomodoro"
      aria-label={`${label} ${remaining}`}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-border/70",
        "bg-muted/60 px-2.5 py-1 text-xs font-medium text-muted-foreground",
        "transition-colors hover:text-foreground",
      )}
    >
      <Timer className="size-3.5 shrink-0" aria-hidden="true" />
      <span>{label}</span>
      <span className="font-mono text-foreground tabular-nums">{remaining}</span>
    </Link>
  );
}
