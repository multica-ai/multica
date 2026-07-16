import { useEffect, useState } from "react";
import type { HookRun } from "../../core/hook-types";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import type { HookDirectory } from "./hook-target-picker";

export function HookTestHistory({ runs, onTest, directory = {} }: { runs: HookRun[]; onTest?: (event?: Record<string, unknown>) => void; directory?: HookDirectory }) {
  const [selectedRunID, setSelectedRunID] = useState(runs[0]?.id ?? "");
  useEffect(() => { if (!selectedRunID && runs[0]) setSelectedRunID(runs[0].id); }, [runs, selectedRunID]);
  const selectedRun = runs.find((run) => run.id === selectedRunID) ?? runs[0];
  return <section aria-label="Test and history" className="grid gap-4 p-6">
    <div><p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Test</p><h2 className="text-lg font-semibold">Run against a real past event</h2><p className="text-sm text-muted-foreground">Nothing is changed while testing.</p></div>
    {runs.length > 0 ? <label className="grid gap-1.5 text-sm font-medium">Past event<Select value={selectedRun?.id ?? null} onValueChange={(value) => value && setSelectedRunID(value)}><SelectTrigger aria-label="Past event"><SelectValue>{selectedRun ? runLabel(selectedRun) : "Choose an event"}</SelectValue></SelectTrigger><SelectContent>{runs.map((run) => <SelectItem key={run.id} value={run.id}>{runLabel(run)}</SelectItem>)}</SelectContent></Select></label> : <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">No past events yet. Save the hook and let it observe an event in Dry run first.</p>}
    <Button type="button" disabled={!selectedRun} onClick={() => onTest?.(selectedRun?.event)}>Run test with selected event</Button>
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

function runLabel(run: HookRun) {
  const when = run.created_at ? new Date(run.created_at).toLocaleString() : "Unknown time";
  return `${run.source.split(" · ")[0]} · ${when}`;
}

function scopeLabel(run: HookRun, directory: HookDirectory) {
  if (!run.source_scope.id) return run.source_scope.kind === "workspace" ? "This workspace" : run.source_scope.kind;
  const options = directory[run.source_scope.kind as "agent" | "member" | "model" | "issue" | "project" | "workflow" | "session" | "squad" | "skill" | "artifact"] ?? [];
  return options.find((option) => option.value === run.source_scope.id)?.label ?? `${run.source_scope.kind} target`;
}
