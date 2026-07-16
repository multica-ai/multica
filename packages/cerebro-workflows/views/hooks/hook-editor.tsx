"use client";

import { useEffect, useState } from "react";
import { ArrowLeft, FlaskConical } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import type { HookRun, WorkflowHook } from "../../core/hook-types";
import { HookChain, type HookStepKey } from "./hook-chain";
import { HookStepPanel } from "./hook-step-panel";
import { HookTestHistory } from "./hook-test-history";
import { validateHook } from "../../core/hook-validation";
import type { HookDirectory } from "./hook-target-picker";

export function HookEditor({ initialHook, onSave, onTest, onBack, canPublish = false, runs = [], directory = {} }: { initialHook: WorkflowHook; onSave: (hook: WorkflowHook) => void; onTest?: (hook: WorkflowHook) => void; onBack?: () => void; canPublish?: boolean; runs?: HookRun[]; directory?: HookDirectory }) {
  const [hook, setHook] = useState(initialHook);
  const [selected, setSelected] = useState<HookStepKey>(initialHook.events.length > 0 ? "decision" : "trigger");
  const [testing, setTesting] = useState(false);
  useEffect(() => setHook(initialHook), [initialHook]);
  const validation = validateHook(hook);
  const publishEnabled = canPublish && hook.baseline_run_count > 0 && hook.mode !== "managed" && validation.valid;

  return <div className="flex h-full min-h-0 flex-col bg-background">
    <header className="flex min-h-14 flex-wrap items-center gap-3 border-b px-4 py-2">
      <Button variant="outline" size="icon" aria-label="Back to hooks" onClick={onBack}><ArrowLeft className="size-4" /></Button>
      <div className="min-w-0 flex-1 sm:flex-none"><Input aria-label="Hook name" placeholder="Untitled hook" className="h-8 w-full min-w-0 border-0 bg-transparent px-0 text-sm font-semibold shadow-none focus-visible:ring-0 sm:w-80" value={hook.name} onChange={(event) => setHook({ ...hook, name: event.target.value })} /><p className="text-[11px] text-muted-foreground">Draft v{hook.version} · Dry run</p></div>
      <div className="ml-auto flex flex-wrap items-center gap-2"><span className="rounded-full bg-primary/10 px-2 py-1 text-xs font-semibold text-primary">Dry run</span><Button variant="outline" size="sm" disabled={!hook.id} onClick={() => { onTest?.(hook); setTesting(true); }}><FlaskConical className="size-4" />Test</Button><Button variant="outline" size="sm" onClick={() => onSave({ ...hook, mode: "dry_run" })}>Save draft</Button><Button title={validation.message} size="sm" disabled={!publishEnabled} onClick={() => onSave({ ...hook, mode: "enforce" })}>Publish</Button></div>
    </header>
    <main className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto md:grid-cols-[minmax(18rem,430px)_minmax(0,1fr)] md:overflow-hidden">
      <div className="overflow-y-auto border-b bg-muted/30 md:border-b-0 md:border-r"><HookChain selected={selected} hook={hook} onSelect={(step) => { setSelected(step); setTesting(false); }} onAdd={(step) => { setSelected(step); setTesting(false); setHook(addItemForStep(hook, step)); }} /></div>
      <div className="overflow-y-auto">{testing ? <HookTestHistory runs={runs} /> : <HookStepPanel step={selected} hook={hook} onChange={setHook} directory={directory} />}</div>
    </main>
  </div>;
}

function addItemForStep(hook: WorkflowHook, step: HookStepKey): WorkflowHook {
  if (step === "scope") return { ...hook, bindings: [...hook.bindings, { kind: "workspace", value: "" }] };
  if (step === "filter") return { ...hook, conditions: [...hook.conditions, { field: "", operator: "eq", value: "", conjunction: "AND" }] };
  if (step === "action") return { ...hook, actions: [...hook.actions, { type: "audit.record", label: "Record audit event", config: {} }] };
  return hook;
}
