import { useEffect, useState } from "react";
import type { HookJournalEvent, HookRun } from "../../core/hook-types";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import type { HookDirectory } from "./hook-target-picker";

export function HookTestHistory({ events = [], runs, onTest, directory = {} }: { events?: HookJournalEvent[]; runs: HookRun[]; onTest?: (eventID: string) => void; directory?: HookDirectory }) {
  const [selectedEventID, setSelectedEventID] = useState(events[0]?.id ?? "");
  useEffect(() => { if (!selectedEventID && events[0]) setSelectedEventID(events[0].id); }, [events, selectedEventID]);
  const selectedEvent = events.find((event) => event.id === selectedEventID) ?? events[0];
  return <section aria-label="Test and history" className="grid gap-4 p-6">
    <div><p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Test</p><h2 className="text-lg font-semibold">Run against a real past event</h2><p className="text-sm text-muted-foreground">Nothing is changed while testing.</p></div>
    {events.length > 0 ? <label className="grid gap-1.5 text-sm font-medium">Past event<Select value={selectedEvent?.id ?? null} onValueChange={(value) => value && setSelectedEventID(value)}><SelectTrigger aria-label="Past event"><SelectValue>{selectedEvent ? eventLabel(selectedEvent) : "Choose an event"}</SelectValue></SelectTrigger><SelectContent>{events.map((event) => <SelectItem key={event.id} value={event.id}>{eventLabel(event)}</SelectItem>)}</SelectContent></Select></label> : <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">No compatible events from the last 7 days yet. Save the Draft and let the workflow observe one first.</p>}
    <Button type="button" disabled={!selectedEvent} onClick={() => selectedEvent && onTest?.(selectedEvent.id)}>Run test with selected event</Button>
    {runs.map((run) => <article key={run.id} className="rounded-lg border bg-muted/30 p-4">
      <div className="flex justify-between gap-3"><strong>{run.source}</strong><span className="text-xs text-muted-foreground">{run.latency_ms} ms</span></div>
      <p className="mt-2 text-xs text-muted-foreground">Policy version {run.policy_version}</p>
      <p className="mt-1 text-xs text-muted-foreground">Source scope: {scopeLabel(run, directory)}</p>
      <ol className="mt-3 grid gap-2 text-sm">{run.matched_steps.map((step) => <li key={step}>✓ {step}</li>)}</ol>
      {run.matched_conditions.length > 0 && <div className="mt-3"><p className="text-xs font-medium">Matched conditions</p><ul className="mt-1 grid gap-1 text-sm">{run.matched_conditions.map((condition) => <li key={condition}>{condition}</li>)}</ul></div>}
      <p className="mt-3 font-medium">Would {run.decision}: {run.would_action}</p>
      <p className="mt-1 text-sm capitalize">Fail {run.fail_mode}</p>
      {run.remediation.length > 0 && <div className="mt-3"><p className="text-xs font-medium">Remediation</p><ul className="mt-1 grid gap-1 text-sm">{run.remediation.map((item) => <li key={item}>{item}</li>)}</ul></div>}
      <p className="mt-1 text-xs text-muted-foreground">Test run — no side effects</p>
    </article>)}
  </section>;
}

function eventLabel(event: HookJournalEvent) {
  const when = event.occurred_at ? new Date(event.occurred_at).toLocaleString() : "Unknown time";
  return `${event.event_type} · ${when}`;
}

function scopeLabel(run: HookRun, directory: HookDirectory) {
  if (!run.source_scope.id) return run.source_scope.kind === "workspace" ? "This workspace" : run.source_scope.kind;
  const options = directory[run.source_scope.kind as "agent" | "member" | "model" | "issue" | "project" | "workflow" | "session" | "squad" | "skill" | "artifact"] ?? [];
  return options.find((option) => option.value === run.source_scope.id)?.label ?? `${run.source_scope.kind} target`;
}
