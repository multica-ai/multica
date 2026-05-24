"use client";

import { useState, useEffect, useMemo } from "react";
import { Clock, Link2, X, Check, Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { DateTimePicker } from "@/components/ui/date-time-picker";
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
import type { IssueReference } from "@/shared/types";
import { TimeEntryOverlapApiError } from "@/shared/api";
import { useTimeEntryActions } from "../hooks/use-time-entry-actions";
import { useCurrentTimerQuery } from "../hooks/use-time-tracking";

// ── Helpers ────────────────────────────────────────────────────────────────────

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
 * Inline issue picker used inside the time entry create sheet.
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

export interface TimeEntryCreateSheetProps {
  open: boolean;
  defaultIssueId?: string | null;
  onClose: () => void;
}

/**
 * Slide-over sheet for creating a new historical time entry.
 *
 * Fields:
 * - Description (free text, optional)
 * - Linked issue (searchable picker, optional)
 * - Start time (required)
 * - Stop time (required)
 *
 * Duration is auto-calculated from start/stop and shown as a preview.
 */
export function TimeEntryCreateSheet({
  open,
  defaultIssueId,
  onClose,
}: TimeEntryCreateSheetProps) {
  const { data: currentEntry } = useCurrentTimerQuery();
  const { createHistoricalEntry } = useTimeEntryActions({ currentEntry });

  const [description, setDescription] = useState("");
  const [issueId, setIssueId] = useState<string | null>(defaultIssueId ?? null);
  const [startIso, setStartIso] = useState<string | null>(null);
  const [stopIso, setStopIso] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  // Reset form when the sheet opens or defaultIssueId changes.
  useEffect(() => {
    if (open) {
      setIssueId(defaultIssueId ?? null);
    }
  }, [open, defaultIssueId]);

  /** Restores the sheet fields to their initial values. */
  const resetForm = () => {
    setDescription("");
    setIssueId(defaultIssueId ?? null);
    setStartIso(null);
    setStopIso(null);
  };

  // Duration preview computed from current ISO values.
  const previewDuration = (() => {
    if (!startIso || !stopIso) return null;
    const diff = new Date(stopIso).getTime() - new Date(startIso).getTime();
    if (diff <= 0) return null;
    return Math.round(diff / 1000);
  })();

  const handleSave = async () => {
    if (!startIso || !stopIso) {
      toast.error("Start and stop times are required");
      return;
    }

    if (new Date(stopIso) < new Date(startIso)) {
      toast.error("Stop time must be after start time");
      return;
    }

    setIsSaving(true);
    try {
      await createHistoricalEntry({
       description: description.trim() || undefined,
       issue_id: issueId ?? undefined,
       start_time: startIso,
       stop_time: stopIso,
      });
      toast.success("Time entry created");
      resetForm();
      onClose();
    } catch (error) {
      if (error instanceof TimeEntryOverlapApiError) {
       toast.error("May overlap with an existing entry", {
         description: "Review the time range or save anyway to keep both entries.",
         action: {
           label: "Save anyway",
           onClick: async () => {
             try {
               await createHistoricalEntry({
                 description: description.trim() || undefined,
                 issue_id: issueId ?? undefined,
                 start_time: startIso,
                 stop_time: stopIso,
                 confirm_overlap: true,
               });
               toast.success("Time entry created");
               resetForm();
               onClose();
             } catch {
               toast.error("Failed to create time entry");
             }
           },
         },
       });
      } else {
       toast.error("Failed to create time entry");
      }
    } finally {
      setIsSaving(false);
    }
  };

  const handleClose = () => {
    if (!isSaving) {
      resetForm();
      onClose();
    }
  };

  return (
    <Sheet open={open} onOpenChange={(next) => !next && handleClose()}>
      <SheetContent className="flex flex-col gap-0 p-0 sm:max-w-md">
        <SheetHeader className="border-b px-6 py-4">
          <div className="flex items-center gap-2">
            <Clock className="size-4 text-muted-foreground" />
            <SheetTitle className="text-base">Add time entry</SheetTitle>
          </div>
        </SheetHeader>

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
            <Label>Issue (optional)</Label>
            <div className="flex items-center gap-1.5 rounded-md border px-3 py-1.5 min-h-9">
              <IssuePicker selectedIssueId={issueId} onChange={setIssueId} />
            </div>
          </div>

          {/* Start time */}
          <div className="space-y-2">
            <Label>Start time</Label>
            <div className="flex items-center rounded-md border px-3 py-1.5 min-h-9">
              <DateTimePicker
                value={startIso}
                onChange={(v) => setStartIso(v)}
                placeholder="Pick start time"
                required
                align="start"
                showSeconds
              />
            </div>
          </div>

          {/* Stop time */}
          <div className="space-y-2">
            <Label>Stop time</Label>
            <div className="flex items-center rounded-md border px-3 py-1.5 min-h-9">
              <DateTimePicker
                value={stopIso}
                onChange={(v) => setStopIso(v)}
                placeholder="Pick stop time"
                required
                align="start"
                showSeconds
              />
            </div>
          </div>

          {/* Duration preview */}
          {previewDuration !== null && (
            <div className="rounded-md bg-muted px-4 py-3 text-sm">
              <span className="text-muted-foreground">Duration: </span>
              <span className="font-mono font-semibold">{formatDuration(previewDuration)}</span>
            </div>
          )}
        </div>

        {/* Footer actions */}
        <div className="flex items-center justify-end gap-2 border-t px-6 py-4">
          <Button variant="outline" onClick={handleClose} disabled={isSaving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isSaving}>
            <Save className="mr-1.5 size-3.5" />
            Save entry
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
