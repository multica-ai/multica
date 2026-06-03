"use client";

import { useState, useEffect, useRef } from "react";
import { Timer, Play, Pause, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import type { PomodoroSession, CompletePomodoroBody } from "@/shared/types";
import {
  usePomodoroQuery,
  useStartPomodoroMutation,
  usePausePomodoroMutation,
  useCompletePomodoroMutation,
  useResetPomodoroMutation,
} from "../hooks/use-pomodoro";
import { usePomodoroSettings } from "../hooks/use-pomodoro-settings";
import { useSoundSystem } from "../hooks/use-sound-system";
import type { PomodoroSettings } from "../hooks/use-pomodoro-settings";
import { useTimeEntryLabelsQuery, useTimeEntryLabelMutations } from "../hooks/use-time-tracking";
import { TimeEntryLabelPicker } from "./time-entry-label-picker";

interface Props {
  /** Called when a work phase completes (optional — e.g. to auto-stop the live timer). */
  onWorkComplete?: () => void;
  /**
   * Controls which presentation is used.
   * - "compact" (default): sidebar widget — tight row with play/reset controls.
   * - "page": full /pomodoro page panel — large clock, Current session header, and Quick capture section.
   */
  variant?: "compact" | "page";
}

/** State carried while the user decides what to attach to a completed pomodoro. */
interface CompletionFlowState {
  isWorkPhase: boolean;
  pendingIssueId?: string;
  pendingLabelIds: string[];
  showNoteInput?: boolean;
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

/** Resolve display duration from settings for each phase. */
function phaseDuration(
  phase: PomodoroSession["phase"],
  settings: { work_minutes: number; short_break_minutes: number; long_break_minutes: number },
): number {
  if (phase === "work") return settings.work_minutes * 60;
  if (phase === "long_break") return settings.long_break_minutes * 60;
  return settings.short_break_minutes * 60;
}

/**
 * Pomodoro timer driven by server-side session state.
 * Survives page refreshes — session is fetched from the backend on mount.
 */
export function PomodoroTimer({ onWorkComplete, variant = "compact" }: Props) {
  const { data: session, isLoading } = usePomodoroQuery();
  const { settings } = usePomodoroSettings();
  const {
    playWorkComplete,
    playBreakComplete,
    playStartTick,
    playTick,
    startWhiteNoise,
    stopWhiteNoise,
    updateWhiteNoiseVolume,
  } = useSoundSystem(settings);
  const { data: workspaceLabels = [] } = useTimeEntryLabelsQuery();
  const { createTimeEntryLabel } = useTimeEntryLabelMutations();

  // Local display counter — synced from server on every session change.
  const [remaining, setRemaining] = useState<number>(settings.work_minutes * 60);

  // Inline state for the post-completion flow (link issue / add note / skip).
  const [completionFlow, setCompletionFlow] = useState<CompletionFlowState | null>(null);
  const [noteInputValue, setNoteInputValue] = useState("");

  const startMutation = useStartPomodoroMutation();
  const pauseMutation = usePausePomodoroMutation();
  const completeMutation = useCompletePomodoroMutation();
  const resetMutation = useResetPomodoroMutation();

  // Prevent firing completePomodoro() more than once per phase transition.
  const completingRef = useRef(false);

  // Guard against a second completeMutation call while one is already in-flight
  // (e.g. the inline Skip button and the toast Skip action firing independently).
  const completeMutatingRef = useRef(false);

  // Track previous session status to detect transitions.
  const prevStatusRef = useRef<string | undefined>(undefined);
  // Track the current ambient-noise type so setting changes can restart cleanly.
  const prevAmbientNoiseRef = useRef<PomodoroSettings["white_noise"] | null>(null);

  // Sync remaining from server whenever the session changes.
  useEffect(() => {
    if (!session) return;
    completingRef.current = false;
    completeMutatingRef.current = false;
    setRemaining(Math.round(calcRemaining(session)));
  }, [session]);

  // Update document title while session is running.
  useEffect(() => {
    if (session?.status === "running") {
      const m = Math.floor(remaining / 60).toString().padStart(2, "0");
      const s = (remaining % 60).toString().padStart(2, "0");
      document.title = `🍅 ${m}:${s} · Multica`;
    } else {
      document.title = "Multica";
    }
    return () => {
      document.title = "Multica";
    };
  }, [session?.status, remaining]);

  // Handle ambient-sound transitions when the session or sound settings change.
  useEffect(() => {
    const prevStatus = prevStatusRef.current;
    const currStatus = session?.status;
    const ambientNoiseEnabled = currStatus === "running" && settings.sound_enabled && settings.white_noise !== "none";
    const prevAmbientNoise = prevAmbientNoiseRef.current;

    prevStatusRef.current = currStatus;

    if (!ambientNoiseEnabled) {
      // Stop ambient noise whenever the timer is not running or sound is disabled.
      if (prevAmbientNoise !== null) {
        stopWhiteNoise();
      }
      prevAmbientNoiseRef.current = null;
      return;
    }

    // Restart the noise node on the first running transition or when the noise type changes.
    if (prevStatus !== "running" || prevAmbientNoise !== settings.white_noise) {
      stopWhiteNoise();
      startWhiteNoise(settings.white_noise);
    }
    // Keep the ambient-noise gain in sync while the timer is running.
    updateWhiteNoiseVolume(settings.volume);
    prevAmbientNoiseRef.current = settings.white_noise;
  }, [
    session?.status,
    settings.sound_enabled,
    settings.white_noise,
    settings.volume,
    startWhiteNoise,
    stopWhiteNoise,
    updateWhiteNoiseVolume,
  ]);

  // Helper: fire completePomodoro and handle the full onSuccess flow inline.
  const fireComplete = (body?: CompletePomodoroBody) => {
    // Guard: ignore any call that arrives while a completion mutation is already
    // in-flight. This prevents the inline completion controls and the toast Skip
    // action from both calling completeMutation.mutate on the same phase end.
    if (completeMutatingRef.current) return;
    completeMutatingRef.current = true;
    const wasWork = session?.phase === "work";
    completeMutation.mutate(body, {
      onSuccess: () => {
        if (wasWork) {
          onWorkComplete?.();
          // Auto-start break if configured.
          if (settings.auto_start_break) {
            setTimeout(() => startMutation.mutate(), 1_000);
          }
        } else {
          // Break completed — toast already shown by the countdown handler.
          // Auto-start work if configured.
          if (settings.auto_start_work) {
            setTimeout(() => startMutation.mutate(), 1_000);
          }
        }
      },
      onSettled: () => {
        // Release the guard once the mutation resolves (success or error) so a
        // retry after a network failure is allowed.
        completeMutatingRef.current = false;
      },
    });
  };

  // Completion handlers for the action buttons shown in the toast.
  const handleSkip = () => {
    setCompletionFlow(null);
    fireComplete({ long_break_after: settings.long_break_after });
  };

  const handleSubmitCompletion = () => {
    if (!completionFlow) return;
    const body: CompletePomodoroBody = {
      long_break_after: settings.long_break_after,
      issue_id: completionFlow.pendingIssueId || undefined,
      label_ids: completionFlow.pendingLabelIds,
      note: noteInputValue || undefined,
    };
    setCompletionFlow(null);
    setNoteInputValue("");
    fireComplete(body);
  };

  // Tick down every second while the session is running.
  useEffect(() => {
    if (!session || session.status !== "running") return;

    const id = setInterval(() => {
      setRemaining((prev) => {
        const next = prev - 1;
        if (next > 0 && settings.sound_enabled && settings.tick_enabled) {
          void playTick();
        }
        if (next <= 0 && !completingRef.current) {
          completingRef.current = true;
          const isWorkPhase = session.phase === "work";

          setTimeout(() => {
            if (isWorkPhase) {
              // Work phase complete: play sound and show action toast.
              playWorkComplete();
              setCompletionFlow({ isWorkPhase: true, pendingLabelIds: [] });
              toast(
                "🍅 Pomodoro complete!",
                {
                  duration: 15_000,
                  description: "Great work! What would you like to do?",
                  action: {
                    label: "Skip",
                    onClick: () => {
                      setCompletionFlow(null);
                      fireComplete({ long_break_after: settings.long_break_after });
                    },
                  },
                },
              );
            } else {
              // Break phase complete: auto-dismiss toast and fire immediately.
              playBreakComplete();
              toast.info("☕ Break time over!", { duration: 5_000 });
              fireComplete({ long_break_after: settings.long_break_after });
            }
          }, 0);
          return 0;
        }
        return Math.max(0, next);
      });
    }, 1000);

    return () => clearInterval(id);
  }, [playBreakComplete, playTick, playWorkComplete, session, settings.long_break_after, settings.sound_enabled, settings.tick_enabled]);

  const handleToggle = () => {
    if (!session) return;
    if (session.status === "running") {
      pauseMutation.mutate();
    } else {
      startMutation.mutate(undefined, {
        onSuccess: () => {
          playStartTick();
        },
      });
    }
  };

  const handleReset = () => {
    setCompletionFlow(null);
    setNoteInputValue("");
    resetMutation.mutate();
  };

  // Show a sensible placeholder while loading the initial session.
  const isRunning = session?.status === "running";
  const phase = session?.phase ?? "work";
  const pomodoroCount = session?.pomodoro_count;

  // Display uses settings-derived duration on idle so UI matches settings.
  const displayRemaining =
    isLoading
      ? phaseDuration(phase, settings)
      : remaining;

  const minutes = Math.floor(displayRemaining / 60);
  const seconds = displayRemaining % 60;
  const display = `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;

  const phaseLabel =
    phase === "work" ? "专注" : phase === "long_break" ? "长休" : "休息";

  const isPending =
    startMutation.isPending ||
    pauseMutation.isPending ||
    completeMutation.isPending ||
    resetMutation.isPending;

  // ── Page variant ─────────────────────────────────────────────────────────
  if (variant === "page") {
    const phaseColor =
      phase === "work"
        ? "text-foreground"
        : phase === "long_break"
          ? "text-blue-500"
          : "text-green-500";

    return (
      <section aria-label="Current session" className="rounded-xl border bg-card p-4 sm:p-6">
        <div className="space-y-6">
          {/* Header */}
          <div>
            <h2 className="text-sm font-medium text-foreground">Current session</h2>
            <p className="text-xs text-muted-foreground">Stay in the zone.</p>
          </div>

          {/* Large clock */}
          <div className="flex flex-col items-center gap-2 py-4">
            <span className={`text-6xl font-mono font-bold tabular-nums ${phaseColor}`}>
              {display}
            </span>
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">{phaseLabel}</span>
              {pomodoroCount !== undefined && pomodoroCount > 0 && (
                <span className="text-sm text-muted-foreground tabular-nums">
                  {"🍅".repeat(Math.min(pomodoroCount, 4))}
                </span>
              )}
            </div>
          </div>

          {/* Controls */}
          <div className="flex items-center justify-center gap-3">
            <Button
              size="lg"
              className="w-28"
              disabled={isPending || isLoading}
              onClick={handleToggle}
              aria-label={isRunning ? "Pause timer" : "Start timer"}
            >
              {isRunning ? <Pause className="mr-2 size-4" /> : <Play className="mr-2 size-4" />}
              {isRunning ? "Pause" : "Start"}
            </Button>
            <Button
              size="icon"
              variant="ghost"
              className="size-10 text-muted-foreground"
              disabled={isPending || isLoading}
              onClick={handleReset}
              aria-label="Reset timer"
            >
              <RotateCcw className="size-4" />
            </Button>
          </div>

          {/* Quick capture — always visible in page mode */}
          <div className="border-t pt-4">
            <h3 className="mb-2 text-xs font-medium text-muted-foreground uppercase tracking-wide">
              Quick capture
            </h3>
            {completionFlow ? (
              // Active completion flow: show full note/label/issue inputs
              <div className="space-y-2">
                {completionFlow.showNoteInput ? (
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={noteInputValue}
                      onChange={(e) => setNoteInputValue(e.target.value)}
                      placeholder="Add a note…"
                      className="flex-1 text-sm border rounded px-3 py-1.5 bg-background text-foreground"
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleSubmitCompletion();
                        // Escape backs out of note-input mode only — the completion
                        // flow stays open so the user can still link an issue or skip.
                        if (e.key === "Escape")
                          setCompletionFlow((prev) =>
                            prev ? { ...prev, showNoteInput: false } : null,
                          );
                      }}
                      autoFocus
                    />
                    <Button size="sm" onClick={handleSubmitCompletion}>
                      Save
                    </Button>
                  </div>
                ) : (
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        const id = window.prompt("Enter issue ID to link:");
                        if (id) {
                          setCompletionFlow(null);
                          setNoteInputValue("");
                          fireComplete({
                            long_break_after: settings.long_break_after,
                            issue_id: id,
                          });
                        }
                      }}
                    >
                      Link Issue
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        setCompletionFlow((prev) => (prev ? { ...prev, showNoteInput: true } : null))
                      }
                    >
                      Add Note
                    </Button>
                    <div className="min-w-[160px]">
                      <TimeEntryLabelPicker
                        labels={workspaceLabels}
                        selectedIds={completionFlow.pendingLabelIds}
                        onAdd={async ({ labelId, name }) => {
                          if (labelId) {
                            setCompletionFlow((prev) => {
                              if (!prev) return prev;
                              if (prev.pendingLabelIds.includes(labelId)) return prev;
                              return { ...prev, pendingLabelIds: [...prev.pendingLabelIds, labelId] };
                            });
                            return;
                          }
                          if (!name?.trim()) return;
                          const created = await createTimeEntryLabel({ name: name.trim() });
                          setCompletionFlow((prev) => {
                            if (!prev) return prev;
                            if (prev.pendingLabelIds.includes(created.id)) return prev;
                            return {
                              ...prev,
                              pendingLabelIds: [...prev.pendingLabelIds, created.id],
                            };
                          });
                        }}
                        onRemove={async (labelId) => {
                          setCompletionFlow((prev) => {
                            if (!prev) return prev;
                            return {
                              ...prev,
                              pendingLabelIds: prev.pendingLabelIds.filter((id) => id !== labelId),
                            };
                          });
                        }}
                        align="start"
                      />
                    </div>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-muted-foreground"
                      onClick={handleSkip}
                    >
                      Skip
                    </Button>
                  </div>
                )}
              </div>
            ) : (
              // Idle state: static placeholder when no completion flow is active
              <p className="text-xs text-muted-foreground">
                Capture notes, issues, and labels when the next pomodoro completes.
              </p>
            )}
          </div>
        </div>
      </section>
    );
  }

  // ── Compact variant (default) — sidebar widget ────────────────────────────
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2 px-2 py-1.5">
        <Timer className="size-4 shrink-0 text-brand" />
        <div className="flex items-center gap-1.5 flex-1">
          <span className="text-sm font-mono font-semibold tabular-nums text-foreground">
            {display}
          </span>
          <span className="text-xs text-muted-foreground">{phaseLabel}</span>
          {pomodoroCount !== undefined && pomodoroCount > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {"🍅".repeat(Math.min(pomodoroCount, 4))}
            </span>
          )}
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

      {/* Inline completion flow: shown after work phase ends */}
      {completionFlow && (
        <div className="flex flex-col gap-1.5 px-2 pb-1.5">
          {completionFlow.showNoteInput ? (
            <div className="flex gap-1">
              <input
                type="text"
                value={noteInputValue}
                onChange={(e) => setNoteInputValue(e.target.value)}
                placeholder="Add a note…"
                className="flex-1 text-xs border rounded px-2 py-1 bg-background text-foreground"
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleSubmitCompletion();
                  // Escape backs out of note-input mode only — the completion
                  // flow stays open so the user can still link an issue or skip.
                  if (e.key === "Escape")
                    setCompletionFlow((prev) =>
                      prev ? { ...prev, showNoteInput: false } : null,
                    );
                }}
                autoFocus
              />
              <Button size="sm" className="text-xs h-7" onClick={handleSubmitCompletion}>
                Save
              </Button>
            </div>
          ) : (
            <div className="flex gap-1 flex-wrap">
              <Button
                size="sm"
                variant="outline"
                className="text-xs h-6"
                onClick={() => {
                  const id = window.prompt("Enter issue ID to link:");
                  if (id) {
                    // Bypass setState async — pass issue_id directly to fireComplete.
                    setCompletionFlow(null);
                    setNoteInputValue("");
                    fireComplete({
                      long_break_after: settings.long_break_after,
                      issue_id: id,
                    });
                  }
                }}
              >
                Link Issue
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="text-xs h-6"
                onClick={() =>
                  setCompletionFlow((prev) => prev ? { ...prev, showNoteInput: true } : null)
                }
              >
                Add Note
              </Button>
              <div className="min-w-[160px]">
                <TimeEntryLabelPicker
                  labels={workspaceLabels}
                  selectedIds={completionFlow.pendingLabelIds}
                  onAdd={async ({ labelId, name }) => {
                    if (labelId) {
                      setCompletionFlow((prev) => {
                        if (!prev) return prev;
                        if (prev.pendingLabelIds.includes(labelId)) return prev;
                        return { ...prev, pendingLabelIds: [...prev.pendingLabelIds, labelId] };
                      });
                      return;
                    }
                    if (!name?.trim()) return;
                    const created = await createTimeEntryLabel({ name: name.trim() });
                    setCompletionFlow((prev) => {
                      if (!prev) return prev;
                      if (prev.pendingLabelIds.includes(created.id)) return prev;
                      return { ...prev, pendingLabelIds: [...prev.pendingLabelIds, created.id] };
                    });
                  }}
                  onRemove={async (labelId) => {
                    setCompletionFlow((prev) => {
                      if (!prev) return prev;
                      return { ...prev, pendingLabelIds: prev.pendingLabelIds.filter((id) => id !== labelId) };
                    });
                  }}
                  align="start"
                />
              </div>
              <Button
                size="sm"
                variant="ghost"
                className="text-xs h-6 text-muted-foreground"
                onClick={handleSkip}
              >
                Skip
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
