"use client";

import { LockKeyhole, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "@multica/views/layout/page-header";
import type { HookPartialError } from "../../core/hook-api";
import type { HookLifecycleState, HookMode, WorkflowHook } from "../../core/hook-types";
import { describeHook, type HookSummaryDirectory } from "../../core/hook-ux";

export function HooksPage({ hooks, partialErrors = [], directory = {}, onOpenHook }: { hooks: WorkflowHook[]; partialErrors?: HookPartialError[]; directory?: HookSummaryDirectory; onOpenHook: (id?: string) => void }) {
  return <div className="flex h-full flex-col bg-background">
    <PageHeader className="justify-between gap-2 px-3 sm:px-5"><div className="min-w-0 flex-1"><h1 className="text-sm font-semibold">Hooks</h1><p className="truncate text-[11px] text-muted-foreground">Workflows · trigger, filter, decide, and act</p></div><Button className="shrink-0" size="sm" onClick={() => onOpenHook()}><Plus className="size-4" />New hook</Button></PageHeader>
    <div className="overflow-y-auto p-4 sm:p-6">{partialErrors.length > 0 && <div role="alert" className="mb-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{partialErrors.length} {partialErrors.length === 1 ? "Hook could" : "Hooks could"} not be displayed. The remaining Hooks are still available.</div>}<div className="grid gap-2">{hooks.map((hook) => <button type="button" key={hook.family_id ?? hook.id ?? hook.name} className="grid w-full gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/40 md:grid-cols-[minmax(12rem,1.2fr)_minmax(16rem,1fr)_minmax(8rem,auto)_auto] md:items-center" onClick={() => onOpenHook(hook.family_id ?? hook.id)}>
      <span className="min-w-0"><strong className="block truncate">{hook.name || "Untitled hook"}</strong><span className="block truncate text-xs text-muted-foreground">{hook.description || "No description"}</span></span>
      <span className="truncate text-xs" title={describeHook(hook, directory)}>{describeHook(hook, directory)}</span>
      <span className="text-xs text-muted-foreground">{hook.last_run_at ? new Date(hook.last_run_at).toLocaleString() : "Never"}</span>
      <HookState mode={hook.mode} lifecycle={hook.family_id ? hook.lifecycle.state : undefined} />
    </button>)}</div>{hooks.length === 0 && <div className="rounded-xl border border-dashed bg-muted/20 p-8 text-center"><div className="mx-auto flex size-11 items-center justify-center rounded-xl bg-primary/10 text-primary"><Plus className="size-5" /></div><h2 className="mt-4 font-semibold">Turn workflow moments into safe automation</h2><p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">A Hook watches an event, applies it to named work, checks conditions, makes a decision, and runs an action. Start from a plain-language recipe and verify it in Dry run before publishing.</p><Button className="mt-4" onClick={() => onOpenHook()}><Plus className="size-4" />Choose a recipe</Button></div>}<div className="mt-4 rounded-md border border-l-[3px] border-l-primary bg-muted/30 p-3 text-sm text-muted-foreground"><strong className="text-foreground">Dry run is required.</strong> Every new or changed hook records what it would do before an authorised person can publish it. Managed hooks are locked by the workspace owner.</div></div>
  </div>;
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
