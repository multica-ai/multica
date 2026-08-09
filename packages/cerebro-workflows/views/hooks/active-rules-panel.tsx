"use client";

import type { ActiveHookRule } from "../../core/hook-api";
import { HookTargetPicker, type HookDirectory } from "./hook-target-picker";

export function ActiveRulesPanel({ directory, agentId, issueId, onAgentChange, onIssueChange, rules, loading = false, error = false }: {
  directory: HookDirectory;
  agentId: string;
  issueId: string;
  onAgentChange: (value: string) => void;
  onIssueChange: (value: string) => void;
  rules: ActiveHookRule[];
  loading?: boolean;
  error?: boolean;
}) {
  const ready = Boolean(agentId && issueId);
  return <section className="mb-5 rounded-lg border bg-muted/20 p-4" aria-labelledby="applicable-rules-title">
    <div className="mb-3">
      <h2 id="applicable-rules-title" className="font-semibold">Applicable rules</h2>
      <p className="text-xs text-muted-foreground">Choose an agent and issue to see the live contracts for that run.</p>
    </div>
    <div className="grid gap-3 sm:grid-cols-2">
      <label className="grid gap-1.5 text-sm font-medium">Agent<HookTargetPicker label="Agent" value={agentId} options={directory.agent ?? []} onChange={onAgentChange} /></label>
      <label className="grid gap-1.5 text-sm font-medium">Issue<HookTargetPicker label="Issue" value={issueId} options={directory.issue ?? []} onSearch={directory.searchIssues} onChange={onIssueChange} /></label>
    </div>
    {loading && <p className="mt-4 text-sm text-muted-foreground">Loading applicable rules…</p>}
    {!loading && error && <p role="alert" className="mt-4 text-sm text-destructive">Applicable rules could not be loaded.</p>}
    {!loading && !error && ready && rules.length === 0 && <p className="mt-4 text-sm text-muted-foreground">No live Workflow hook contracts apply.</p>}
    {!loading && rules.length > 0 && <div className="mt-4 grid gap-2">{rules.map((rule) => <article key={rule.id} className="rounded-md border bg-background p-3">
      <div className="flex flex-wrap items-start justify-between gap-2"><strong className="text-sm">{rule.name}</strong><span className="text-xs text-muted-foreground">{rule.scope.kind}</span></div>
      <p className="mt-2 text-sm">{rule.contract_rule}</p>
      <p className="mt-1 text-sm text-muted-foreground"><strong className="text-foreground">How to satisfy it:</strong> {rule.contract_satisfy}</p>
      <p className="mt-2 text-xs text-muted-foreground">{rule.events.join(" · ")}</p>
    </article>)}</div>}
  </section>;
}
