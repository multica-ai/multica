"use client";

import { useEffect, useMemo, useState } from "react";
import { Trash2, Clock, Link2, X, Check } from "lucide-react";
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
import type { TimeEntry, IssueReference } from "@/shared/types";
import { TimeEntryOverlapApiError } from "@/shared/api";
import { useTimeEntryLabelsQuery, useTimeEntryLabelMutations } from "../hooks/use-time-tracking";
import { useTimeEntryActions } from "../hooks/use-time-entry-actions";
import { TimeEntryLabelPicker } from "./time-entry-label-picker";
import { TimeEntryDeleteDialog } from "./TimeEntryDeleteDialog";

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
 * - Labels (tag picker)
 * - Start time (custom DateTimePicker) — read-only for running entries
 * - Stop time (custom DateTimePicker, hidden for running entries)
 *
 * Running entries allow editing description, issue, and labels only.
 * Historical entries allow editing all fields and show overlap warnings.
 */
export function TimeEntryEditSheet({ entry, onClose }: TimeEntryEditSheetProps) {
  const { updateTimeEntry, requestDelete } = useTimeEntryActions({ currentEntry: entry });
  const { data: workspaceLabels = [] } = useTimeEntryLabelsQuery();
  const { createTimeEntryLabel } = useTimeEntryLabelMutations();

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [isSaving, setIsSaving] = useState(false);

  // Local form state — ISO strings, reset whenever a different entry is opened.
  const [description, setDescription] = useState("");
  const [issueId, setIssueId] = useState<string | null>(null);
  const [labelIds, setLabelIds] = useState<string[]>([]);
  const [startIso, setStartIso] = useState<string | null>(null);
  const [stopIso, setStopIso] = useState<string | null>(null);

  useEffect(() => {
    if (!entry) return;
    setDescription(entry.description ?? "");
    setIssueId(entry.issue_id ?? null);
    setLabelIds((entry.labels ?? []).map((label) => label.id));
    setStartIso(entry.start_time);
    setStopIso(entry.stop_time ?? null);
  }, [entry?.id]);

  useEffect(() => {
    if (!entry) {
      setDeleteDialogOpen(false);
    }
  }, [entry]);

  const selectedLabels = useMemo(() => {
    if (!entry) return [];

    const ids = new Set(labelIds);
    const fromWorkspace = workspaceLabels.filter((label) => ids.has(label.id));
    const fromEntry = (entry.labels ?? []).filter((label) => ids.has(label.id));

    const merged = [...fromWorkspace];
    for (const label of fromEntry) {
      if (!merged.some((candidate) => candidate.id === label.id)) {
        merged.push(label);
      }
    }
    return merged;
  }, [entry, labelIds, workspaceLabels]);

  const isRunning = entry ? entry.stop_time === null : false;

  // Duration preview computed from current ISO values for stopped entries.
  const previewDuration = (() => {
    if (!entry || isRunning || !startIso || !stopIso) return null;
    const diff = new Date(stopIso).getTime() - new Date(startIso).getTime();
    if (diff <= 0) return null;
    return Math.round(diff / 1000);
  })();

  const handleSave = async () => {
    if (!entry || !startIso) return;

    if (!isRunning && stopIso && new Date(stopIso) < new Date(startIso)) {
      toast.error("Stop time must be after start time");
      return;
    }

    const payload = {
      description: description || undefined,
      issue_id: issueId,
      label_ids: labelIds,
      start_time: isRunning ? undefined : startIso,
      stop_time: stopIso ?? undefined,
    };

    setIsSaving(true);

    try {
      await updateTimeEntry(entry.id, payload);

      toast.success("Time entry updated");
      onClose();
    } catch (error) {
      if (error instanceof TimeEntryOverlapApiError) {
        const conflicts = error.conflicts.map((c) => c.description || "Untitled entry").join(", ");
        toast.error(`May overlap with: ${conflicts}`, {
          description: "The entry was not saved. Adjust the times or confirm the overlap.",
          action: {
            label: "Save anyway",
            onClick: async () => {
              try {
                await updateTimeEntry(entry.id, {
                  ...payload,
                  confirm_overlap: true,
                });
                toast.success("Time entry updated");
                onClose();
              } catch {
                toast.error("Failed to update time entry");
              }
            },
          },
        });
      } else {
        toast.error("Failed to update time entry");
      }
    } finally {
      setIsSaving(false);
    }
  };

  const handleDeleteClick = () => {
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!entry) return;

    requestDelete(entry, entry.issue_id);
    setDeleteDialogOpen(false);
    onClose();
  };

  return (
    <>
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

              {/* Labels */}
              <div className="space-y-2">
                <Label>Labels</Label>
                <div className="flex items-center gap-1.5 rounded-md border px-3 py-1.5 min-h-9">
                  <TimeEntryLabelPicker
                    labels={workspaceLabels}
                    selectedIds={selectedLabels.map((label) => label.id)}
                    onAdd={async ({ labelId, name }) => {
                      if (labelId) {
                        setLabelIds((prev) => (prev.includes(labelId) ? prev : [...prev, labelId]));
                        return;
                      }
                      if (!name?.trim()) return;
                      const created = await createTimeEntryLabel({ name: name.trim() });
                      setLabelIds((prev) => (prev.includes(created.id) ? prev : [...prev, created.id]));
                    }}
                    onRemove={async (id) => {
                      setLabelIds((prev) => prev.filter((labelId) => labelId !== id));
                    }}
                    align="start"
                  />
                </div>
              </div>

              {/* Start time — read-only for running entries */}
              <div className="space-y-2">
                <Label>Start time</Label>
                <div className="flex items-center rounded-md border px-3 py-1.5 min-h-9">
                  <DateTimePicker
                    value={startIso}
                    onChange={(v) => setStartIso(v)}
                    placeholder="Pick start time"
                    required
                    disabled={isRunning}
                    align="start"
                    showSeconds
                  />
                </div>
                {isRunning && (
                  <p className="text-xs text-muted-foreground">
                    Start time cannot be changed while the timer is running.
                  </p>
                )}
              </div>

              {/* Stop time — hidden for running timers */}
              {!isRunning && (
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
              onClick={handleDeleteClick}
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
                disabled={isSaving}
                onClick={handleSave}
              >
                Save
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>

      {/* Delete confirmation dialog */}
      <TimeEntryDeleteDialog
        open={deleteDialogOpen}
        onCancel={() => setDeleteDialogOpen(false)}
        onConfirm={handleDeleteConfirm}
      />
    </>
  );
}
