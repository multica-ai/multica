"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Play, ArrowLeft, FileText } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import {
  artifactFoldersOptions,
  useNoteTypes,
  useCreateNoteType,
  useUpdateNoteType,
  useDeleteNoteType,
  useRunNoteType,
} from "@multica/cerebro-artifacts/core";
import type {
  CadenceUnit,
  NoteType,
  NoteTypeWriteInput,
  RecurrenceMode,
} from "@multica/cerebro-artifacts/core";

const DEFAULT_TEMPLATE = `## Business Review - {{måned}} {{år}}

### Numbers
- Revenue vs. budget:
- EBITDA:
- Traffic / conversion:

### What went well

### What went poorly

### Decisions & next steps
- [ ]
`;

const MODE_LABELS: Record<RecurrenceMode, string> = {
  running_doc: "Running document",
  new_note: "New note each period",
};

// Short unit labels; the "Every N <unit>" sentence is composed in the editor.
const CADENCE_LABELS: Record<CadenceUnit, string> = {
  manual: "Manual",
  day: "Day",
  week: "Week",
  month: "Month",
  quarter: "Quarter",
};

// Plural unit words used in the list badge, e.g. "Every 2 weeks".
const CADENCE_BADGE: Record<CadenceUnit, string> = {
  manual: "Manual",
  day: "day",
  week: "week",
  month: "month",
  quarter: "quarter",
};

const WEEKDAY_LABELS: Record<number, string> = {
  1: "Monday",
  2: "Tuesday",
  3: "Wednesday",
  4: "Thursday",
  5: "Friday",
  6: "Saturday",
  7: "Sunday",
};

const WEEKDAY_OPTIONS = Object.entries(WEEKDAY_LABELS).map(([value, label]) => ({
  value: Number(value),
  label,
}));

function cadenceBadge(unit: CadenceUnit, count: number, anchorWeekday?: number | null): string {
  if (unit === "manual") return "Manual";
  if (unit === "week" && anchorWeekday) {
    const interval = count <= 1 ? "week" : `${count} weeks`;
    return `Every ${interval} on ${WEEKDAY_LABELS[anchorWeekday] ?? "selected weekday"}`;
  }
  if (count <= 1) return `Every ${CADENCE_BADGE[unit]}`;
  return `Every ${count} ${CADENCE_BADGE[unit]}s`;
}

function cadenceHelperText(
  unit: CadenceUnit,
  count: number,
  anchorWeekday?: number | null,
): string {
  if (unit === "manual") return "Runs only when you click “Run now”.";
  const badge = cadenceBadge(unit, count, anchorWeekday);
  if (unit === "week" && anchorWeekday) {
    return `Runs automatically ${badge.charAt(0).toLowerCase()}${badge.slice(1)}.`;
  }
  return `Runs automatically ${badge.toLowerCase()}.`;
}

interface DraftState {
  id: string | null;
  name: string;
  icon: string;
  template_body: string;
  recurrence_mode: RecurrenceMode;
  cadence_unit: CadenceUnit;
  cadence_count: number;
  target_folder_id: string | null;
  enabled: boolean;
  numbering_enabled: boolean;
  next_number: number;
  anchor_weekday: number | null;
}

function emptyDraft(): DraftState {
  return {
    id: null,
    name: "",
    icon: "",
    template_body: DEFAULT_TEMPLATE,
    recurrence_mode: "running_doc",
    cadence_unit: "month",
    cadence_count: 1,
    target_folder_id: null,
    enabled: true,
    numbering_enabled: false,
    next_number: 1,
    anchor_weekday: null,
  };
}

function draftFrom(nt: NoteType): DraftState {
  return {
    id: nt.id,
    name: nt.name,
    icon: nt.icon,
    template_body: nt.template_body,
    recurrence_mode: nt.recurrence_mode,
    cadence_unit: nt.cadence_unit,
    cadence_count: nt.cadence_count,
    target_folder_id: nt.target_folder_id,
    enabled: nt.enabled,
    numbering_enabled: nt.numbering_enabled,
    next_number: nt.next_number,
    anchor_weekday: nt.anchor_weekday,
  };
}

const NO_FOLDER = "__none__";
const NO_ANCHOR_WEEKDAY = "__creation_day__";

/**
 * Admin panel for note types (TECH-3511). Lists the workspace's note types and
 * lets an admin create / edit / delete one and run it on demand ("Run now").
 * Designed to render inside a Sheet from the Documents file manager.
 *
 * onOpenNote (FIR-1460): when the panel is hosted by the Notes surface itself,
 * the parent passes a handler that selects the materialised note in place. The
 * default (Documents file manager) navigates to the Notes route instead.
 */
export function NoteTypesPanel({
  onOpenNote,
}: {
  onOpenNote?: (artifactId: string) => void;
} = {}) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { data: noteTypes, isLoading } = useNoteTypes();
  const { data: folders } = useQuery(
    artifactFoldersOptions(wsId, { kind: "note" }),
  );
  const create = useCreateNoteType();
  const update = useUpdateNoteType();
  const remove = useDeleteNoteType();
  const run = useRunNoteType();

  const [draft, setDraft] = React.useState<DraftState | null>(null);

  // "Run now": materialise the note type now, tell the user what happened, and
  // open the note. A materialised note type is a Notes-feature note (kind='note'
  // registered via UpsertNote, see note_types/apply.go) — so it opens on the
  // Notes surface, not in the Documents file manager (TECH-3780). created=false
  // means this period was already materialised (idempotent no-op) — we still
  // open the existing note.
  const handleRun = React.useCallback(
    async (id: string) => {
      try {
        const result = await run.mutateAsync(id);
        if (!result || !result.artifact_id) {
          toast.error("Could not start the note. Try again.");
          return;
        }
        toast.success(
          result.created
            ? "New note created."
            : "Already exists for this period - opening it.",
        );
        // FIR-1460: when hosted on the Notes surface, select the note in place
        // (a route push to the page we're already on doesn't re-open it).
        // Otherwise navigate to the Notes route from the Documents file manager.
        if (onOpenNote) {
          onOpenNote(result.artifact_id);
        } else {
          router.push(
            `${wsPaths.notes()}?note=${encodeURIComponent(result.artifact_id)}`,
          );
        }
      } catch {
        toast.error("Could not start the note. Try again.");
      }
    },
    [run, router, wsPaths, onOpenNote],
  );

  const folderName = React.useCallback(
    (id: string | null) => folders?.find((f) => f.id === id)?.name ?? null,
    [folders],
  );

  const handleSave = async () => {
    if (!draft || draft.name.trim().length === 0) return;
    const payload: NoteTypeWriteInput = {
      name: draft.name.trim(),
      icon: draft.icon,
      template_body: draft.template_body,
      recurrence_mode: draft.recurrence_mode,
      cadence_unit: draft.cadence_unit,
      cadence_count: Math.max(1, draft.cadence_count),
      target_folder_id: draft.target_folder_id,
      enabled: draft.enabled,
      numbering_enabled: draft.numbering_enabled,
      next_number: Math.max(1, draft.next_number),
      anchor_weekday: draft.cadence_unit === "week" ? draft.anchor_weekday : null,
    };
    if (draft.id) {
      await update.mutateAsync({ id: draft.id, data: payload });
    } else {
      await create.mutateAsync(payload);
    }
    setDraft(null);
  };

  const isSaving = create.isPending || update.isPending;

  // -------------------------------------------------------------------------
  // Editor view
  // -------------------------------------------------------------------------
  if (draft) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
            <ArrowLeft className="mr-1 size-4" /> Back
          </Button>
          <h3 className="text-sm font-semibold">
            {draft.id ? "Edit note type" : "New note type"}
          </h3>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="nt-name">Name</Label>
          <Input
            id="nt-name"
            value={draft.name}
            placeholder="E.g. Monthly Business Review"
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="nt-template">Template (inserted every time)</Label>
          <Textarea
            id="nt-template"
            value={draft.template_body}
            onChange={(e) => setDraft({ ...draft, template_body: e.target.value })}
            className="min-h-[220px] font-mono text-xs"
          />
          <p className="text-xs text-muted-foreground">
            Pladsholdere: {"{{måned}}"}, {"{{år}}"}, {"{{kvartal}}"}, {"{{uge}}"},{" "}
            {"{{dato}}"}, {"{{periode}}"}, {"{{nummer}}"} — de udfyldes automatisk.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label>How does it repeat?</Label>
          <Select
            value={draft.recurrence_mode}
            onValueChange={(v) =>
              setDraft({ ...draft, recurrence_mode: v as RecurrenceMode })
            }
          >
            <SelectTrigger>
              <span>{MODE_LABELS[draft.recurrence_mode]}</span>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="running_doc">{MODE_LABELS.running_doc}</SelectItem>
              <SelectItem value="new_note">{MODE_LABELS.new_note}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {draft.recurrence_mode === "running_doc"
              ? "A new template is added at the top of the same document."
              : "A new note is created in the folder each period."}
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label>Frequency</Label>
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Every</span>
            <Input
              type="number"
              min={1}
              value={draft.cadence_count}
              disabled={draft.cadence_unit === "manual"}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  cadence_count: Math.max(1, Number(e.target.value) || 1),
                })
              }
              className="w-20"
            />
            <Select
              value={draft.cadence_unit}
              onValueChange={(v) => {
                const cadenceUnit = v as CadenceUnit;
                setDraft({
                  ...draft,
                  cadence_unit: cadenceUnit,
                  anchor_weekday: cadenceUnit === "week" ? draft.anchor_weekday : null,
                });
              }}
            >
              <SelectTrigger className="flex-1">
                <span>{CADENCE_LABELS[draft.cadence_unit]}</span>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="manual">{CADENCE_LABELS.manual}</SelectItem>
                <SelectItem value="day">{CADENCE_LABELS.day}</SelectItem>
                <SelectItem value="week">{CADENCE_LABELS.week}</SelectItem>
                <SelectItem value="month">{CADENCE_LABELS.month}</SelectItem>
                <SelectItem value="quarter">{CADENCE_LABELS.quarter}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <p className="text-xs text-muted-foreground">
            {cadenceHelperText(
              draft.cadence_unit,
              draft.cadence_count,
              draft.anchor_weekday,
            )}
          </p>
        </div>

        {draft.cadence_unit === "week" && (
          <div className="flex flex-col gap-1.5">
            <Label>Anchor weekday</Label>
            <Select
              value={draft.anchor_weekday ? String(draft.anchor_weekday) : NO_ANCHOR_WEEKDAY}
              onValueChange={(v) =>
                setDraft({
                  ...draft,
                  anchor_weekday: v === NO_ANCHOR_WEEKDAY ? null : Number(v),
                })
              }
            >
              <SelectTrigger>
                <span>
                  {draft.anchor_weekday
                    ? WEEKDAY_LABELS[draft.anchor_weekday]
                    : "Creation day"}
                </span>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_ANCHOR_WEEKDAY}>Creation day</SelectItem>
                {WEEKDAY_OPTIONS.map((day) => (
                  <SelectItem key={day.value} value={String(day.value)}>
                    {day.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Weekly notes stay on this weekday, even if the note type was created on another day.
            </p>
          </div>
        )}

        <div className="grid grid-cols-1 gap-3">
          <div className="flex flex-col gap-1.5">
            <Label>Folder</Label>
            <Select
              value={draft.target_folder_id ?? NO_FOLDER}
              onValueChange={(v) =>
                setDraft({
                  ...draft,
                  target_folder_id: v === NO_FOLDER ? null : v,
                })
              }
            >
              <SelectTrigger>
                <span>{draft.target_folder_id ? folderName(draft.target_folder_id) : "None"}</span>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_FOLDER}>None</SelectItem>
                {(folders ?? []).map((f) => (
                  <SelectItem key={f.id} value={f.id}>
                    {f.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="flex items-center justify-between rounded-md border px-3 py-2">
          <div>
            <div className="text-sm font-medium">Active</div>
            <div className="text-xs text-muted-foreground">
              Off: does not run automatically.
            </div>
          </div>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(c) => setDraft({ ...draft, enabled: c })}
          />
        </div>

        <div className="flex flex-col gap-2 rounded-md border px-3 py-2">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-medium">Auto-number</div>
              <div className="text-xs text-muted-foreground">
                Each new note gets a sequential number (#1, #2, #3 ...).
              </div>
            </div>
            <Switch
              checked={draft.numbering_enabled}
              onCheckedChange={(c) => setDraft({ ...draft, numbering_enabled: c })}
            />
          </div>
          {draft.numbering_enabled && (
            <div className="flex items-center gap-2">
              <Label htmlFor="nt-next-number" className="text-xs">
                Next number
              </Label>
              <Input
                id="nt-next-number"
                type="number"
                min={1}
                value={draft.next_number}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    next_number: Math.max(1, Number(e.target.value) || 1),
                  })
                }
                className="w-24"
              />
              <span className="text-xs text-muted-foreground">
                Brug {"{{nummer}}"} i skabelonen for at vise det.
              </span>
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={() => setDraft(null)} disabled={isSaving}>
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={isSaving || draft.name.trim().length === 0}
          >
            {isSaving ? "Saving..." : "Save note type"}
          </Button>
        </div>
      </div>
    );
  }

  // -------------------------------------------------------------------------
  // List view
  // -------------------------------------------------------------------------
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          Templates for business reviews and recurring notes, shared by the whole workspace.
        </p>
        <Button size="sm" onClick={() => setDraft(emptyDraft())}>
          <Plus className="mr-1 size-4" /> New note type
        </Button>
      </div>

      {isLoading && (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading...</p>
      )}

      {!isLoading && (noteTypes ?? []).length === 0 && (
        <div className="flex flex-col items-center gap-2 rounded-md border border-dashed py-10 text-center">
          <FileText className="size-6 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">
            No note types yet. Create the first one.
          </p>
        </div>
      )}

      {(noteTypes ?? []).map((nt) => (
        <div
          key={nt.id}
          className="flex items-start gap-3 rounded-md border px-3 py-2.5"
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium">{nt.name}</span>
              {!nt.enabled && (
                <Badge variant="outline" className="text-[10px]">
                  inactive
                </Badge>
              )}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <Badge variant="secondary" className="text-[10px] font-normal">
                {MODE_LABELS[nt.recurrence_mode]}
              </Badge>
              <Badge variant="outline" className="text-[10px] font-normal">
                {cadenceBadge(nt.cadence_unit, nt.cadence_count, nt.anchor_weekday)}
              </Badge>
              {nt.numbering_enabled && (
                <Badge variant="outline" className="text-[10px] font-normal">
                  Numbered
                </Badge>
              )}
              {folderName(nt.target_folder_id) && (
                <span className="text-[11px] text-muted-foreground">
                  📁 {folderName(nt.target_folder_id)}
                </span>
              )}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-0.5">
            <Button
              variant="ghost"
              size="sm"
              title="Run now"
              onClick={() => handleRun(nt.id)}
              disabled={run.isPending}
            >
              <Play className="size-4 sm:mr-1" />
              <span className="hidden sm:inline">Run now</span>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              title="Edit"
              onClick={() => setDraft(draftFrom(nt))}
            >
              <Pencil className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              title="Delete"
              className="text-destructive hover:text-destructive"
              onClick={() => remove.mutate(nt.id)}
              disabled={remove.isPending}
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
