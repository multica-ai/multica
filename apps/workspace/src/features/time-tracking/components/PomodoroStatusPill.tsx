"use client";

import { useEffect, useState } from "react";
import { Focus } from "lucide-react";
import { Link } from "@/shared/router";
import { cn } from "@/lib/utils";
import { useFocusQuery } from "../hooks/use-focus";

/**
 * Renders a compact shell-level status link for the active Focus session.
 */
export function PomodoroStatusPill() {
  const { data: session } = useFocusQuery();
  const [, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    if (!session || !["focusing", "paused", "breaking"].includes(session.phase)) {
      return undefined;
    }

    const intervalId = window.setInterval(() => {
      setNowMs(() => Date.now());
    }, 1000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [session]);

  if (!session || !["focusing", "paused", "breaking"].includes(session.phase)) {
    return null;
  }

  const elapsed = session.phase === "focusing" && session.started_at
    ? session.elapsed_focus_seconds + Math.max(0, Math.floor((Date.now() - new Date(session.started_at).getTime()) / 1000))
    : session.elapsed_focus_seconds;
  const breakRemaining = session.phase === "breaking" && session.started_at && session.suggested_break_seconds
    ? Math.max(0, session.suggested_break_seconds - Math.floor((Date.now() - new Date(session.started_at).getTime()) / 1000))
    : session.suggested_break_seconds ?? 0;
  const shownSeconds = session.phase === "breaking" ? breakRemaining : elapsed;
  const minutes = Math.floor(shownSeconds / 60);
  const seconds = shownSeconds % 60;
  const display = `${minutes}:${String(seconds).padStart(2, "0")}`;
  const label = session.phase === "breaking" ? "Break" : session.phase === "paused" ? "Paused" : "Focus";

  return (
    <Link
      href="/focus"
      aria-label={`${label} ${display}`}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-border/70",
        "bg-muted/60 px-2.5 py-1 text-xs font-medium text-muted-foreground",
        "transition-colors hover:text-foreground",
      )}
    >
      <Focus className="size-3.5 shrink-0" aria-hidden="true" />
      <span>{label}</span>
      <span className="font-mono text-foreground tabular-nums">{display}</span>
    </Link>
  );
}
