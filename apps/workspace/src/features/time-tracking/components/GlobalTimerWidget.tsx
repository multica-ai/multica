"use client";

import { useState, useEffect, useRef } from "react";
import { Clock, Square, Play, X, Timer } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { usePathname } from "@/shared/router";
import { useCurrentTimerQuery, useStopTimerMutation } from "../hooks/use-time-tracking";
import { useTimeEntryActions } from "../hooks/use-time-entry-actions";
import { LiveDuration } from "./LiveDuration";
import { PomodoroTimer } from "./PomodoroTimer";
import { ConfirmTimerSwitchDialog } from "./ConfirmTimerSwitchDialog";

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
  const [isStarting, setIsStarting] = useState(false);
  const [isSwitching, setIsSwitching] = useState(false);
  // Toggles between normal time-tracking mode and Pomodoro mode.
  // Persisted in localStorage so the mode survives page refreshes.
  const [pomodoroMode, setPomodoroMode] = useState(() => {
    try {
      return localStorage.getItem("pomodoro-mode") === "true";
    } catch {
      return false;
    }
  });

  const handleSetPomodoroMode = (val: boolean) => {
    setPomodoroMode(val);
    try {
      localStorage.setItem("pomodoro-mode", String(val));
    } catch {
      // Ignore storage writes when the environment does not expose localStorage.
    }
  };
  const inputRef = useRef<HTMLInputElement>(null);
  // Used to detect the /pomodoro route so we yield document.title to PomodoroTimer there too.
  const pathname = usePathname();

  const { data: currentEntry } = useCurrentTimerQuery();
  const { requestStart, pendingSwitch, confirmSwitch, setPendingSwitch } = useTimeEntryActions({ currentEntry });
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
  // Yields title ownership to PomodoroTimer when pomodoroMode is active OR when
  // the user is on the /pomodoro route (PomodoroTimer variant="page" renders there).
  useEffect(() => {
    if (pomodoroMode || pathname.startsWith("/pomodoro")) {
      // PomodoroTimer owns document.title — do nothing here.
      return;
    }

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
  }, [currentEntry, pomodoroMode, pathname]);

  const handleStart = async () => {
    if (isStarting) return;
    const now = new Date().toISOString();
    setIsStarting(true);
    try {
      const result = await requestStart({
        description: description.trim() || undefined,
        start_time: now,
      });
      // Only clear input and collapse if the timer actually started (not just staged)
      if (result !== null) {
        setDescription("");
        setExpanded(false);
      }
    } catch (error) {
      toast.error("Failed to start timer");
    } finally {
      setIsStarting(false);
    }
  };

  const handleConfirmSwitch = async () => {
    setIsSwitching(true);
    try {
      await confirmSwitch();
      setDescription("");
      setExpanded(false);
    } catch (error) {
      toast.error("Failed to switch timer");
    } finally {
      setIsSwitching(false);
    }
  };

  const handleStop = () => {
    if (!currentEntry) return;
    stopMutation.mutate(currentEntry.id, {
      onError: () => toast.error("Failed to stop timer"),
    });
  };

  // ── Pomodoro mode ───────────────────────────────────────────────────────────
  if (pomodoroMode) {
    // When the user is on /pomodoro, PomodoroTimer variant="page" is already
    // mounted by PomodoroPage. Rendering the compact variant here as well would
    // create two live instances sharing the same session — each with its own
    // countdown interval, completingRef, and completeMutation — which can
    // trigger double-completion. Suppress the compact timer on that route and
    // show only the mode-switch control so the sidebar remains useful.
    const onPomodoroPage = pathname.startsWith("/pomodoro");
    return (
      <SidebarMenuItem>
        <div className="space-y-1">
          {!onPomodoroPage && (
            <PomodoroTimer variant="compact" onWorkComplete={() => { if (isRunning && currentEntry) handleStop(); }} />
          )}
          <div className="px-2">
            <Button
              size="sm"
              variant="ghost"
              className="h-6 w-full text-xs text-muted-foreground justify-start"
              onClick={() => handleSetPomodoroMode(false)}
            >
              <Clock className="mr-1 size-3" />
              切换为普通计时
            </Button>
          </div>
        </div>
      </SidebarMenuItem>
    );
  }

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
      <>
        <SidebarMenuItem>
          <div className="px-2 py-1.5 space-y-2">
            <Input
              ref={inputRef}
              placeholder="What are you working on?"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void handleStart();
                if (e.key === "Escape") setExpanded(false);
              }}
              className="h-7 text-xs"
            />
            <div className="flex gap-1.5">
              <Button
                size="sm"
                className="h-7 flex-1 text-xs"
                disabled={isStarting}
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

        <ConfirmTimerSwitchDialog
          open={!!pendingSwitch}
          isLoading={isSwitching}
          onCancel={() => setPendingSwitch(null)}
          onConfirm={handleConfirmSwitch}
        />
      </>
    );
  }

  // ── Idle / collapsed ────────────────────────────────────────────────────────
  return (
    <SidebarMenuItem>
      <div className="space-y-0.5">
        <SidebarMenuButton
          className="text-muted-foreground"
          onClick={() => setExpanded(true)}
        >
          <Clock />
          <span>Track time</span>
        </SidebarMenuButton>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 w-full text-xs text-muted-foreground justify-start px-2"
          onClick={() => handleSetPomodoroMode(true)}
        >
          <Timer className="mr-1 size-3" />
          番茄钟
        </Button>
      </div>
    </SidebarMenuItem>
  );
}
