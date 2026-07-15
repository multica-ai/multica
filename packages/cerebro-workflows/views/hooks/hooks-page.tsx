"use client";

import { LockKeyhole, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import type { HookMode, WorkflowHook } from "../../core/hook-types";

export function HooksPage({ hooks, onOpenHook, onOpenHistory }: { hooks: WorkflowHook[]; onOpenHook: (id?: string) => void; onOpenHistory?: () => void }) {
  return <div className="flex h-full flex-col bg-background">
    <header className="flex min-h-14 items-center gap-3 border-b px-5"><div><h1 className="text-sm font-semibold">Hooks</h1><p className="text-[11px] text-muted-foreground">Workflows · trigger, filter, decide, and act</p></div><div className="ml-auto flex gap-2">{onOpenHistory && <Button variant="outline" size="sm" onClick={onOpenHistory}>Run history</Button>}<Button size="sm" onClick={() => onOpenHook()}><Plus className="size-4" />New hook</Button></div></header>
    <div className="overflow-y-auto p-6"><Table><TableHeader><TableRow><TableHead>Name</TableHead><TableHead>Chain</TableHead><TableHead>Last run</TableHead><TableHead>State</TableHead></TableRow></TableHeader><TableBody>{hooks.map((hook) => <TableRow key={hook.id ?? hook.name} className="cursor-pointer" onClick={() => onOpenHook(hook.id)}><TableCell><strong>{hook.name}</strong><span className="block text-xs text-muted-foreground">{hook.description}</span></TableCell><TableCell className="text-xs">Trigger → {hook.conditions.length} conditions → {capitalize(hook.decision)} → {hook.actions.length} action</TableCell><TableCell className="text-xs text-muted-foreground">{hook.last_run_at ? new Date(hook.last_run_at).toLocaleString() : "Never"}</TableCell><TableCell><HookState mode={hook.mode} /></TableCell></TableRow>)}</TableBody></Table><div className="mt-4 rounded-md border border-l-[3px] border-l-[#5b5bd6] bg-muted/30 p-3 text-sm text-muted-foreground"><strong className="text-foreground">Dry run is required.</strong> Every new or changed hook records what it would do before an authorised person can publish it. Managed hooks are locked by the workspace owner.</div></div>
  </div>;
}

function HookState({ mode }: { mode: HookMode }) { const config = { off: ["Off", "bg-muted text-muted-foreground"], dry_run: ["Dry run", "bg-blue-50 text-blue-700"], enforce: ["Enforced", "bg-emerald-50 text-emerald-700"], managed: ["Managed", "bg-purple-50 text-purple-700"] }[mode]; return <span className={`inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs font-semibold ${config[1]}`}>{mode === "managed" && <LockKeyhole className="size-3" />}{config[0]}</span>; }
function capitalize(value: string) { return value.charAt(0).toUpperCase() + value.slice(1); }
