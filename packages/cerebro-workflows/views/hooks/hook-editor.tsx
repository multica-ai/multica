"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowLeft, FlaskConical, MoreHorizontal } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import type { HookRun, WorkflowHook } from "../../core/hook-types";
import { HookChain, type HookStepKey } from "./hook-chain";
import { HookStepPanel } from "./hook-step-panel";
import { HookTestHistory } from "./hook-test-history";
import { validateHook } from "../../core/hook-validation";
import { describeHook, stepSummary } from "../../core/hook-ux";
import type { HookDirectory } from "./hook-target-picker";

interface HookEditorProps {
  initialHook: WorkflowHook;
  onSave: (hook: WorkflowHook) => void | Promise<unknown>;
  onPublish?: (hook: WorkflowHook, saved?: unknown) => void | Promise<unknown>;
  onTest?: (hook: WorkflowHook, event?: Record<string, unknown>) => void | Promise<unknown>;
  onDisable?: (hook: WorkflowHook) => void;
  onDelete?: (hook: WorkflowHook) => void;
  onDuplicate?: (hook: WorkflowHook) => void;
  onOpenHistory?: () => void;
  onBack?: () => void;
  canPublish?: boolean;
  runs?: HookRun[];
  directory?: HookDirectory;
  pending?: boolean;
}

export function HookEditor({ initialHook, onSave, onPublish, onTest, onDisable, onDelete, onDuplicate, onOpenHistory, onBack, canPublish = false, runs = [], directory = {}, pending = false }: HookEditorProps) {
  const [hook, setHook] = useState(initialHook);
  const [selected, setSelected] = useState<HookStepKey>(initialHook.events.length > 0 ? "decision" : "trigger");
  const [testing, setTesting] = useState(false);
  const [publishOpen, setPublishOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => setHook(initialHook), [initialHook]);
  const validation = validateHook(hook);
  const publishEnabled = canPublish && hook.baseline_run_count > 0 && hook.mode !== "managed" && validation.valid;

  return <div className="flex h-full min-h-0 flex-col bg-background">
    <header className="flex min-h-14 flex-wrap items-center gap-3 border-b px-4 py-2">
      <Button variant="outline" size="icon" aria-label="Back to hooks" onClick={onBack}><ArrowLeft className="size-4" /></Button>
      <div className="min-w-0 flex-1 sm:flex-none"><Input aria-label="Hook name" placeholder="Untitled hook" className="h-8 w-full min-w-0 border-0 bg-transparent px-0 text-sm font-semibold shadow-none focus-visible:ring-0 sm:w-80" value={hook.name} onChange={(event) => setHook({ ...hook, name: event.target.value })} /><p className="text-[11px] text-muted-foreground">Draft v{hook.version} · {stepSummary(hook, "failure")}</p></div>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <span className="rounded-full bg-primary/10 px-2 py-1 text-xs font-semibold text-primary">{({ off: "Off", dry_run: "Dry run", enforce: "Enforced", managed: "Managed" })[hook.mode]}</span>
        {hook.id && <Button variant="outline" size="sm" onClick={() => { setTesting(true); onOpenHistory?.(); }}>History</Button>}
        <Button variant="outline" size="sm" disabled={!hook.id || pending} onClick={() => { onTest?.(hook); setTesting(true); }}><FlaskConical className="size-4" />Test</Button>
        <Button variant="outline" size="sm" disabled={pending} onClick={() => onSave({ ...hook, mode: "dry_run" })}>{pending ? "Saving…" : "Save draft"}</Button>
        <Button title={validation.message ?? (hook.baseline_run_count < 1 ? "Run at least one dry-run test before publishing." : undefined)} size="sm" disabled={!publishEnabled || pending} onClick={() => setPublishOpen(true)}>Publish</Button>
        {hook.id && (onDisable || onDelete || onDuplicate) && <DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" size="icon-sm" aria-label="More hook actions" />}><MoreHorizontal className="size-4" /></DropdownMenuTrigger><DropdownMenuContent align="end">
          {onDisable && <DropdownMenuItem onClick={() => onDisable(hook)}>Disable hook</DropdownMenuItem>}
          {onDuplicate && <DropdownMenuItem onClick={() => onDuplicate(hook)}>Duplicate hook</DropdownMenuItem>}
          {onDelete && <><DropdownMenuSeparator /><DropdownMenuItem variant="destructive" onClick={() => setDeleteOpen(true)}>Delete hook</DropdownMenuItem></>}
        </DropdownMenuContent></DropdownMenu>}
      </div>
    </header>
    <div className="grid gap-2 border-b bg-muted/20 px-4 py-3 sm:grid-cols-[minmax(12rem,22rem)_minmax(0,1fr)] sm:items-center">
      <Input aria-label="Hook description" placeholder="Add a short description" value={hook.description} onChange={(event) => setHook({ ...hook, description: event.target.value })} />
      <p aria-label="What this hook does" className="text-sm text-muted-foreground"><strong className="text-foreground">What this hook does: </strong>{describeHook(hook)}</p>
    </div>
    <main className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto md:grid-cols-[minmax(18rem,430px)_minmax(0,1fr)] md:overflow-hidden">
      <div className="overflow-y-auto border-b bg-muted/30 md:border-b-0 md:border-r"><HookChain selected={selected} hook={hook} onSelect={(step) => { setSelected(step); setTesting(false); requestAnimationFrame(() => { if (typeof panelRef.current?.scrollIntoView === "function") panelRef.current.scrollIntoView({ behavior: "smooth", block: "start" }); }); }} /></div>
      <div ref={panelRef} className="scroll-mt-2 overflow-y-auto max-md:pl-14 max-md:pb-20">{testing ? <HookTestHistory runs={runs} onTest={(event) => onTest?.(hook, event)} directory={directory} /> : <HookStepPanel step={selected} hook={hook} onChange={setHook} directory={directory} />}</div>
    </main>
    <Dialog open={publishOpen} onOpenChange={setPublishOpen}><DialogContent aria-label="Publish hook"><DialogHeader><DialogTitle>Publish hook</DialogTitle><DialogDescription>Publishing switches this hook from Dry run to Enforced. Matching events can be blocked and configured actions can run.</DialogDescription></DialogHeader>
      <div aria-label="Publish checklist" className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm"><p><strong>Trigger:</strong> {stepSummary(hook, "trigger")}</p><p><strong>Scope:</strong> {stepSummary(hook, "scope")}</p><p><strong>Decision:</strong> {stepSummary(hook, "decision")}</p><p><strong>Action:</strong> {stepSummary(hook, "action")}</p><p><strong>Failure behavior:</strong> {stepSummary(hook, "failure")}</p><p><strong>Dry run evidence:</strong> {hook.baseline_run_count} observed {hook.baseline_run_count === 1 ? "run" : "runs"}</p></div>
      <DialogFooter><Button variant="outline" onClick={() => setPublishOpen(false)}>Cancel</Button><Button disabled={publishing} onClick={async () => { setPublishing(true); try { const saved = await onSave({ ...hook, mode: "dry_run" }); await onPublish?.({ ...hook, mode: "enforce" }, saved); setPublishOpen(false); } finally { setPublishing(false); } }}>{publishing ? "Publishing…" : "Confirm publish"}</Button></DialogFooter>
    </DialogContent></Dialog>
    <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}><DialogContent aria-label="Delete hook"><DialogHeader><DialogTitle>Delete hook?</DialogTitle><DialogDescription>This removes the hook and its saved versions. This action cannot be undone.</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>Cancel</Button><Button variant="destructive" onClick={() => { onDelete?.(hook); setDeleteOpen(false); }}>Confirm delete</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
