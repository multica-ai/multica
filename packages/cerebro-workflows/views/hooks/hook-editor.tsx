"use client";

import { useEffect, useState } from "react";
import { ArrowLeft, FlaskConical } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { HookRun, WorkflowHook } from "../../core/hook-types";
import { HookChain, type HookStepKey } from "./hook-chain";
import { HookStepPanel } from "./hook-step-panel";
import { HookTestHistory } from "./hook-test-history";

export function HookEditor({ initialHook, onSave, onTest, onBack, canPublish = false, runs = [] }: { initialHook: WorkflowHook; onSave: (hook: WorkflowHook) => void; onTest?: (hook: WorkflowHook) => void; onBack?: () => void; canPublish?: boolean; runs?: HookRun[] }) {
  const [hook, setHook] = useState(initialHook);
  const [selected, setSelected] = useState<HookStepKey>("decision");
  const [testing, setTesting] = useState(false);
  useEffect(() => setHook(initialHook), [initialHook]);
  const publishEnabled = canPublish && hook.baseline_run_count > 0 && hook.mode !== "managed";

  return <div className="flex h-full min-h-0 flex-col bg-background">
    <header className="flex min-h-14 items-center gap-3 border-b px-4">
      <Button variant="outline" size="icon" aria-label="Back to hooks" onClick={onBack}><ArrowLeft className="size-4" /></Button>
      <div><input aria-label="Hook name" className="w-80 bg-transparent text-sm font-semibold outline-none" value={hook.name} onChange={(event) => setHook({ ...hook, name: event.target.value })} /><p className="text-[11px] text-muted-foreground">Draft v{hook.version} · Dry run</p></div>
      <div className="ml-auto flex items-center gap-2"><span className="rounded-full bg-blue-50 px-2 py-1 text-xs font-semibold text-blue-700">Dry run</span><Button variant="outline" size="sm" disabled={!hook.id} onClick={() => { onTest?.(hook); setTesting(true); }}><FlaskConical className="size-4" />Test</Button><Button variant="outline" size="sm" onClick={() => onSave({ ...hook, mode: "dry_run" })}>Save draft</Button><Button size="sm" disabled={!publishEnabled} onClick={() => onSave({ ...hook, mode: "enforce" })}>Publish</Button></div>
    </header>
    <main className="grid min-h-0 flex-1 grid-cols-[430px_minmax(0,1fr)] overflow-hidden">
      <div className="overflow-y-auto border-r bg-muted/30"><HookChain selected={selected} onSelect={(step) => { setSelected(step); setTesting(false); }} /></div>
      <div className="overflow-y-auto">{testing ? <HookTestHistory runs={runs} /> : <HookStepPanel step={selected} hook={hook} onChange={setHook} />}</div>
    </main>
  </div>;
}
