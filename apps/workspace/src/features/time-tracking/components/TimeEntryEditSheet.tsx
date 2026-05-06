"use client";

import { useEffect, useMemo, useState } from "react";
import { Trash2, Clock, Link2, X, Check } from "lucide-react";
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
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "@/features/issues/components/pickers/property-picker";
import { useIssueStore } from "@/features/issues";
import { useIssuesListQuery } from "@/features/issues/queries";
import type { TimeEntry, IssueReference } from "@/shared/types";
import { useUpdateTimeEntryMutation, useDeleteTimeEntryMutation } from "../hooks/use-time-tracking";

// ── Helpers ────────────────────────────────────────────────────────────────────

/**
 * Converts an ISO 8601 timestamp to a value for <input type="datetime-local">.
 * e.g. "2024-06-10T14:30:00Z" → "2024-06-10T14:30" (local time)
 */
function toDatetimeLocal(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Converts a datetime-local input value back to an ISO 8601 UTC string. */
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

// ── Issue picker ───────────────────────────────────────────────────────────────

/**
 * Inline issue picker used inside the time entry edit sheet.
 * Shows a popover with a searchable list of workspace issues.
 */
function IssuePicker({
  selectedIssueId,
  onChange,
}: {
  selectedIssueId: string | null;
  onChange: (issueId: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  // Load issues from store (already fetched by the issue list page) or fallback to API.
  const storeIssues = useIssueStore((s) => s.issues) as IssueReference[];
  const { data } = useIssuesListQuery();
  const allIssues: IssueReference[] = data?.issues ?? storeIssues;

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return allIssues;
    return allIssues.filter(
      (i) =>
        i.title.toLowerCase().includes(q) ||
        i.identifier.toLowerCase().includes(q),
    );
  }, [allIssues, filter]);

  const selected = allIssues.find((i) => i.id === selectedIssueId) ?? null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setFilter("");
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
              onClick={(e) => {
                e.stopPropagation();
                onChange(null);
              }}
              aria-label="Remove issue link"
            >
              <X className="size-3" />
            </button>
          </>
        ) : (
          <span className="text-muted-foreground text-sm">Link to an issue…</span>
        )
      }
    >
      {/* Clear option */}
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

// ── Main sheet ─────────────────────────────────────────────────────────────────

interface TimeEntryEditSheetProps {
  entry: TimeEntry | null;
  onClose: () => void;
}

/**
 * Slide-over sheet for editing a time entry.
 *
 * Editable fields:
 * - Description (free text)
 * - Linked issue (searchable picker)
 * - Start time (datetime-local)
 * - Stop time (datetime-local, hidden for running entries)
 *
 * Duration is auto-calculated from start/stop and shown read-only.
 */
export function TimeEntryEditSheet({ entry, onClose }: TimeEntryEditSheetProps) {
  const updateMutation = useUpdateTimeEntryMutation();
  const deleteMutation = useDeleteTimeEntryMutation();

  // Local form state — reset whenever a different entry is opened.
  const [description, setDescription] = useState("");
  const [issueId, setIssueId] = useState<string | null>(null);
  const [startValue, setStartValue] = useState("");
  const [stopValue, setStopValue] = useState("");

  useEffect(() => {
    if (!entry) return;
    setDescription(entry.description ?? "");
    setIssueId(entry.issue_id ?? null);
    setStartValue(toDatetimeLocal(entry.start_time));
    setStopValue(entry.stop_time ? toDatetimeLocal(entry.stop_time) : "");
  }, [entry?.id]);

  const isRunning = entry ? entry.stop_time === null : false;

  // Duration preview computed from local form values for stopped entries.
  const previewDuration = (() => {
    if (!entry || isRunning) return null;
    const start = new Date(startValue).getTime();
    const stop = new Date(stopValue).getTime();
    if (isNaN(start) || isNaN(stop) || stop <= start) return null;
    return Math.round((stop - start) / 1000);
  })();

  const handleSave = () => {
    if (!entry) return;

    const startIso = fromDatetimeLocal(startValue);
    const stopIso = stopValue ? fromDatetimeLocal(stopValue) : undefined;

    if (!isRunning && stopIso && new Date(stopIso) <= new Date(startIso)) {
      toast.error("Stop time must be after start time");
      return;
    }

    updateMutation.mutate(
      {
        id: entry.id,
        data: {
          description: description || undefined,
          // Pass null explicitly to clear the link; undefined = no change (but we always send it).
          issue_id: issueId,
          start_time: startIso,
          stop_time: stopIso,
        },
      },
      {
        onSuccess: () => {
          toast.success("Time entry updated");
          onClose();
        },
        onError: () => toast.error("Failed to update time entry"),
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
          <div className="flex flex-1 flex-col gap-5 overflow-y-auto px-6 py-6">
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

            {/* Issue link */}
            <div className="space-y-2">
              <Label>Issue</Label>
              <div className="flex items-center gap-1.5 rounded-md border px-3 py-1.5 min-h-9">
                <IssuePicker selectedIssueId={issueId} onChange={setIssueId} />
              </div>
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

            {/* Duration preview */}
            {previewDuration !== null && (
              <div className="rounded-md bg-muted px-4 py-3 text-sm">
                <span className="text-muted-foreground">Duration: </span>
                <span className="font-mono font-semibold">{formatDuration(previewDuration)}</span>
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


