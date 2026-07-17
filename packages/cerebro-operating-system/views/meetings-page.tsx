"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { meetingOptions, useUpdateMeeting } from "../core/queries";
import type { MeetingAgendaSection, MeetingCadenceUnit, MeetingConfig } from "../core/types";
import { SearchSelect } from "./search-select";

const cadenceOptions = ["manual", "day", "week", "month", "quarter"] as const;
const cadenceLabels: Record<MeetingCadenceUnit, string> = { manual: "Manual", day: "Daily", week: "Weekly", month: "Monthly", quarter: "Quarterly" };
const bindingOptions = [
  { value: "none", label: "Free text" },
  { value: "scorecard", label: "Scorecard" },
  { value: "goals", label: "Goals" },
  { value: "issues_list", label: "Issues list" },
];

function agendaFingerprint(config?: MeetingConfig) {
  return config ? `${config.note_type_id ?? ""}|${config.cadence_unit}|${config.cadence_count}|${config.agenda.map((item) => `${item.id}:${item.name}:${item.position}:${item.binding}`).join("|")}` : "";
}

export function MeetingsPage() {
  const wsId = useWorkspaceId();
  const meeting = useQuery(meetingOptions(wsId));
  const save = useUpdateMeeting(wsId);
  const [draft, setDraft] = useState<MeetingConfig | null>(null);

  useEffect(() => {
    if (meeting.data) setDraft(meeting.data);
    // The fingerprint changes only when the server state changes, not on draft edits.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agendaFingerprint(meeting.data)]);

  if (meeting.isLoading || !draft) return <div className="mx-auto max-w-4xl p-6 text-sm text-muted-foreground">Loading meeting settings…</div>;
  if (meeting.isError) return <div role="alert" className="mx-auto max-w-4xl p-6 text-sm text-destructive">Meeting settings could not be loaded.</div>;
  const config = draft;

  const noteTypeOptions = config.available_note_types.map((noteType) => ({ value: noteType.id, label: noteType.name, hint: `${cadenceLabels[noteType.cadence_unit]} · every ${noteType.cadence_count}` }));

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
    save.mutate({ note_type_id: config.note_type_id, cadence_unit: config.cadence_unit, cadence_count: Math.max(1, config.cadence_count), agenda: config.agenda.map((section, position) => ({ ...section, name: section.name.trim(), position })) });
  }

  return (
    <div className="mx-auto grid max-w-4xl gap-8 p-6">
      <div><h1 className="text-2xl font-semibold">{config.note_type_name || "Meetings"}</h1><p className="mt-1 text-sm text-muted-foreground">Choose the recurring note that represents this meeting and shape its agenda around the live operating-system data.</p></div>
      <section className="grid gap-4 rounded-xl border bg-card p-5">
        <h2 className="font-semibold">Meeting setup</h2>
        <div className="grid gap-4 md:grid-cols-3">
          <label className="grid gap-1 text-sm md:col-span-2">Recurring note type
            <SearchSelect label="Recurring note type" options={noteTypeOptions} value={config.note_type_id ?? ""} onChange={(value) => setDraft({ ...config, note_type_id: value || undefined })} clearLabel="No note type selected" placeholder="Choose a recurring note" />
          </label>
          <label className="grid gap-1 text-sm">Repeat every
            <div className="flex gap-2"><input aria-label="Cadence count" type="number" min={1} value={config.cadence_count} onChange={(event) => setDraft({ ...config, cadence_count: Number(event.target.value) || 1 })} className="h-10 w-20 rounded-md border bg-background px-3" /><SearchSelect label="Cadence" compact options={cadenceOptions.map((value) => ({ value, label: cadenceLabels[value] }))} value={config.cadence_unit} onChange={(value) => setDraft({ ...config, cadence_unit: value as MeetingCadenceUnit })} /></div>
          </label>
        </div>
      </section>
      <section className="grid gap-4 rounded-xl border bg-card p-5">
        <div className="flex items-center justify-between gap-3"><div><h2 className="font-semibold">Agenda</h2><p className="text-sm text-muted-foreground">Rename, reorder, add or remove sections. A binding adds live data to that part of the meeting.</p></div><button type="button" onClick={addAgendaSection} className="h-9 rounded-md border px-3 text-sm font-medium">+ Add section</button></div>
        <div className="grid gap-2">
          {config.agenda.map((section, index) => <div key={section.id} className="grid gap-2 rounded-lg border p-3 md:grid-cols-[auto_1fr_12rem_auto] md:items-center">
            <span className="text-sm font-medium text-muted-foreground">{index + 1}</span>
            <input aria-label={`Agenda section ${index + 1}`} value={section.name} onChange={(event) => updateAgenda(index, { name: event.target.value })} className="h-10 rounded-md border bg-background px-3 text-sm" />
            <SearchSelect label={`Binding for ${section.name}`} compact options={bindingOptions} value={section.binding} onChange={(value) => updateAgenda(index, { binding: value as MeetingAgendaSection["binding"] })} />
            <div className="flex items-center gap-2"><button type="button" aria-label={`Move ${section.name} up`} disabled={index === 0} onClick={() => moveAgendaSection(index, -1)} className="text-sm font-medium disabled:opacity-40">↑</button><button type="button" aria-label={`Move ${section.name} down`} disabled={index === config.agenda.length - 1} onClick={() => moveAgendaSection(index, 1)} className="text-sm font-medium disabled:opacity-40">↓</button><button type="button" onClick={() => removeAgendaSection(index)} className="text-sm font-medium text-destructive hover:underline">Remove</button></div>
          </div>)}
          {config.agenda.length === 0 && <p className="text-sm text-muted-foreground">No agenda sections yet.</p>}
        </div>
      </section>
      {save.isError && <p role="alert" className="text-sm text-destructive">Meeting settings could not be saved.</p>}
      <div className="flex justify-end"><button type="button" disabled={save.isPending} onClick={submit} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save meeting</button></div>
    </div>
  );
}
