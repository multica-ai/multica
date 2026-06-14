"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
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
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
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

const DEFAULT_TEMPLATE = `## Business Review – {{måned}} {{år}}

### Tal
- Omsætning vs. budget:
- EBITDA:
- Trafik / konvertering:

### Hvad gik godt

### Hvad gik skidt

### Beslutninger & næste skridt
- [ ]
`;

const MODE_LABELS: Record<RecurrenceMode, string> = {
  running_doc: "Fortløbende dokument",
  new_note: "Ny note pr. periode",
};

const CADENCE_LABELS: Record<CadenceUnit, string> = {
  manual: "Manuel",
  week: "Hver uge",
  month: "Hver måned",
  quarter: "Hvert kvartal",
};

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
  };
}

const NO_FOLDER = "__none__";

/**
 * Admin panel for note types (TECH-3511). Lists the workspace's note types and
 * lets an admin create / edit / delete one and run it on demand ("Start ny nu").
 * Designed to render inside a Sheet from the Documents file manager.
 */
export function NoteTypesPanel() {
  const wsId = useWorkspaceId();
  const { data: noteTypes, isLoading } = useNoteTypes();
  const { data: folders } = useQuery(artifactFoldersOptions(wsId));
  const create = useCreateNoteType();
  const update = useUpdateNoteType();
  const remove = useDeleteNoteType();
  const run = useRunNoteType();

  const [draft, setDraft] = React.useState<DraftState | null>(null);

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
      cadence_count: draft.cadence_count,
      target_folder_id: draft.target_folder_id,
      enabled: draft.enabled,
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
            <ArrowLeft className="mr-1 size-4" /> Tilbage
          </Button>
          <h3 className="text-sm font-semibold">
            {draft.id ? "Rediger note type" : "Ny note type"}
          </h3>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="nt-name">Navn</Label>
          <Input
            id="nt-name"
            value={draft.name}
            placeholder="Fx Månedlig Business Review"
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="nt-template">Skabelon (indsættes hver gang)</Label>
          <Textarea
            id="nt-template"
            value={draft.template_body}
            onChange={(e) => setDraft({ ...draft, template_body: e.target.value })}
            className="min-h-[220px] font-mono text-xs"
          />
          <p className="text-xs text-muted-foreground">
            Pladsholdere: {"{{måned}}"}, {"{{år}}"}, {"{{kvartal}}"}, {"{{uge}}"},{" "}
            {"{{dato}}"} — de udfyldes automatisk.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label>Hvordan gentages den?</Label>
          <Select
            value={draft.recurrence_mode}
            onValueChange={(v) =>
              setDraft({ ...draft, recurrence_mode: v as RecurrenceMode })
            }
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="running_doc">{MODE_LABELS.running_doc}</SelectItem>
              <SelectItem value="new_note">{MODE_LABELS.new_note}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {draft.recurrence_mode === "running_doc"
              ? "Ny skabelon lægges øverst i ét og samme dokument."
              : "Der oprettes en ny note i mappen hver periode."}
          </p>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1.5">
            <Label>Hyppighed</Label>
            <Select
              value={draft.cadence_unit}
              onValueChange={(v) =>
                setDraft({ ...draft, cadence_unit: v as CadenceUnit })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="manual">{CADENCE_LABELS.manual}</SelectItem>
                <SelectItem value="week">{CADENCE_LABELS.week}</SelectItem>
                <SelectItem value="month">{CADENCE_LABELS.month}</SelectItem>
                <SelectItem value="quarter">{CADENCE_LABELS.quarter}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>Mappe</Label>
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
                <SelectValue placeholder="Ingen mappe" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_FOLDER}>Ingen mappe</SelectItem>
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
            <div className="text-sm font-medium">Aktiv</div>
            <div className="text-xs text-muted-foreground">
              Slået fra: kører ikke automatisk.
            </div>
          </div>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(c) => setDraft({ ...draft, enabled: c })}
          />
        </div>

        <div className="flex items-center justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={() => setDraft(null)} disabled={isSaving}>
            Annuller
          </Button>
          <Button
            onClick={handleSave}
            disabled={isSaving || draft.name.trim().length === 0}
          >
            {isSaving ? "Gemmer…" : "Gem note type"}
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
          Skabeloner til fx business reviews — deles af hele workspacet.
        </p>
        <Button size="sm" onClick={() => setDraft(emptyDraft())}>
          <Plus className="mr-1 size-4" /> Ny note type
        </Button>
      </div>

      {isLoading && (
        <p className="py-8 text-center text-sm text-muted-foreground">Indlæser…</p>
      )}

      {!isLoading && (noteTypes ?? []).length === 0 && (
        <div className="flex flex-col items-center gap-2 rounded-md border border-dashed py-10 text-center">
          <FileText className="size-6 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">
            Ingen note typer endnu. Opret den første.
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
                  inaktiv
                </Badge>
              )}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <Badge variant="secondary" className="text-[10px] font-normal">
                {MODE_LABELS[nt.recurrence_mode]}
              </Badge>
              <Badge variant="outline" className="text-[10px] font-normal">
                {CADENCE_LABELS[nt.cadence_unit]}
              </Badge>
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
              title="Start ny nu"
              onClick={() => run.mutate(nt.id)}
              disabled={run.isPending}
            >
              <Play className="size-4 sm:mr-1" />
              <span className="hidden sm:inline">Start ny</span>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              title="Rediger"
              onClick={() => setDraft(draftFrom(nt))}
            >
              <Pencil className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              title="Slet"
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
