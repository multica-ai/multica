import { useEffect, useState } from "react";
import { HOOK_EVENT_OPTIONS, type HookJournalEvent, type HookRun } from "../../core/hook-types";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import type { HookDirectory } from "./hook-target-picker";

export function HookTestHistory({ events = [], runs, onTest, directory = {} }: { events?: HookJournalEvent[]; runs: HookRun[]; onTest?: (eventID: string) => void; directory?: HookDirectory }) {
  const [selectedEventID, setSelectedEventID] = useState(events[0]?.id ?? "");
  useEffect(() => { if (!selectedEventID && events[0]) setSelectedEventID(events[0].id); }, [events, selectedEventID]);
  const selectedEvent = events.find((event) => event.id === selectedEventID) ?? events[0];
  return <section aria-label="Test and history" className="grid gap-4 p-6">
    <div><p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Test</p><h2 className="text-lg font-semibold">Run against a real past event</h2><p className="text-sm text-muted-foreground">Nothing is changed while testing.</p></div>
    {events.length > 0 ? <label className="grid gap-1.5 text-sm font-medium">Past event<Select value={selectedEvent?.id ?? null} onValueChange={(value) => value && setSelectedEventID(value)}><SelectTrigger aria-label="Past event"><SelectValue>{selectedEvent ? eventOptionLabel(selectedEvent) : "Choose an event"}</SelectValue></SelectTrigger><SelectContent>{events.map((event) => <SelectItem key={event.id} value={event.id}>{eventOptionLabel(event)}</SelectItem>)}</SelectContent></Select></label> : <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">No compatible events from the last 7 days yet. Save the Draft and let the workflow observe one first.</p>}
    <Button type="button" disabled={!selectedEvent} onClick={() => selectedEvent && onTest?.(selectedEvent.id)}>Run test with selected event</Button>

    <div className="mt-2"><h2 className="text-lg font-semibold">History</h2><p className="text-sm text-muted-foreground">Newest first. Each line says who was affected and what this hook did.</p></div>
    {runs.length === 0 && <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">This hook has not run yet.</p>}
    <ol className="grid gap-2">{runs.map((run) => <li key={run.id}><HookRunRow run={run} directory={directory} /></li>)}</ol>
  </section>;
}

// The old rows printed matched steps, scope ids and raw condition strings, and
// never said who the run belonged to. A person reading history wants: when, who,
// what happened, and what would fix it.
function HookRunRow({ run, directory }: { run: HookRun; directory: HookDirectory }) {
  const [open, setOpen] = useState(false);
  const event = (run.event ?? {}) as Record<string, unknown>;
  const agentID = typeof event.agent_id === "string" ? event.agent_id : "";
  const issueID = typeof event.issue_id === "string" ? event.issue_id : "";
  const eventType = typeof event.event_type === "string" ? event.event_type : "";
  return <article className="rounded-lg border bg-muted/20 p-3">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <span className="min-w-0"><strong className="text-sm">{eventLabel(eventType)}</strong><span className="ml-2 text-xs text-muted-foreground">{nameFor(directory, "agent", agentID, "No agent on this event")}</span></span>
      <span className="text-xs text-muted-foreground">{run.created_at ? new Date(run.created_at).toLocaleString() : "Unknown time"} · {run.latency_ms} ms</span>
    </div>
    <p className="mt-1 text-sm">{outcomeSentence(run)}</p>
    <p className="mt-1 text-xs">{run.dry_run
      ? <span className="inline-flex w-fit rounded-full bg-muted px-2 py-0.5 font-semibold text-muted-foreground">Dry run · changed nothing</span>
      : <span className="inline-flex w-fit rounded-full bg-success/10 px-2 py-0.5 font-semibold text-success">Ran for real</span>}</p>
    <p className="mt-1 text-xs text-muted-foreground">{nameFor(directory, "issue", issueID, "No issue")} · version {run.policy_version}</p>
    {run.remediation.length > 0 && <p className="mt-2 text-sm"><strong className="font-medium">To satisfy it: </strong>{run.remediation.join(" ")}</p>}
    <button type="button" className="mt-2 text-xs text-muted-foreground underline" onClick={() => setOpen((current) => !current)}>{open ? "Hide details" : "Show details"}</button>
    {open && <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
      {run.matched_conditions.length > 0 && <p><strong className="font-medium text-foreground">Matched: </strong>{run.matched_conditions.join("; ")}</p>}
      <p><strong className="font-medium text-foreground">If the check cannot run: </strong>{({ open: "continue", closed: "stop", warn: "continue and log" })[run.fail_mode]}</p>
    </div>}
  </article>;
}

function outcomeSentence(run: HookRun) {
  const action = run.would_action && run.would_action !== "No action" ? ` Action: ${run.would_action}.` : "";
  const decision = ({ allow: "Let it continue.", block: "Stopped the action.", modify: "Modified the action.", require: "Required an outcome." })[run.decision];
  return `${run.dry_run ? "Would have: " : ""}${decision}${action}`;
}

function eventLabel(eventType: string) {
  return HOOK_EVENT_OPTIONS.find((option) => option.value === eventType)?.label ?? eventType ?? "Unknown trigger";
}

function nameFor(directory: HookDirectory, kind: "agent" | "issue", id: string, fallback: string) {
  if (!id) return fallback;
  return directory[kind]?.find((option) => option.value === id)?.label ?? `Unknown ${kind}`;
}

function eventOptionLabel(event: HookJournalEvent) {
  const when = event.occurred_at ? new Date(event.occurred_at).toLocaleString() : "Unknown time";
  return `${eventLabel(event.event_type)} · ${when}`;
}
