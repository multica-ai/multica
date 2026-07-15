import type { HookRun } from "../../core/hook-types";

export function HookTestHistory({ runs, onTest }: { runs: HookRun[]; onTest?: () => void }) {
  return <section aria-label="Test and history" className="grid gap-4 p-6">
    <div><p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Test</p><h2 className="text-lg font-semibold">Run against a real past event</h2><p className="text-sm text-muted-foreground">Nothing is changed while testing.</p></div>
    <button type="button" onClick={onTest} className="rounded-md bg-[#5b5bd6] px-3 py-2 text-sm font-semibold text-white">Run test</button>
    {runs.map((run) => <article key={run.id} className="rounded-lg border bg-muted/30 p-4">
      <div className="flex justify-between gap-3"><strong>{run.source}</strong><span className="text-xs text-muted-foreground">{run.latency_ms} ms</span></div>
      <ol className="mt-3 grid gap-2 text-sm">{run.matched_steps.map((step) => <li key={step}>✓ {step}</li>)}</ol>
      <p className="mt-3 font-medium">Would {run.decision}: {run.would_action}</p>
      <p className="mt-1 text-xs text-muted-foreground">Test run — no side effects</p>
    </article>)}
  </section>;
}
