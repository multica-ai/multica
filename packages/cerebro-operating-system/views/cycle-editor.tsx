"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";

import { useDeleteCycle, useSaveCycle } from "../core/queries";
import type { CycleInput, MeetingNoteType } from "../core/types";
import { SearchSelect, type SearchSelectOption } from "./search-select";

/**
 * Create a cycle, or change when an existing one meets (FIR-3589 item 6).
 *
 * A cycle IS a recurring Note, so this writes the Note's recurrence: how often
 * it repeats, which weekday it lands on, and — for a monthly rhythm — which
 * ordinal week of the month, so "the third Monday of every month" is expressible.
 * The Note itself (its plan and its content) is edited by opening the Note.
 */
const CADENCE_OPTIONS: SearchSelectOption[] = [
  { value: "week", label: "Every week" },
  { value: "month", label: "Every month" },
  { value: "quarter", label: "Every quarter" },
  { value: "day", label: "Every day" },
];

const WEEKDAY_OPTIONS: SearchSelectOption[] = [
  { value: "1", label: "Monday" },
  { value: "2", label: "Tuesday" },
  { value: "3", label: "Wednesday" },
  { value: "4", label: "Thursday" },
  { value: "5", label: "Friday" },
  { value: "6", label: "Saturday" },
  { value: "7", label: "Sunday" },
];

const WEEK_OF_MONTH_OPTIONS: SearchSelectOption[] = [
  { value: "1", label: "First" },
  { value: "2", label: "Second" },
  { value: "3", label: "Third" },
  { value: "4", label: "Fourth" },
  { value: "-1", label: "Last" },
];

type CadenceUnit = CycleInput["cadence_unit"];

function draftFrom(cycle?: MeetingNoteType): CycleInput {
  return {
    name: cycle?.name ?? "",
    cadence_unit: (cycle && cycle.cadence_unit !== "manual" ? cycle.cadence_unit : "week") as CadenceUnit,
    cadence_count: cycle?.cadence_count ?? 1,
    anchor_weekday: cycle?.anchor_weekday ?? null,
    anchor_week_of_month: cycle?.anchor_week_of_month ?? null,
    participants: cycle?.participants ?? [],
  };
}

export function CycleEditor({ cycle, open, onOpenChange }: {
  cycle?: MeetingNoteType;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const save = useSaveCycle(wsId);
  const remove = useDeleteCycle(wsId);
  const [draft, setDraft] = useState<CycleInput>(draftFrom(cycle));
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const participantOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "People" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];
  const participantValues = draft.participants.map((participant) => `${participant.type}:${participant.id}`);

  function setParticipants(values: string[]) {
    setDraft({ ...draft, participants: values.flatMap((value) => {
      const [type, id] = value.split(":");
      if (!id || (type !== "member" && type !== "agent")) return [];
      return [{ type, id }];
    }) });
  }

  // The month rhythm is the only one that carries an ordinal week; switching
  // away from it clears the setting so a stale "third" can never be stored.
  function setCadence(unit: CadenceUnit) {
    setDraft({ ...draft, cadence_unit: unit, anchor_week_of_month: unit === "month" ? draft.anchor_week_of_month : null });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-auto bottom-0 left-0 max-h-[85dvh] max-w-none translate-x-0 translate-y-0 overflow-y-auto rounded-b-none sm:top-1/2 sm:bottom-auto sm:left-1/2 sm:max-w-lg sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-xl">
        <DialogTitle className="text-base font-semibold">{cycle ? `Edit ${cycle.name}` : "New cycle"}</DialogTitle>
        <div className="grid gap-3">
          <label className="grid gap-1 text-sm font-medium">Name
            <input aria-label="Cycle name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="e.g. Business Review" className="min-h-11 rounded-md border bg-background px-3 font-normal" />
          </label>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1 text-sm font-medium">Repeats
              <SearchSelect label="Repeats" fieldLabel="Repeats" compact options={CADENCE_OPTIONS} value={draft.cadence_unit} onChange={(value) => setCadence(value as CadenceUnit)} placeholder="Choose" />
            </div>
            <label className="grid gap-1 text-sm font-medium">Every
              <input aria-label="Repeat interval" type="number" min={1} max={52} value={draft.cadence_count} onChange={(event) => setDraft({ ...draft, cadence_count: Math.max(1, Number(event.target.value) || 1) })} className="min-h-11 rounded-md border bg-background px-3 font-normal" />
            </label>
          </div>

          {draft.cadence_unit === "month" && (
            <div className="grid gap-1 text-sm font-medium">Week of the month
              <SearchSelect label="Week of the month" fieldLabel="Week" compact options={WEEK_OF_MONTH_OPTIONS} value={draft.anchor_week_of_month != null ? String(draft.anchor_week_of_month) : ""} onChange={(value) => setDraft({ ...draft, anchor_week_of_month: value ? Number(value) : null })} clearLabel="Same date each month" placeholder="Same date each month" />
            </div>
          )}

          {(draft.cadence_unit === "week" || draft.cadence_unit === "month") && (
            <div className="grid gap-1 text-sm font-medium">Day
              <SearchSelect label="Day" fieldLabel="Day" compact options={WEEKDAY_OPTIONS} value={draft.anchor_weekday != null ? String(draft.anchor_weekday) : ""} onChange={(value) => setDraft({ ...draft, anchor_weekday: value ? Number(value) : null })} clearLabel="No fixed day" placeholder="No fixed day" />
            </div>
          )}

          <div className="grid gap-1 text-sm font-medium">Who attends
            <SearchSelect label="Who attends" fieldLabel="Attends" compact multiple options={participantOptions} values={participantValues} onValuesChange={setParticipants} placeholder="Nobody yet" />
          </div>

          <div className="flex flex-wrap justify-end gap-2 pt-1">
            <button type="button" onClick={() => onOpenChange(false)} className="min-h-11 rounded-md border px-3 text-sm font-medium">Cancel</button>
            {cycle && (confirmingDelete ? (
              <>
                <button type="button" aria-label={`Confirm delete ${cycle.name}`} onClick={() => remove.mutate(cycle.id, { onSuccess: () => onOpenChange(false) })} className="min-h-11 rounded-md bg-destructive px-3 text-sm font-medium text-destructive-foreground">Delete cycle</button>
                <button type="button" aria-label="Cancel delete" onClick={() => setConfirmingDelete(false)} className="min-h-11 rounded-md border px-3 text-sm font-medium">Keep</button>
              </>
            ) : (
              <button type="button" aria-label={`Delete ${cycle.name}`} onClick={() => setConfirmingDelete(true)} className="min-h-11 rounded-md border px-3 text-sm font-medium text-destructive">Delete</button>
            ))}
            <button
              type="button"
              disabled={save.isPending || !draft.name.trim()}
              onClick={() => save.mutate({ id: cycle?.id, input: { ...draft, name: draft.name.trim() } }, { onSuccess: () => onOpenChange(false) })}
              className="min-h-11 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50"
            >
              {cycle ? "Save cycle" : "Create cycle"}
            </button>
          </div>

          {(save.isError || remove.isError) && <p role="alert" className="text-sm text-destructive">The cycle could not be saved.</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
