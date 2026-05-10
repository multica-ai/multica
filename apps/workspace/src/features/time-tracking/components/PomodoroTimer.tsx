"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Timer, Play, Pause, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import type { PomodoroSession } from "@/shared/types";
import {
  usePomodoroQuery,
  useStartPomodoroMutation,
  usePausePomodoroMutation,
  useCompletePomodoroMutation,
  useResetPomodoroMutation,
} from "../hooks/use-pomodoro";

interface Props {
  /** Called when a work phase completes (optional — e.g. to auto-stop the live timer). */
  onWorkComplete?: () => void;
}

/**
 * Calculate the remaining seconds from a server-side session,
 * compensating for clock drift when the timer is running client-side.
 */
function calcRemaining(session: PomodoroSession): number {
  if (session.status === "running" && session.started_at) {
    const runningFor = (Date.now() - new Date(session.started_at).getTime()) / 1000;
    return Math.max(0, session.phase_duration_seconds - session.elapsed_seconds - runningFor);
  }
  return Math.max(0, session.phase_duration_seconds - session.elapsed_seconds);
}

/** Play a short beep using the Web Audio API (no asset dependency). */
function playBeep() {
  try {
    const ctx = new AudioContext();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.type = "sine";
    osc.frequency.setValueAtTime(880, ctx.currentTime);
    gain.gain.setValueAtTime(0.3, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.6);
    osc.start(ctx.currentTime);
    osc.stop(ctx.currentTime + 0.6);
  } catch {
    // AudioContext not available (SSR / restricted environment).
  }
}

/**
 * Pomodoro timer driven by server-side session state.
 * Survives page refreshes — session is fetched from the backend on mount.
 */
export function PomodoroTimer({ onWorkComplete }: Props) {
  const { data: session, isLoading } = usePomodoroQuery();

  // Local display counter — synced from server on every session change.
  const [remaining, setRemaining] = useState<number>(25 * 60);

  const startMutation = useStartPomodoroMutation();
  const pauseMutation = usePausePomodoroMutation();
  const completeMutation = useCompletePomodoroMutation();
  const resetMutation = useResetPomodoroMutation();

  // Prevent firing completePomodoro() more than once per phase transition.
  const completingRef = useRef(false);

  // Sync remaining from server whenever the session changes.
  useEffect(() => {
    if (!session) return;
    completingRef.current = false;
    setRemaining(Math.round(calcRemaining(session)));
  }, [session]);

  // Tick down every second while the session is running.
  useEffect(() => {
    if (!session || session.status !== "running") return;

    const id = setInterval(() => {
      setRemaining((prev) => {
        const next = prev - 1;
        if (next <= 0 && !completingRef.current) {
          completingRef.current = true;
          // Trigger phase completion asynchronously so we don't update state inside setState.
          setTimeout(() => {
            completeMutation.mutate(undefined, {
              onSuccess: () => {
                const label =
                  session.phase === "work" ? "专注时间结束！休息一下 🎉" : "休息结束！继续工作 💪";
                toast.info(label);
                playBeep();
                if (session.phase === "work") onWorkComplete?.();
              },
            });
          }, 0);
          return 0;
        }
        return Math.max(0, next);
      });
    }, 1000);

    return () => clearInterval(id);
    // completeMutation.mutate is stable across renders — exclude from deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);

  const handleToggle = () => {
    if (!session) return;
    if (session.status === "running") {
      pauseMutation.mutate();
    } else {
      startMutation.mutate();
    }
  };

  const handleReset = () => {
    resetMutation.mutate();
  };

  // Show a sensible placeholder while loading the initial session.
  const isRunning = session?.status === "running";
  const phase = session?.phase ?? "work";
  const minutes = Math.floor(remaining / 60);
  const seconds = remaining % 60;
  const display = isLoading
    ? "25:00"
    : `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;

  const isPending =
    startMutation.isPending ||
    pauseMutation.isPending ||
    completeMutation.isPending ||
    resetMutation.isPending;

  return (
    <div className="flex items-center gap-2 px-2 py-1.5">
      <Timer className="size-4 shrink-0 text-brand" />
      <div className="flex items-center gap-1.5 flex-1">
        <span className="text-sm font-mono font-semibold tabular-nums text-foreground">
          {display}
        </span>
        <span className="text-xs text-muted-foreground">
          {phase === "work" ? "专注" : "休息"}
        </span>
      </div>
      <Button
        size="icon"
        variant="ghost"
        className="size-7 shrink-0 text-muted-foreground"
        disabled={isPending || isLoading}
        onClick={handleToggle}
        aria-label={isRunning ? "暂停番茄钟" : "开始番茄钟"}
      >
        {isRunning ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
      </Button>
      <Button
        size="icon"
        variant="ghost"
        className="size-7 shrink-0 text-muted-foreground"
        disabled={isPending || isLoading}
        onClick={handleReset}
        aria-label="重置番茄钟"
      >
        <RotateCcw className="size-3.5" />
      </Button>
    </div>
  );
}

