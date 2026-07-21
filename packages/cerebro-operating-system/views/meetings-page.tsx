"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { ChevronDown, ChevronUp, Plus, Settings2, Trash2 } from "lucide-react";
import { meetingOptions, settingsOptions, useUpdateMeeting } from "../core/queries";
import type { MeetingAgendaSection, MeetingCadenceUnit, MeetingConfig } from "../core/types";
import { SearchSelect } from "./search-select";
import { CycleTimeline } from "./cycle-timeline";

const bindingOptions = [
  { value: "none", label: "Free text" },
  { value: "scorecard", label: "Scorecard" },
  { value: "goals", label: "Goals" },
  { value: "issues_list", label: "Issues list" },
];

function agendaFingerprint(config?: MeetingConfig) {
  return config ? `${config.note_type_id ?? ""}|${config.cadence_unit}|${config.cadence_count}|${config.agenda.map((item) => `${item.id}:${item.name}:${item.position}:${item.binding}`).join("|")}` : "";
}

function cadenceSummary(unit: MeetingCadenceUnit, count: number) {
  if (unit === "manual") return "No recurring timing";
  const labels: Record<Exclude<MeetingCadenceUnit, "manual">, [string, string]> = {
    day: ["day", "days"], week: ["week", "weeks"], month: ["month", "months"], quarter: ["quarter", "quarters"],
  };
  const [singular, plural] = labels[unit];
  return count === 1 ? `Every ${singular}` : `Every ${count} ${plural}`;
}

export function MeetingsPage({ renderCurrentNote }: { renderCurrentNote?: (noteId: string, openSetup: () => void) => React.ReactNode } = {}) {
  const wsId = useWorkspaceId();
  const meeting = useQuery(meetingOptions(wsId));
  const settings = useQuery(settingsOptions(wsId));
  const save = useUpdateMeeting(wsId);
  const [draft, setDraft] = useState<MeetingConfig | null>(null);
  const [showSetup, setShowSetup] = useState(false);

  useEffect(() => {
    if (meeting.data) setDraft(meeting.data);
    // The fingerprint changes only when the server state changes, not on draft edits.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agendaFingerprint(meeting.data)]);

  if (meeting.isError) return <div role="alert" className="mx-auto max-w-4xl p-6 text-sm text-destructive">Meeting settings could not be loaded.</div>;
  if (meeting.isLoading || !draft) return <div className="mx-auto max-w-4xl p-6 text-sm text-muted-foreground">Loading meeting settings…</div>;
  const config = draft;

  const availableNoteTypes = config.available_note_types.filter((noteType) => noteType.enabled && noteType.cadence_unit !== "manual");
  const selectedNoteType = availableNoteTypes.find((noteType) => noteType.id === config.note_type_id);
  const noteTypeOptions = availableNoteTypes.map((noteType) => ({ value: noteType.id, label: noteType.name, hint: cadenceSummary(noteType.cadence_unit, noteType.cadence_count) }));
  const cadenceUnit = selectedNoteType?.cadence_unit ?? (config.note_type_id ? config.cadence_unit : "manual");
  const cadenceCount = selectedNoteType?.cadence_count ?? (config.note_type_id ? config.cadence_count : 1);
  const timingSourceName = selectedNoteType?.name ?? config.note_type_name;
  const currentNoteId = selectedNoteType?.current_note_id ?? config.current_note_id;
  const cyclesLabel = settings.data?.terminology?.meetings ?? "Cycles";

  function updateAgenda(index: number, changes: Partial<MeetingAgendaSection>) {
    setDraft({ ...config, agenda: config.agenda.map((section, current) => current === index ? { ...section, ...changes } : section) });
  }

  function addAgendaSection() {
    const position = config.agenda.length;
    setDraft({ ...config, agenda: [...config.agenda, { id: `custom-${Date.now()}`, name: "New section", position, binding: "none" }] });
  }

  function removeAgendaSection(index: number) {
    setDraft({ ...config, agenda: config.agenda.filter((_, current) => current !== index).map((section, position) => ({ ...section, position })) });
  }

  function moveAgendaSection(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= config.agenda.length) return;
    const agenda = [...config.agenda];
    const currentSection = agenda[index];
    const targetSection = agenda[target];
    if (!currentSection || !targetSection) return;
    agenda[index] = targetSection;
    agenda[target] = currentSection;
    setDraft({ ...config, agenda: agenda.map((section, position) => ({ ...section, position })) });
  }

  function submit() {
    save.mutate({ note_type_id: config.note_type_id, cadence_unit: cadenceUnit, cadence_count: cadenceCount, agenda: config.agenda.map((section, position) => ({ ...section, name: section.name.trim(), position })) });
  }

  return (
    <div className="mx-auto grid h-full max-w-5xl gap-6 overflow-y-auto p-4 sm:p-6">
      <div className="flex flex-wrap items-end justify-between gap-3"><div><p className="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Operating cadence</p><h1 className="mt-1 text-2xl font-semibold">{cyclesLabel}</h1><p className="mt-1 text-sm text-muted-foreground">Run the current recurring review Note with its issues, comments, mentions, and agent work in context.</p></div><button type="button" aria-expanded={showSetup} onClick={() => setShowSetup((value) => !value)} className="flex min-h-11 items-center gap-2 rounded-md border bg-background px-4 text-sm font-medium"><Settings2 aria-hidden className="size-4" />{showSetup ? "Close setup" : "Cycle setup"}</button></div>
      {!showSetup && cadenceUnit !== "manual" && <CycleTimeline cadenceUnit={cadenceUnit} cadenceCount={cadenceCount} label={cyclesLabel} />}
      {!showSetup && currentNoteId && renderCurrentNote?.(currentNoteId, () => setShowSetup(true))}
      {!showSetup && currentNoteId && !renderCurrentNote && <div role="alert" className="rounded-xl border border-destructive/30 p-8 text-center text-sm text-destructive">The current review Note is unavailable. Cycle setup is still available.</div>}
      {!showSetup && !currentNoteId && <div className="rounded-xl border border-dashed bg-card p-8 text-center"><h2 className="font-semibold">No current review Note</h2><p className="mt-1 text-sm text-muted-foreground">Choose a recurring Note type in Cycle setup. The Note remains the single source of truth and is never created implicitly here.</p><button type="button" onClick={() => setShowSetup(true)} className="mt-4 min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Open Cycle setup</button></div>}
      {showSetup && <>
      <section className="grid gap-4 rounded-xl border bg-card p-5">
        <h2 className="font-semibold">Cycle setup</h2>
        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_16rem]">
          <label className="grid gap-1 text-sm">Recurring note type
            <SearchSelect label="Recurring note type" options={noteTypeOptions} value={config.note_type_id ?? ""} onChange={(value) => {
              const noteType = availableNoteTypes.find((candidate) => candidate.id === value);
              setDraft({
                ...config,
                note_type_id: noteType?.id,
                note_type_name: noteType?.name,
                cadence_unit: noteType?.cadence_unit ?? "manual",
                cadence_count: noteType?.cadence_count ?? 1,
              });
            }} clearLabel="No note type selected" placeholder="Choose a recurring note" />
          </label>
          <div className="rounded-lg border bg-muted/30 px-3 py-2.5">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Timing</p>
            <p className="mt-0.5 text-sm font-semibold">{cadenceSummary(cadenceUnit, cadenceCount)}</p>
            <p className="mt-1 text-xs text-muted-foreground">{timingSourceName ? `Timing is controlled by ${timingSourceName} in recurring Notes.` : "Choose a recurring Note to control this meeting's timing."}</p>
          </div>
        </div>
      </section>
      <section className="grid gap-4 rounded-xl border bg-card p-5">
        <div className="flex items-center justify-between gap-3"><div><h2 className="font-semibold">Agenda</h2><p className="text-sm text-muted-foreground">Rename, reorder, add or remove sections. A binding adds live data to that part of the cycle.</p></div><button type="button" onClick={addAgendaSection} className="flex min-h-11 items-center gap-2 rounded-md border px-3 text-sm font-medium"><Plus aria-hidden className="size-4" />Add section</button></div>
        <div className="grid gap-2">
          {config.agenda.map((section, index) => <div key={section.id} className="grid gap-2 rounded-lg border p-3 md:grid-cols-[auto_1fr_12rem_auto] md:items-center">
            <span className="text-sm font-medium text-muted-foreground">{index + 1}</span>
            <input aria-label={`Agenda section ${index + 1}`} value={section.name} onChange={(event) => updateAgenda(index, { name: event.target.value })} className="min-h-11 rounded-md border bg-background px-3 text-sm" />
            <SearchSelect label={`Binding for ${section.name}`} compact options={bindingOptions} value={section.binding} onChange={(value) => updateAgenda(index, { binding: value as MeetingAgendaSection["binding"] })} />
            <div className="flex items-center gap-1"><button type="button" aria-label={`Move ${section.name} up`} disabled={index === 0} onClick={() => moveAgendaSection(index, -1)} className="grid size-11 place-items-center rounded-md border disabled:opacity-40"><ChevronUp aria-hidden className="size-4" /></button><button type="button" aria-label={`Move ${section.name} down`} disabled={index === config.agenda.length - 1} onClick={() => moveAgendaSection(index, 1)} className="grid size-11 place-items-center rounded-md border disabled:opacity-40"><ChevronDown aria-hidden className="size-4" /></button><button type="button" aria-label={`Remove ${section.name}`} onClick={() => removeAgendaSection(index)} className="grid size-11 place-items-center rounded-md border text-destructive"><Trash2 aria-hidden className="size-4" /></button></div>
          </div>)}
          {config.agenda.length === 0 && <p className="text-sm text-muted-foreground">No agenda sections yet.</p>}
        </div>
      </section>
      {save.isError && <p role="alert" className="text-sm text-destructive">Meeting settings could not be saved.</p>}
      <div className="flex justify-end"><button type="button" disabled={save.isPending} onClick={submit} className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save Cycle setup</button></div>
      </>}
    </div>
  );
}
