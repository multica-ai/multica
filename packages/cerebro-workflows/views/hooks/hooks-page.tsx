"use client";

import { LockKeyhole, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "@multica/views/layout/page-header";
import type { HookMode, WorkflowHook } from "../../core/hook-types";

export function HooksPage({ hooks, onOpenHook, onOpenHistory }: { hooks: WorkflowHook[]; onOpenHook: (id?: string) => void; onOpenHistory?: () => void }) {
  return <div className="flex h-full flex-col bg-background">
    <PageHeader className="flex-wrap justify-between gap-3 px-4 sm:px-5"><div className="min-w-0"><h1 className="text-sm font-semibold">Hooks</h1><p className="truncate text-[11px] text-muted-foreground">Workflows · trigger, filter, decide, and act</p></div><div className="ml-auto flex gap-2">{onOpenHistory && <Button variant="outline" size="sm" onClick={onOpenHistory}>Run history</Button>}<Button size="sm" onClick={() => onOpenHook()}><Plus className="size-4" />New hook</Button></div></PageHeader>
    <div className="overflow-y-auto p-4 sm:p-6"><div className="grid gap-2">{hooks.map((hook) => <button type="button" key={hook.id ?? hook.name} className="grid w-full gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/40 md:grid-cols-[minmax(12rem,1.2fr)_minmax(16rem,1fr)_minmax(8rem,auto)_auto] md:items-center" onClick={() => onOpenHook(hook.id)}>
      <span className="min-w-0"><strong className="block truncate">{hook.name || "Untitled hook"}</strong><span className="block truncate text-xs text-muted-foreground">{hook.description || "No description"}</span></span>
      <span className="text-xs">{hook.events.length > 0 ? "Trigger" : "Choose trigger"} → {hook.conditions.length} conditions → {capitalize(hook.decision)} → {hook.actions.length} {hook.actions.length === 1 ? "action" : "actions"}</span>
      <span className="text-xs text-muted-foreground">{hook.last_run_at ? new Date(hook.last_run_at).toLocaleString() : "Never"}</span>
      <HookState mode={hook.mode} />
    </button>)}</div>{hooks.length === 0 && <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">No hooks yet. Select <strong>New hook</strong> to build one step by step.</div>}<div className="mt-4 rounded-md border border-l-[3px] border-l-primary bg-muted/30 p-3 text-sm text-muted-foreground"><strong className="text-foreground">Dry run is required.</strong> Every new or changed hook records what it would do before an authorised person can publish it. Managed hooks are locked by the workspace owner.</div></div>
  </div>;
}

function HookState({ mode }: { mode: HookMode }) { const config = { off: ["Off", "bg-muted text-muted-foreground"], dry_run: ["Dry run", "bg-primary/10 text-primary"], enforce: ["Enforced", "bg-success/10 text-success"], managed: ["Managed", "bg-secondary text-secondary-foreground"] }[mode]; return <span className={`inline-flex w-fit items-center gap-1 rounded-full px-2 py-1 text-xs font-semibold ${config[1]}`}>{mode === "managed" && <LockKeyhole className="size-3" />}{config[0]}</span>; }
function capitalize(value: string) { return value.charAt(0).toUpperCase() + value.slice(1); }
