"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, Coffee, Focus, Link2, Pause, Play, RotateCcw, Square, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "@/features/issues/components/pickers/property-picker";
import { useIssueStore } from "@/features/issues";
import { useIssuesListQuery } from "@/features/issues/queries";
import type { FocusMode, FocusReason, IssueReference } from "@/shared/types";
import {
  useAbandonFocusMutation,
  useCompleteFocusBreakMutation,
  useCompleteFocusMutation,
  useCompleteQuickStartMutation,
  useFocusQuery,
  usePauseFocusMutation,
  useResumeFocusMutation,
  useSkipFocusBreakMutation,
  useStartFocusBreakMutation,
  useStartFocusMutation,
  useUpdateFocusMutation,
} from "../hooks/use-focus";
import {
  useTimeEntryLabelMutations,
  useTimeEntryLabelsQuery,
} from "../hooks/use-time-tracking";
import { TimeEntryLabelPicker } from "../components/time-entry-label-picker";

const reasonOptions: Array<{ value: FocusReason; label: string }> = [
  { value: "unclear_next_step", label: "Next step unclear" },
  { value: "too_large", label: "Too large" },
  { value: "low_energy", label: "Low energy" },
  { value: "avoidance", label: "Avoidance" },
  { value: "interruption", label: "Interruption" },
  { value: "blocked", label: "Blocked" },
  { value: "other", label: "Other" },
];

const modeOptions: Array<{ value: FocusMode; label: string; preset?: string }> = [
  { value: "flowtime", label: "Flowtime", preset: "flowtime_default" },
  { value: "pomodoro", label: "Pomodoro", preset: "pomodoro_25_5" },
  { value: "quick_start", label: "2 min start", preset: "two_minute_start" },
];

const defaultModeOption = modeOptions[0] as { value: FocusMode; label: string; preset?: string };

/** Formats a duration in seconds as H:MM:SS or M:SS. */
function formatDuration(seconds: number): string {
  const safeSeconds = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(safeSeconds / 3600);
  const minutes = Math.floor((safeSeconds % 3600) / 60);
  const secs = safeSeconds % 60;
  const pad = (value: number) => String(value).padStart(2, "0");
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(secs)}` : `${minutes}:${pad(secs)}`;
}

/** Computes the live elapsed focus seconds from a persisted session snapshot. */
function getLiveElapsedSeconds(session: { phase: string; elapsed_focus_seconds: number; started_at?: string | null }): number {
  if (session.phase !== "focusing" || !session.started_at) {
    return session.elapsed_focus_seconds;
  }
  return session.elapsed_focus_seconds + Math.max(0, (Date.now() - new Date(session.started_at).getTime()) / 1000);
}

/** Computes the remaining break seconds from a persisted breaking session. */
function getBreakRemainingSeconds(session: { phase: string; suggested_break_seconds?: number | null; started_at?: string | null }): number {
  if (session.phase !== "breaking" || !session.started_at || !session.suggested_break_seconds) {
    return session.suggested_break_seconds ?? 0;
  }
  const elapsed = Math.max(0, (Date.now() - new Date(session.started_at).getTime()) / 1000);
  return Math.max(0, session.suggested_break_seconds - elapsed);
}

/**
 * Searchable issue picker for Focus context.
 */
function FocusIssuePicker({
  selectedIssueId,
  onChange,
}: {
  selectedIssueId: string | null;
  onChange: (issueId: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const storeIssues = useIssueStore((s) => s.issues) as IssueReference[];
  const { data } = useIssuesListQuery();
  const allIssues = data?.issues ?? storeIssues;

  const filtered = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return allIssues;
    return allIssues.filter((issue) =>
      issue.title.toLowerCase().includes(query) ||
      issue.identifier.toLowerCase().includes(query),
    );
  }, [allIssues, filter]);

  const selected = allIssues.find((issue) => issue.id === selectedIssueId) ?? null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
      width="w-80"
      align="start"
      searchable
      searchPlaceholder="Search issues..."
      onSearchChange={setFilter}
      trigger={
        selected ? (
          <>
            <Link2 className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate text-sm">
              {selected.identifier} · {selected.title}
            </span>
            <button
              type="button"
              className="ml-auto shrink-0 text-muted-foreground hover:text-foreground"
              onClick={(event) => {
                event.stopPropagation();
                onChange(null);
              }}
              aria-label="Remove issue link"
            >
              <X className="size-3" />
            </button>
          </>
        ) : (
          <span className="text-sm text-muted-foreground">Link issue</span>
        )
      }
    >
      {selectedIssueId && (
        <PickerItem
          selected={false}
          onClick={() => {
            onChange(null);
            setOpen(false);
          }}
        >
          <X className="size-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">No issue</span>
        </PickerItem>
      )}

      {filtered.map((issue) => (
        <PickerItem
          key={issue.id}
          selected={issue.id === selectedIssueId}
          onClick={() => {
            onChange(issue.id);
            setOpen(false);
          }}
        >
          {issue.id === selectedIssueId ? (
            <Check className="size-3.5 shrink-0 text-primary" />
          ) : (
            <Link2 className="size-3.5 shrink-0 text-muted-foreground" />
          )}
          <span className="flex min-w-0 flex-1 flex-col items-start">
            <span className="truncate text-sm">{issue.title}</span>
            <span className="text-[11px] text-muted-foreground">{issue.identifier}</span>
          </span>
        </PickerItem>
      ))}

      {filtered.length === 0 && <PickerEmpty />}
    </PropertyPicker>
  );
}

/** Main Focus Mode page. */
export function FocusPage() {
  const { data: session, isLoading } = useFocusQuery();
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [mode, setMode] = useState<FocusMode>("flowtime");
  const [issueId, setIssueId] = useState<string | null>(null);
  const [description, setDescription] = useState("");
  const [commitment, setCommitment] = useState("");
  const [labelIds, setLabelIds] = useState<string[]>([]);
  const [reason, setReason] = useState<FocusReason | "">("");
  const [reasonNote, setReasonNote] = useState("");
  const [pauseReason, setPauseReason] = useState<FocusReason | "">("");
  const [abandonReason, setAbandonReason] = useState<FocusReason | "">("");
  const { data: labels = [] } = useTimeEntryLabelsQuery();
  const { createTimeEntryLabel } = useTimeEntryLabelMutations();
  const startFocus = useStartFocusMutation();
  const updateFocus = useUpdateFocusMutation();
  const pauseFocus = usePauseFocusMutation();
  const resumeFocus = useResumeFocusMutation();
  const completeQuickStart = useCompleteQuickStartMutation();
  const completeFocus = useCompleteFocusMutation();
  const abandonFocus = useAbandonFocusMutation();
  const startBreak = useStartFocusBreakMutation();
  const skipBreak = useSkipFocusBreakMutation();
  const completeBreak = useCompleteFocusBreakMutation();

  useEffect(() => {
    const intervalId = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(intervalId);
  }, []);

  useEffect(() => {
    if (!session || session.phase === "idle") return;
    setMode(session.mode);
    setIssueId(session.issue_id ?? null);
    setDescription(session.description ?? "");
    setCommitment(session.commitment_text ?? "");
    setLabelIds(session.label_ids ?? []);
  }, [session?.id, session?.phase]);

  const selectedMode = modeOptions.find((option) => option.value === mode) ?? defaultModeOption;
  const currentMode = session ? (modeOptions.find((option) => option.value === session.mode) ?? selectedMode) : selectedMode;
  const elapsed = session ? getLiveElapsedSeconds(session) : 0;
  const quickStartRemaining = session?.mode === "quick_start" && session.phase === "focusing"
    ? Math.max(0, 120 - elapsed)
    : 0;
  const quickStartReady = session?.mode === "quick_start" && session.phase === "focusing" && quickStartRemaining <= 0;
  const breakRemaining = session ? getBreakRemainingSeconds(session) : 0;
  const isBusy =
    startFocus.isPending ||
    updateFocus.isPending ||
    pauseFocus.isPending ||
    resumeFocus.isPending ||
    completeQuickStart.isPending ||
    completeFocus.isPending ||
    abandonFocus.isPending ||
    startBreak.isPending ||
    skipBreak.isPending ||
    completeBreak.isPending;

  const persistContext = () => {
    updateFocus.mutate({
      issue_id: issueId,
      description: description.trim() || undefined,
      commitment_text: commitment.trim() || undefined,
      label_ids: labelIds,
    });
  };

  const handleStart = () => {
    startFocus.mutate({
      mode,
      preset: selectedMode.preset,
      issue_id: issueId,
      description: description.trim() || undefined,
      commitment_text: commitment.trim() || undefined,
      label_ids: labelIds,
      timer_conflict_action: "stop_existing",
      resistance_reason: reason || undefined,
      resistance_note: reasonNote.trim() || undefined,
    }, {
      onError: () => toast.error("Failed to start focus"),
    });
  };

  const handlePause = () => {
    pauseFocus.mutate({
      reason: pauseReason || undefined,
    }, {
      onError: () => toast.error("Failed to pause focus"),
    });
  };

  const handleAbandon = () => {
    abandonFocus.mutate({
      reason: abandonReason || undefined,
      note: reasonNote.trim() || undefined,
    }, {
      onError: () => toast.error("Failed to abandon focus"),
    });
  };

  const handleComplete = () => {
    completeFocus.mutate({
      note: description.trim() || undefined,
      end_reason: "completed",
    }, {
      onError: () => toast.error("Failed to complete focus"),
    });
  };

  const handleCompleteQuickStart = () => {
    completeQuickStart.mutate(undefined, {
      onError: () => toast.error("Failed to complete quick start"),
    });
  };

  const isActive = session?.phase === "focusing" || session?.phase === "paused";
  const canEditContext = !session || session.phase === "idle" || isActive;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="border-b px-6 py-4">
        <div className="mx-auto flex w-full max-w-5xl items-center gap-2">
          <Focus className="size-4 text-muted-foreground" />
          <div>
            <h1 className="text-base font-medium text-foreground">Focus</h1>
            <p className="text-xs text-muted-foreground">Flowtime, Pomodoro, and low-friction starts.</p>
          </div>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto grid w-full max-w-5xl gap-6 lg:grid-cols-[minmax(0,1.35fr)_minmax(280px,0.8fr)]">
          <section aria-label="Current focus" className="rounded-xl border bg-card p-5">
            <div className="space-y-6">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h2 className="text-sm font-medium text-foreground">Current focus</h2>
                  <p className="text-xs text-muted-foreground">
                    {session?.phase === "break_suggested"
                      ? "Take a recovery break before the next block."
                      : session?.phase === "breaking"
                        ? "Break in progress."
                        : "Choose a mode and commit to the next step."}
                  </p>
                </div>
                <span className="rounded-full border px-2.5 py-1 text-xs text-muted-foreground">
                  {session?.phase ?? "idle"}
                </span>
                <span className="rounded-full border px-2.5 py-1 text-xs text-muted-foreground">
                  {currentMode.label}
                </span>
              </div>

              <div className="flex flex-col items-center gap-2 py-5">
                <span className="font-mono text-6xl font-bold tabular-nums text-foreground">
                  {session?.phase === "breaking"
                    ? formatDuration(breakRemaining)
                    : session?.mode === "quick_start" && session.phase === "focusing"
                      ? formatDuration(quickStartRemaining)
                      : formatDuration(elapsed)}
                </span>
                <span className="text-xs text-muted-foreground">
                  {session?.phase === "breaking"
                    ? "Break remaining"
                    : session?.mode === "quick_start" && session.phase === "focusing"
                      ? "Quick start remaining"
                      : selectedMode.label}
                </span>
              </div>

              {session?.phase === "break_suggested" || session?.phase === "breaking" ? (
                <div className="rounded-lg border bg-muted/30 p-4">
                  <div className="mb-3 flex items-center gap-2">
                    <Coffee className="size-4 text-muted-foreground" />
                    <div>
                      <p className="text-sm font-medium">Suggested break</p>
                      <p className="text-xs text-muted-foreground">
                        {formatDuration(session.suggested_break_seconds ?? 0)}
                      </p>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {session.phase === "break_suggested" ? (
                      <>
                        <Button size="sm" disabled={isBusy} onClick={() => startBreak.mutate()}>
                          <Play className="mr-1.5 size-3.5" />
                          Start break
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={isBusy}
                          onClick={() => skipBreak.mutate({ reason: "not_needed" })}
                        >
                          Skip
                        </Button>
                      </>
                    ) : (
                      <Button size="sm" disabled={isBusy} onClick={() => completeBreak.mutate()}>
                        <Check className="mr-1.5 size-3.5" />
                        Complete break
                      </Button>
                    )}
                  </div>
                </div>
              ) : (
                <div className="flex flex-wrap justify-center gap-2">
                  {!session || session.phase === "idle" || session.phase === "abandoned" ? (
                    <Button disabled={isBusy || isLoading} onClick={handleStart}>
                      <Play className="mr-2 size-4" />
                      Start
                    </Button>
                  ) : session.phase === "paused" ? (
                    <Button disabled={isBusy} onClick={() => resumeFocus.mutate()}>
                      <Play className="mr-2 size-4" />
                      Resume
                    </Button>
                  ) : (
                    <Button disabled={isBusy} onClick={handlePause}>
                      <Pause className="mr-2 size-4" />
                      Pause
                    </Button>
                  )}
                  {quickStartReady && (
                    <Button disabled={isBusy} onClick={handleCompleteQuickStart}>
                      <Check className="mr-2 size-4" />
                      Continue Flowtime
                    </Button>
                  )}
                  {isActive && (
                    <>
                      <Button variant="outline" disabled={isBusy} onClick={handleComplete}>
                        <Square className="mr-2 size-4" />
                        Complete
                      </Button>
                      <Button variant="ghost" disabled={isBusy} onClick={handleAbandon}>
                        <RotateCcw className="mr-2 size-4" />
                        Abandon
                      </Button>
                    </>
                  )}
                </div>
              )}
            </div>
          </section>

          <section aria-label="Focus context" className="rounded-xl border bg-card p-5">
            <div className="space-y-4">
              <div>
                <h2 className="text-sm font-medium">Context</h2>
                <p className="text-xs text-muted-foreground">Keep this block tied to a concrete next action.</p>
              </div>

              <div className="grid gap-2">
                <Label htmlFor="focus-mode">Mode</Label>
                <select
                  id="focus-mode"
                  value={mode}
                  disabled={!canEditContext}
                  onChange={(event) => setMode(event.target.value as FocusMode)}
                  className="h-9 rounded-md border bg-background px-3 text-sm"
                >
                  {modeOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="grid gap-2">
                <Label>Issue</Label>
                <FocusIssuePicker selectedIssueId={issueId} onChange={setIssueId} />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="focus-commitment">Next step</Label>
                <Input
                  id="focus-commitment"
                  value={commitment}
                  disabled={!canEditContext}
                  placeholder="Open the failing CI log"
                  onChange={(event) => setCommitment(event.target.value)}
                />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="focus-description">Note</Label>
                <Textarea
                  id="focus-description"
                  value={description}
                  disabled={!canEditContext}
                  placeholder="What should this focus block capture?"
                  onChange={(event) => setDescription(event.target.value)}
                />
              </div>

              <div className="grid gap-2">
                <Label>Labels</Label>
                <TimeEntryLabelPicker
                  labels={labels}
                  selectedIds={labelIds}
                  onAdd={async ({ labelId, name }) => {
                    if (labelId) {
                      setLabelIds((current) => current.includes(labelId) ? current : [...current, labelId]);
                      return;
                    }
                    if (!name?.trim()) return;
                    const created = await createTimeEntryLabel({ name: name.trim() });
                    setLabelIds((current) => current.includes(created.id) ? current : [...current, created.id]);
                  }}
                  onRemove={async (labelId) => {
                    setLabelIds((current) => current.filter((id) => id !== labelId));
                  }}
                />
              </div>

              {(!session || session.phase === "idle" || session.phase === "abandoned") && (
                <>
                  <div className="grid gap-2">
                    <Label htmlFor="focus-reason">Start friction</Label>
                    <select
                      id="focus-reason"
                      value={reason}
                      onChange={(event) => setReason(event.target.value as FocusReason | "")}
                      className="h-9 rounded-md border bg-background px-3 text-sm"
                    >
                      <option value="">No reason</option>
                      {reasonOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </div>
                  <Input
                    value={reasonNote}
                    placeholder="Optional reason note"
                    onChange={(event) => setReasonNote(event.target.value)}
                  />
                </>
              )}

              {session?.phase === "focusing" && (
                <div className="grid gap-2">
                  <Label htmlFor="pause-reason">Pause reason</Label>
                  <select
                    id="pause-reason"
                    value={pauseReason}
                    onChange={(event) => setPauseReason(event.target.value as FocusReason | "")}
                    className="h-9 rounded-md border bg-background px-3 text-sm"
                  >
                    <option value="">No reason</option>
                    {reasonOptions.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
              )}

              {isActive && (
                <div className="grid gap-2">
                  <Label htmlFor="abandon-reason">Abandon reason</Label>
                  <select
                    id="abandon-reason"
                    value={abandonReason}
                    onChange={(event) => setAbandonReason(event.target.value as FocusReason | "")}
                    className="h-9 rounded-md border bg-background px-3 text-sm"
                  >
                    <option value="">No reason</option>
                    {reasonOptions.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
              )}

              {canEditContext && session?.phase !== "idle" && (
                <Button variant="outline" size="sm" disabled={isBusy} onClick={persistContext}>
                  Save context
                </Button>
              )}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
