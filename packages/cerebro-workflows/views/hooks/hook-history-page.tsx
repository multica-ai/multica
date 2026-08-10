"use client";

import { useMemo, useState } from "react";
import { ArrowLeft, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { PageHeader } from "@multica/views/layout/page-header";
import { HOOK_EVENT_OPTIONS, type HookRunSummary } from "../../core/hook-types";
import type { HookSummaryDirectory } from "../../core/hook-ux";

// Every hook's runs on one timeline. Before this, "what did the hooks do?"
// meant opening all 44 hooks one at a time.
export function HookHistoryPage({ runs, directory = {}, loading = false, error = false, onBack, onOpenHook }: { runs: HookRunSummary[]; directory?: HookSummaryDirectory; loading?: boolean; error?: boolean; onBack?: () => void; onOpenHook?: (familyId: string) => void }) {
  const [query, setQuery] = useState("");
  const [effect, setEffect] = useState<"all" | "stopped" | "enforced" | "dry_run">("all");
  const visible = useMemo(() => runs.filter((run) => {
    if (effect === "stopped" && !(run.enforced && (run.decision === "block" || run.decision === "require"))) return false;
    if (effect === "enforced" && !run.enforced) return false;
    if (effect === "dry_run" && run.enforced) return false;
    const needle = query.trim().toLowerCase();
    if (!needle) return true;
    return [run.hook_name, eventLabel(run.event_type), agentLabel(run, directory)].some((field) => field.toLowerCase().includes(needle));
  }), [runs, query, effect, directory]);

  return <div className="flex h-full flex-col bg-background">
    <PageHeader className="justify-between gap-2 px-3 sm:px-5">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {onBack && <Button variant="outline" size="icon-sm" aria-label="Back to hooks" onClick={onBack}><ArrowLeft className="size-4" /></Button>}
        <div className="min-w-0"><h1 className="text-sm font-semibold">Hook history</h1><p className="truncate text-[11px] text-muted-foreground">Every hook, newest first · what ran, on whose work, and what it changed</p></div>
      </div>
    </PageHeader>
    <div className="overflow-y-auto p-4 sm:p-6">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative min-w-0 flex-1 sm:max-w-xs"><Search className="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" /><Input className="pl-8" value={query} placeholder="Search hook, trigger, or agent" aria-label="Search history" onChange={(event) => setQuery(event.target.value)} /></div>
        <Select value={effect} onValueChange={(next) => next && setEffect(next as typeof effect)}>
          <SelectTrigger aria-label="Effect" className="h-8 w-auto min-w-40 text-xs"><SelectValue><span className="truncate">{EFFECT_OPTIONS.find((option) => option.value === effect)?.label}</span></SelectValue></SelectTrigger>
          <SelectContent>{EFFECT_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground">{visible.length} of {runs.length}</span>
      </div>
      {loading && <p className="p-6 text-center text-sm text-muted-foreground">Loading history…</p>}
      {error && <p role="alert" className="p-6 text-center text-sm text-destructive">The history could not be read.</p>}
      {!loading && !error && visible.length === 0 && <div className="rounded-xl border border-dashed bg-muted/20 p-8 text-center text-sm text-muted-foreground">No hook has run yet for this search.</div>}
      <ol className="grid gap-2">{visible.map((run) => <li key={run.id}>
        <button type="button" disabled={!run.family_id || !onOpenHook} className="grid w-full gap-2 rounded-lg border p-3 text-left transition enabled:hover:bg-muted/40 md:grid-cols-[minmax(12rem,1fr)_minmax(10rem,1fr)_minmax(9rem,auto)_auto] md:items-center" onClick={() => run.family_id && onOpenHook?.(run.family_id)}>
          <span className="min-w-0"><strong className="block truncate text-sm">{run.hook_name || "Unnamed hook"}</strong><span className="block truncate text-xs text-muted-foreground">{eventLabel(run.event_type)}</span></span>
          <span className="min-w-0 text-xs"><span className="block truncate">{agentLabel(run, directory)}</span><span className="block truncate text-muted-foreground">{issueLabel(run, directory)}</span></span>
          <span className="text-xs"><EffectBadge run={run} />{run.requirements.length > 0 && <span className="mt-1 block truncate text-muted-foreground" title={run.requirements.join(" ")}>{run.requirements[0]}</span>}</span>
          <span className="text-right text-xs text-muted-foreground">{run.created_at ? new Date(run.created_at).toLocaleString() : "Unknown time"}<span className="block">{run.latency_ms} ms{run.timed_out ? " · timed out" : ""}</span></span>
        </button>
      </li>)}</ol>
    </div>
  </div>;
}

const EFFECT_OPTIONS = [
  { value: "all", label: "Everything" },
  { value: "stopped", label: "Stopped or required something" },
  { value: "enforced", label: "Ran for real" },
  { value: "dry_run", label: "Dry run or test only" },
] as const;

function EffectBadge({ run }: { run: HookRunSummary }) {
  if (!run.enforced) return <span className="inline-flex w-fit rounded-full bg-muted px-2 py-0.5 text-[11px] font-semibold text-muted-foreground">Dry run · changed nothing</span>;
  const decision = run.decision;
  if (decision === "block") return <span className="inline-flex w-fit rounded-full bg-destructive/10 px-2 py-0.5 text-[11px] font-semibold text-destructive">Stopped the action</span>;
  if (decision === "require") return <span className="inline-flex w-fit rounded-full bg-warning/10 px-2 py-0.5 text-[11px] font-semibold text-warning">Required an outcome</span>;
  if (decision === "modify") return <span className="inline-flex w-fit rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-semibold text-primary">Modified the action</span>;
  return <span className="inline-flex w-fit rounded-full bg-success/10 px-2 py-0.5 text-[11px] font-semibold text-success">Let it continue</span>;
}

function eventLabel(eventType: string) {
  return HOOK_EVENT_OPTIONS.find((option) => option.value === eventType)?.label ?? eventType ?? "Unknown trigger";
}

// "Who did it" is the agent whose run the hook judged. Naming it is the whole
// point of a history a person can act on.
function agentLabel(run: HookRunSummary, directory: HookSummaryDirectory) {
  if (!run.agent_id) return "No agent on this event";
  return directory.agent?.find((option) => option.value === run.agent_id)?.label ?? "Unknown agent";
}

function issueLabel(run: HookRunSummary, directory: HookSummaryDirectory) {
  if (!run.issue_id) return "No issue";
  return directory.issue?.find((option) => option.value === run.issue_id)?.label ?? "Issue outside this list";
}
