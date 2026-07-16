import type { HookRun } from "../../core/hook-types";
import { Button } from "@multica/ui/components/ui/button";

export function HookTestHistory({ runs, onTest }: { runs: HookRun[]; onTest?: () => void }) {
  return <section aria-label="Test and history" className="grid gap-4 p-6">
    <div><p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Test</p><h2 className="text-lg font-semibold">Run against a real past event</h2><p className="text-sm text-muted-foreground">Nothing is changed while testing.</p></div>
    <Button type="button" onClick={onTest}>Run test</Button>
    {runs.map((run) => <article key={run.id} className="rounded-lg border bg-muted/30 p-4">
      <div className="flex justify-between gap-3"><strong>{run.source}</strong><span className="text-xs text-muted-foreground">{run.latency_ms} ms</span></div>
      <p className="mt-2 text-xs text-muted-foreground">Policy {run.policy_id} · version {run.policy_version}</p>
      <p className="mt-1 text-xs text-muted-foreground">Source scope: {run.source_scope.kind}{run.source_scope.id ? ` ${run.source_scope.id}` : ""}</p>
      <ol className="mt-3 grid gap-2 text-sm">{run.matched_steps.map((step) => <li key={step}>✓ {step}</li>)}</ol>
      {run.matched_conditions.length > 0 && <div className="mt-3"><p className="text-xs font-medium">Matched conditions</p><ul className="mt-1 grid gap-1 text-sm">{run.matched_conditions.map((condition) => <li key={condition}>{condition}</li>)}</ul></div>}
      <p className="mt-3 font-medium">Would {run.decision}: {run.would_action}</p>
      <p className="mt-1 text-sm capitalize">Fail {run.fail_mode}</p>
      {run.remediation.length > 0 && <div className="mt-3"><p className="text-xs font-medium">Remediation</p><ul className="mt-1 grid gap-1 text-sm">{run.remediation.map((item) => <li key={item}>{item}</li>)}</ul></div>}
      <p className="mt-1 text-xs text-muted-foreground">Test run — no side effects</p>
    </article>)}
  </section>;
}
