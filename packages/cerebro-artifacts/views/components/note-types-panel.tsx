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
  NoteTypeParticipant,
  NoteTypeWriteInput,
  RecurrenceMode,
} from "@multica/cerebro-artifacts/core";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";

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

// Ordinal week-of-month labels; -1 means the last occurrence in the month.
const WEEK_OF_MONTH_LABELS: Record<number, string> = {
  1: "First",
  2: "Second",
  3: "Third",
  4: "Fourth",
  5: "Fifth",
  [-1]: "Last",
};

const WEEK_OF_MONTH_OPTIONS = [1, 2, 3, 4, 5, -1].map((value) => ({
  value,
  label: WEEK_OF_MONTH_LABELS[value] ?? String(value),
}));

// "the 3rd Monday" style fragment for a monthly week-of-month anchor.
function monthlyWeekPhrase(weekOfMonth: number, anchorWeekday: number): string {
  const ordinal = WEEK_OF_MONTH_LABELS[weekOfMonth] ?? "selected";
  const weekday = WEEKDAY_LABELS[anchorWeekday] ?? "selected weekday";
  return `the ${ordinal.toLowerCase()} ${weekday}`;
}

function hasMonthlyWeekAnchor(
  unit: CadenceUnit,
  anchorWeekday?: number | null,
  weekOfMonth?: number | null,
): boolean {
  return unit === "month" && !!anchorWeekday && weekOfMonth != null;
}

function cadenceBadge(
  unit: CadenceUnit,
  count: number,
  anchorWeekday?: number | null,
  weekOfMonth?: number | null,
): string {
  if (unit === "manual") return "Manual";
  if (unit === "week" && anchorWeekday) {
    const interval = count <= 1 ? "week" : `${count} weeks`;
    return `Every ${interval} on ${WEEKDAY_LABELS[anchorWeekday] ?? "selected weekday"}`;
  }
  if (hasMonthlyWeekAnchor(unit, anchorWeekday, weekOfMonth)) {
    const interval = count <= 1 ? "month" : `${count} months`;
    return `Every ${interval} on ${monthlyWeekPhrase(weekOfMonth as number, anchorWeekday as number)}`;
  }
  if (count <= 1) return `Every ${CADENCE_BADGE[unit]}`;
  return `Every ${count} ${CADENCE_BADGE[unit]}s`;
}

function cadenceHelperText(
  unit: CadenceUnit,
  count: number,
  anchorWeekday?: number | null,
  weekOfMonth?: number | null,
): string {
  if (unit === "manual") return "Runs only when you click “Run now”.";
  const badge = cadenceBadge(unit, count, anchorWeekday, weekOfMonth);
  if (
    (unit === "week" && anchorWeekday) ||
    hasMonthlyWeekAnchor(unit, anchorWeekday, weekOfMonth)
  ) {
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
  anchor_week_of_month: number | null;
  author_codes: boolean;
  participants: NoteTypeParticipant[];
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
    anchor_week_of_month: null,
    author_codes: false,
    participants: [],
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
    anchor_week_of_month: nt.anchor_week_of_month,
    author_codes: nt.author_codes,
    participants: nt.participants ?? [],
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
  const { data: members } = useQuery(memberListOptions(wsId));
  const { data: agents } = useQuery(agentListOptions(wsId));
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
      anchor_weekday:
        draft.cadence_unit === "week" || draft.cadence_unit === "month"
          ? draft.anchor_weekday
          : null,
      anchor_week_of_month:
        draft.cadence_unit === "month" ? draft.anchor_week_of_month : null,
      author_codes: draft.author_codes,
      participants: draft.participants,
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
    const participantDraft = draft;
    const memberList = members ?? [];
    const agentList = agents ?? [];
    const participantName = (participant: NoteTypeParticipant): string => {
      const pool = participant.type === "agent" ? agentList : memberList;
      return pool.find((entry) => entry.id === participant.id)?.name ?? "Unknown";
    };
    const participantKey = (participant: NoteTypeParticipant) => `${participant.type}:${participant.id}`;
    const selectedKeys = new Set(participantDraft.participants.map(participantKey));
    const participantOptions = [
      ...memberList.map((member) => ({ value: `member:${member.id}`, label: member.name, group: "People" })),
      ...agentList.map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
    ].filter((option) => !selectedKeys.has(option.value));
    const addParticipant = (value: string | null) => {
      const [type, id] = (value ?? "").split(":");
      if ((type !== "member" && type !== "agent") || !id) return;
      setDraft({ ...participantDraft, participants: [...participantDraft.participants, { type, id }] });
    };
    const removeParticipant = (participant: NoteTypeParticipant) => {
      setDraft({ ...participantDraft, participants: participantDraft.participants.filter((entry) => participantKey(entry) !== participantKey(participant)) });
    };
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
                  anchor_weekday:
                    cadenceUnit === "week" || cadenceUnit === "month"
                      ? draft.anchor_weekday
                      : null,
                  anchor_week_of_month:
                    cadenceUnit === "month" ? draft.anchor_week_of_month : null,
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
              draft.anchor_week_of_month,
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

        {draft.cadence_unit === "month" && (
          <div className="flex flex-col gap-1.5">
            <Label>Day of the month</Label>
            <div className="flex items-center gap-2">
              <Select
                value={
                  draft.anchor_week_of_month != null
                    ? String(draft.anchor_week_of_month)
                    : NO_ANCHOR_WEEKDAY
                }
                onValueChange={(v) =>
                  setDraft({
                    ...draft,
                    anchor_week_of_month: v === NO_ANCHOR_WEEKDAY ? null : Number(v),
                    // Selecting an ordinal needs a weekday; default to Monday.
                    anchor_weekday:
                      v === NO_ANCHOR_WEEKDAY ? null : (draft.anchor_weekday ?? 1),
                  })
                }
              >
                <SelectTrigger className="flex-1">
                  <span>
                    {draft.anchor_week_of_month != null
                      ? WEEK_OF_MONTH_LABELS[draft.anchor_week_of_month]
                      : "Same date each month"}
                  </span>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NO_ANCHOR_WEEKDAY}>Same date each month</SelectItem>
                  {WEEK_OF_MONTH_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={String(option.value)}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={draft.anchor_weekday ? String(draft.anchor_weekday) : "1"}
                disabled={draft.anchor_week_of_month == null}
                onValueChange={(v) => setDraft({ ...draft, anchor_weekday: Number(v) })}
              >
                <SelectTrigger className="flex-1">
                  <span>{WEEKDAY_LABELS[draft.anchor_weekday ?? 1]}</span>
                </SelectTrigger>
                <SelectContent>
                  {WEEKDAY_OPTIONS.map((day) => (
                    <SelectItem key={day.value} value={String(day.value)}>
                      {day.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <p className="text-xs text-muted-foreground">
              {draft.anchor_week_of_month != null && draft.anchor_weekday
                ? `Runs on ${monthlyWeekPhrase(draft.anchor_week_of_month, draft.anchor_weekday)} of each month.`
                : "Pick an ordinal (e.g. Third) and a weekday to run on the 3rd Monday of every month, or keep the same date each month."}
            </p>
          </div>
        )}

        <div className="flex flex-col gap-1.5">
          <Label>Participants</Label>
          {participantDraft.participants.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {participantDraft.participants.map((participant) => (
                <Badge key={participantKey(participant)} variant="secondary" className="gap-1">
                  {participantName(participant)}
                  <button
                    type="button"
                    aria-label={`Remove ${participantName(participant)}`}
                    onClick={() => removeParticipant(participant)}
                    className="ml-0.5 rounded-sm text-muted-foreground hover:text-foreground"
                  >
                    <Trash2 className="size-3" />
                  </button>
                </Badge>
              ))}
            </div>
          )}
          <Select value="" onValueChange={addParticipant}>
            <SelectTrigger aria-label="Add participant">
              <span className="text-muted-foreground">Add a person or agent…</span>
            </SelectTrigger>
            <SelectContent>
              {participantOptions.length === 0 ? (
                <SelectItem value="__none__" disabled>
                  Everyone is already added
                </SelectItem>
              ) : (
                participantOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">Who attends this cycle. They travel with every note it creates.</p>
        </div>

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

        {/* FIR-2810: notes materialised from this type start with author codes
            on — every line a person writes is stamped with their member code. */}
        <div className="flex items-center justify-between rounded-md border px-3 py-2">
          <div>
            <div className="text-sm font-medium">Author codes</div>
            <div className="text-xs text-muted-foreground">
              New notes stamp each writer's member code (e.g. JEH) on every
              line they write.
            </div>
          </div>
          <Switch
            checked={draft.author_codes}
            onCheckedChange={(c) => setDraft({ ...draft, author_codes: c })}
          />
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
                {cadenceBadge(nt.cadence_unit, nt.cadence_count, nt.anchor_weekday, nt.anchor_week_of_month)}
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
