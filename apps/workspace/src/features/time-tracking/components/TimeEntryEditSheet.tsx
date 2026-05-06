"use client";

import { useEffect, useState } from "react";
import { Trash2, Clock, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import type { TimeEntry } from "@/shared/types";
import { useUpdateTimeEntryMutation, useDeleteTimeEntryMutation } from "../hooks/use-time-tracking";

// ── Helpers ────────────────────────────────────────────────────────────────────

/**
 * Converts an ISO 8601 timestamp to a value compatible with <input type="datetime-local">.
 * e.g. "2024-06-10T14:30:00Z" → "2024-06-10T14:30"
 */
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * Converts a datetime-local input value back to an ISO 8601 string.
 * The browser gives us local time; we treat it as local and convert to UTC.
 */
function fromDatetimeLocal(value: string): string {
  return new Date(value).toISOString();
}

/** Formats elapsed seconds as H:MM:SS or M:SS. */
function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// ── Component ──────────────────────────────────────────────────────────────────

interface TimeEntryEditSheetProps {
  entry: TimeEntry | null;
  onClose: () => void;
}

/**
 * Slide-over sheet for editing a completed time entry.
 *
 * Editable fields:
 * - Description (free text)
 * - Start time (datetime-local)
 * - Stop time (datetime-local, hidden for running entries)
 *
 * Duration is auto-calculated from start/stop and shown read-only.
 */
export function TimeEntryEditSheet({ entry, onClose }: TimeEntryEditSheetProps) {
  const updateMutation = useUpdateTimeEntryMutation();
  const deleteMutation = useDeleteTimeEntryMutation();

  // Local form state — populated when entry changes.
  const [description, setDescription] = useState("");
  const [startValue, setStartValue] = useState("");
  const [stopValue, setStopValue] = useState("");

  // Sync state when entry changes (sheet opened with a new entry).
  useEffect(() => {
    if (!entry) return;
    setDescription(entry.description ?? "");
    setStartValue(toDatetimeLocal(entry.start_time));
    setStopValue(entry.stop_time ? toDatetimeLocal(entry.stop_time) : "");
  }, [entry?.id]);

  const isRunning = entry ? entry.stop_time === null : false;

  // Compute preview duration from local form values (only for stopped entries).
  const previewDuration = (() => {
    if (!entry || isRunning) return null;
    const start = new Date(startValue).getTime();
    const stop = new Date(stopValue).getTime();
    if (isNaN(start) || isNaN(stop) || stop <= start) return null;
    return Math.round((stop - start) / 1000);
  })();

  const handleSave = () => {
    if (!entry) return;

    // Build only the fields that changed to keep the payload minimal.
    const startIso = fromDatetimeLocal(startValue);
    const stopIso = stopValue ? fromDatetimeLocal(stopValue) : undefined;

    // Validate that stop is after start for completed entries.
    if (!isRunning && stopIso) {
      if (new Date(stopIso) <= new Date(startIso)) {
        toast.error("Stop time must be after start time");
        return;
      }
    }

    updateMutation.mutate(
      {
        id: entry.id,
        data: {
          description: description || undefined,
          start_time: startIso,
          stop_time: stopIso,
        },
      },
      {
        onSuccess: () => {
          toast.success("Time entry updated");
          onClose();
        },
        onError: () => {
          toast.error("Failed to update time entry");
        },
      },
    );
  };

  const handleDelete = () => {
    if (!entry) return;
    deleteMutation.mutate(
      { id: entry.id, issueId: entry.issue_id },
      {
        onSuccess: () => {
          toast.success("Time entry deleted");
          onClose();
        },
        onError: () => toast.error("Failed to delete time entry"),
      },
    );
  };

  return (
    <Sheet open={!!entry} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent className="flex flex-col gap-0 p-0 sm:max-w-md">
        <SheetHeader className="border-b px-6 py-4">
          <div className="flex items-center gap-2">
            <Clock className="size-4 text-muted-foreground" />
            <SheetTitle className="text-base">
              {isRunning ? "Running Timer" : "Edit Time Entry"}
            </SheetTitle>
          </div>
        </SheetHeader>

        {entry && (
          <div className="flex flex-1 flex-col gap-6 overflow-y-auto px-6 py-6">
            {/* Description */}
            <div className="space-y-2">
              <Label htmlFor="entry-description">Description</Label>
              <Input
                id="entry-description"
                placeholder="What were you working on?"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                autoFocus
              />
            </div>

            {/* Start time */}
            <div className="space-y-2">
              <Label htmlFor="entry-start">Start time</Label>
              <Input
                id="entry-start"
                type="datetime-local"
                value={startValue}
                onChange={(e) => setStartValue(e.target.value)}
                disabled={isRunning}
              />
            </div>

            {/* Stop time — hidden for running timers */}
            {!isRunning && (
              <div className="space-y-2">
                <Label htmlFor="entry-stop">Stop time</Label>
                <Input
                  id="entry-stop"
                  type="datetime-local"
                  value={stopValue}
                  onChange={(e) => setStopValue(e.target.value)}
                />
              </div>
            )}

            {/* Duration preview (computed from local form values) */}
            {previewDuration !== null && (
              <div className="rounded-md bg-muted px-4 py-3 text-sm">
                <span className="text-muted-foreground">Duration: </span>
                <span className="font-mono font-semibold">{formatDuration(previewDuration)}</span>
              </div>
            )}

            {/* Issue link (read-only for now, just shows if linked) */}
            {entry.issue_id && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <ExternalLink className="size-3.5 shrink-0" />
                <span>Linked to issue</span>
              </div>
            )}
          </div>
        )}

        {/* Footer actions */}
        <div className="flex items-center justify-between border-t px-6 py-4">
          <Button
            variant="ghost"
            size="sm"
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
            disabled={deleteMutation.isPending}
            onClick={handleDelete}
          >
            <Trash2 className="mr-1.5 size-3.5" />
            Delete
          </Button>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={updateMutation.isPending || isRunning}
              onClick={handleSave}
            >
              Save
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
