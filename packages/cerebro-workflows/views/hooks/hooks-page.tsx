"use client";

import { useMemo, useState, type ReactNode } from "react";
import { AlertTriangle, History, LockKeyhole, Plus, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { PageHeader } from "@multica/views/layout/page-header";
import type { HookPartialError } from "../../core/hook-api";
import type { HookLifecycleState, HookMode, WorkflowHook } from "../../core/hook-types";
import {
  describeHook, enforcedDecisionSummary, filterHooks, hookDraftState, hookFilterOptions, hookStateKey,
  EMPTY_HOOK_FILTERS, HOOK_STATE_FILTERS, HOOK_STATE_MEANING,
  type HookFilterState, type HookStateFilter, type HookSummaryDirectory,
} from "../../core/hook-ux";

export function HooksPage({ hooks, partialErrors = [], directory = {}, activeRulesPanel, onOpenHook, onOpenHistory }: { hooks: WorkflowHook[]; partialErrors?: HookPartialError[]; directory?: HookSummaryDirectory; activeRulesPanel?: ReactNode; onOpenHook: (id?: string) => void; onOpenHistory?: () => void }) {
  const [filters, setFilters] = useState<HookFilterState>(EMPTY_HOOK_FILTERS);
  const options = useMemo(() => hookFilterOptions(hooks), [hooks]);
  const visible = useMemo(() => filterHooks(hooks, filters, directory), [hooks, filters, directory]);
  const counts = useMemo(() => hooks.reduce<Record<string, number>>((total, hook) => ({ ...total, [hookStateKey(hook)]: (total[hookStateKey(hook)] ?? 0) + 1 }), {}), [hooks]);
  const needsAttention = useMemo(() => hooks.filter((hook) => !hookDraftState(hook).publishable).length, [hooks]);
  const update = (patch: Partial<HookFilterState>) => setFilters((current) => ({ ...current, ...patch }));

  return <div className="flex h-full flex-col bg-background">
    <PageHeader className="justify-between gap-2 px-3 sm:px-5">
      <div className="min-w-0 flex-1"><h1 className="text-sm font-semibold">Hooks</h1><p className="truncate text-[11px] text-muted-foreground">Workflows · trigger, filter, decide, and act</p></div>
      <div className="flex shrink-0 items-center gap-2">
        {onOpenHistory && <Button variant="outline" size="sm" onClick={onOpenHistory}><History className="size-4" />History</Button>}
        <Button size="sm" onClick={() => onOpenHook()}><Plus className="size-4" />New hook</Button>
      </div>
    </PageHeader>
    <div className="overflow-y-auto p-4 sm:p-6">
      {hooks.length > 0 && <HookLegend counts={counts} needsAttention={needsAttention} />}
      {activeRulesPanel}
      {partialErrors.length > 0 && <div role="alert" className="mb-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{partialErrors.length} {partialErrors.length === 1 ? "Hook could" : "Hooks could"} not be displayed. The remaining Hooks are still available.</div>}
      {hooks.length > 0 && <div className="mb-3 grid gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-0 flex-1 sm:max-w-xs"><Search className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" /><Input className="pl-8" value={filters.query} placeholder="Search hooks" aria-label="Search hooks" onChange={(event) => update({ query: event.target.value })} /></div>
          <div className="flex flex-wrap items-center gap-1">{HOOK_STATE_FILTERS.map((filter) => <button type="button" key={filter.value} aria-pressed={filters.state === filter.value} className={`rounded-full border px-3 py-1 text-xs font-medium transition ${filters.state === filter.value ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:bg-muted/50"}`} onClick={() => update({ state: filter.value as HookStateFilter })}>{filter.label}</button>)}</div>
          <span className="text-xs text-muted-foreground">{visible.length} of {hooks.length}</span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <FilterSelect label="Trigger" allLabel="Any trigger" value={filters.trigger} options={options.trigger} onChange={(trigger) => update({ trigger })} />
          <FilterSelect label="Decision" allLabel="Any decision" value={filters.decision} options={options.decision} onChange={(decision) => update({ decision })} />
          <FilterSelect label="Applies to" allLabel="Any scope" value={filters.scope} options={options.scope} onChange={(scope) => update({ scope })} />
          <FilterSelect label="Action" allLabel="Any action" value={filters.action} options={options.action} onChange={(action) => update({ action })} />
          <button type="button" aria-pressed={filters.attention} className={`rounded-full border px-3 py-1 text-xs font-medium transition ${filters.attention ? "border-warning bg-warning/10 text-warning" : "text-muted-foreground hover:bg-muted/50"}`} onClick={() => update({ attention: !filters.attention })}>Needs attention{needsAttention > 0 ? ` (${needsAttention})` : ""}</button>
          <Button variant="ghost" size="sm" className="text-xs" onClick={() => setFilters(EMPTY_HOOK_FILTERS)}>Clear filters</Button>
        </div>
      </div>}
      <div className="grid gap-2">{visible.map((hook) => <HookRow key={hook.family_id ?? hook.id ?? hook.name} hook={hook} directory={directory} onOpen={() => onOpenHook(hook.family_id ?? hook.id)} />)}</div>
      {hooks.length > 0 && visible.length === 0 && <div className="rounded-xl border border-dashed bg-muted/20 p-8 text-center text-sm text-muted-foreground">No hook matches this search or filter.</div>}
      {hooks.length === 0 && <div className="rounded-xl border border-dashed bg-muted/20 p-8 text-center"><div className="mx-auto flex size-11 items-center justify-center rounded-xl bg-primary/10 text-primary"><Plus className="size-5" /></div><h2 className="mt-4 font-semibold">Turn workflow moments into safe automation</h2><p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">A Hook watches an event, applies it to named work, checks conditions, makes a decision, and runs an action. Start from a plain-language recipe and verify it in Dry run before publishing.</p><Button className="mt-4" onClick={() => onOpenHook()}><Plus className="size-4" />Choose a recipe</Button></div>}
    </div>
  </div>;
}

// The page used to explain nothing, and the six words a hook can be in — Draft,
// Publish, Dry run, Enforced, Off, Managed — were left for the reader to guess.
function HookLegend({ counts, needsAttention }: { counts: Record<string, number>; needsAttention: number }) {
  return <section aria-label="What the states mean" className="mb-4 rounded-lg border bg-muted/20 p-4">
    <h2 className="text-sm font-semibold">What these hooks are doing to your runs</h2>
    <dl className="mt-2 grid gap-2 sm:grid-cols-2">
      {(["enforce", "managed", "dry_run", "off"] as const).map((state) => <div key={state} className="flex gap-2 text-xs">
        <dt className="shrink-0 font-semibold text-foreground">{HOOK_STATE_FILTERS.find((filter) => filter.value === state)?.label} <span className="font-normal text-muted-foreground">({counts[state] ?? 0})</span></dt>
        <dd className="min-w-0 text-muted-foreground">{HOOK_STATE_MEANING[state]}</dd>
      </div>)}
    </dl>
    <p className="mt-3 border-t pt-3 text-xs text-muted-foreground">
      <strong className="text-foreground">Draft and Publish.</strong> Editing a live hook does not change it. The edit is saved as a Draft that changes nothing until someone presses <strong className="text-foreground">Publish</strong>, and the live version keeps enforcing until then. A new hook must be observed in Dry run before it can be published.
      {needsAttention > 0 && <> <span className="text-warning">{needsAttention} {needsAttention === 1 ? "hook has a draft that" : "hooks have drafts that"} cannot be published yet.</span></>}
    </p>
  </section>;
}

function HookRow({ hook, directory, onOpen }: { hook: WorkflowHook; directory: HookSummaryDirectory; onOpen: () => void }) {
  const draft = hookDraftState(hook);
  // When a draft sits on top of a live version the body of this row IS the
  // draft, so the summary must say so instead of reading as what is running.
  const summary = describeHook(hook, directory);
  return <button type="button" className="grid w-full gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/40 md:grid-cols-[minmax(15rem,1.4fr)_minmax(16rem,1fr)_minmax(8rem,auto)_auto] md:items-center" onClick={onOpen}>
    <span className="min-w-0"><strong className="block truncate">{hook.name || "Untitled hook"}</strong><span className="block truncate text-xs text-foreground">{hook.contract_rule || hook.description || "No rule provided"}</span><span className="block truncate text-xs text-muted-foreground">{hook.contract_satisfy || "No satisfaction guidance"}</span></span>
    <span className="min-w-0">
      <span className="mb-1 inline-flex w-fit items-center rounded border px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">{enforcedDecisionSummary(hook)}</span>
      <span className="block truncate text-xs" title={summary}>{draft.overLive && <span className="text-muted-foreground">Draft (not live): </span>}{summary}</span>
    </span>
    <span className="grid gap-0.5 text-xs"><span className="text-success">{hook.pass_count_7d} passed</span><span className="text-warning">{hook.block_count_7d} blocked</span><span className="text-muted-foreground">{hook.last_run_at ? new Date(hook.last_run_at).toLocaleString() : "Never"}</span></span>
    <span className="grid justify-items-start gap-1 md:justify-items-end">
      <HookState mode={hook.mode} lifecycle={hook.family_id ? hook.lifecycle.state : undefined} />
      {draft.present && !draft.publishable && <span title={draft.blocker} className="inline-flex w-fit items-center gap-1 rounded-full bg-warning/10 px-2 py-1 text-[11px] font-semibold text-warning"><AlertTriangle className="size-3" />{draft.overLive ? "Draft cannot be published" : "Cannot be published yet"}</span>}
    </span>
  </button>;
}

function FilterSelect({ label, allLabel, value, options, onChange }: { label: string; allLabel: string; value: string; options: ReadonlyArray<{ value: string; label: string }>; onChange: (value: string) => void }) {
  const all = [{ value: "all", label: allLabel }, ...options];
  return <Select value={value} onValueChange={(next) => next && onChange(next)}>
    <SelectTrigger aria-label={label} className="h-8 w-auto min-w-36 max-w-56 text-xs"><SelectValue><span className="truncate">{all.find((option) => option.value === value)?.label ?? allLabel}</span></SelectValue></SelectTrigger>
    <SelectContent>{all.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
  </Select>;
}

function HookState({ mode, lifecycle }: { mode: HookMode; lifecycle?: HookLifecycleState }) {
  const state = lifecycle ?? mode;
  const config = {
    off: ["Off", "bg-muted text-muted-foreground"],
    off_with_draft: ["Off · Draft changes", "bg-muted text-muted-foreground"],
    dry_run: ["Dry run", "bg-primary/10 text-primary"],
    draft: ["Draft", "bg-primary/10 text-primary"],
    enforce: ["Enforced", "bg-success/10 text-success"],
    live: ["Enforced", "bg-success/10 text-success"],
    live_with_draft: ["Enforced · Draft changes", "bg-success/10 text-success"],
    managed: ["Managed", "bg-secondary text-secondary-foreground"],
  }[state];
  return <span className={`inline-flex w-fit items-center gap-1 rounded-full px-2 py-1 text-xs font-semibold ${config[1]}`}>{state === "managed" && <LockKeyhole className="size-3" />}{config[0]}</span>;
}
