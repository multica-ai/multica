"use client";

import { useState, useEffect, useRef } from "react";
import { Clock, Square, Play, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { useCurrentTimerQuery, useStartTimerMutation, useStopTimerMutation } from "../hooks/use-time-tracking";
import { LiveDuration } from "./LiveDuration";

/**
 * Compact timer widget shown in the sidebar footer.
 *
 * States:
 * - Idle/collapsed: shows "Track time" button
 * - Idle/expanded: shows inline input + Start button (no Popover anchor issues)
 * - Running: shows live duration counter + Stop button
 */
export function GlobalTimerWidget() {
  const [expanded, setExpanded] = useState(false);
  const [description, setDescription] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const { data: currentEntry } = useCurrentTimerQuery();
  const startMutation = useStartTimerMutation();
  const stopMutation = useStopTimerMutation();

  const isRunning = !!currentEntry;

  // Auto-focus the input when the form expands.
  useEffect(() => {
    if (expanded) {
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [expanded]);

  // Update document.title while a timer is running, e.g. "1:23:45 · Multica".
  // Resets to "Multica" when the timer stops or the component unmounts.
  useEffect(() => {
    if (!currentEntry) {
      document.title = "Multica";
      return;
    }

    const tick = () => {
      const elapsedSeconds = Math.floor(Date.now() / 1000) + currentEntry.duration_seconds;
      const h = Math.floor(elapsedSeconds / 3600);
      const m = Math.floor((elapsedSeconds % 3600) / 60);
      const s = elapsedSeconds % 60;
      const pad = (n: number) => String(n).padStart(2, "0");
      const label = h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
      document.title = `${label} · Multica`;
    };

    tick();
    const id = setInterval(tick, 1000);
    return () => {
      clearInterval(id);
      document.title = "Multica";
    };
  }, [currentEntry]);

  const handleStart = () => {
    const now = new Date().toISOString();
    startMutation.mutate(
      { description: description.trim() || undefined, start_time: now },
      {
        onSuccess: () => {
          setDescription("");
          setExpanded(false);
        },
        onError: () => {
          toast.error("Failed to start timer");
        },
      },
    );
  };

  const handleStop = () => {
    if (!currentEntry) return;
    stopMutation.mutate(currentEntry.id, {
      onError: () => toast.error("Failed to stop timer"),
    });
  };

  // ── Running state ───────────────────────────────────────────────────────────
  if (isRunning && currentEntry) {
    return (
      <SidebarMenuItem>
        <div className="flex items-center gap-2 px-2 py-1.5 text-sm">
          <Clock className="size-4 shrink-0 text-brand" />
          <div className="min-w-0 flex-1">
            {currentEntry.description ? (
              <p className="truncate text-xs text-muted-foreground">{currentEntry.description}</p>
            ) : null}
            <LiveDuration
              entry={currentEntry}
              className="text-sm font-semibold text-foreground"
            />
          </div>
          <Button
            size="icon"
            variant="ghost"
            className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
            disabled={stopMutation.isPending}
            onClick={handleStop}
            aria-label="Stop timer"
          >
            <Square className="size-3.5 fill-current" />
          </Button>
        </div>
      </SidebarMenuItem>
    );
  }

  // ── Idle / expanded inline form ─────────────────────────────────────────────
  if (expanded) {
    return (
      <SidebarMenuItem>
        <div className="px-2 py-1.5 space-y-2">
          <Input
            ref={inputRef}
            placeholder="What are you working on?"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleStart();
              if (e.key === "Escape") setExpanded(false);
            }}
            className="h-7 text-xs"
          />
          <div className="flex gap-1.5">
            <Button
              size="sm"
              className="h-7 flex-1 text-xs"
              disabled={startMutation.isPending}
              onClick={handleStart}
            >
              <Play className="mr-1 size-3" />
              Start
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="size-7 shrink-0 text-muted-foreground"
              onClick={() => setExpanded(false)}
              aria-label="Cancel"
            >
              <X className="size-3.5" />
            </Button>
          </div>
        </div>
      </SidebarMenuItem>
    );
  }

  // ── Idle / collapsed ────────────────────────────────────────────────────────
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        className="text-muted-foreground"
        onClick={() => setExpanded(true)}
      >
        <Clock />
        <span>Track time</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
