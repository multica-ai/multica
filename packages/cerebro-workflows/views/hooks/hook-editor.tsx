"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, FlaskConical, MoreHorizontal } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import type { HookRun, WorkflowHook } from "../../core/hook-types";
import { HOOK_STEPS, HookChain, MobileHookStepper, type HookStepKey } from "./hook-chain";
import { HookStepPanel } from "./hook-step-panel";
import { HookTestHistory } from "./hook-test-history";
import { validateHook } from "../../core/hook-validation";
import { describeHook, failModeSummary, stepSummary } from "../../core/hook-ux";
import type { HookDirectory } from "./hook-target-picker";

interface HookEditorProps {
  initialHook: WorkflowHook;
  onSave: (hook: WorkflowHook) => void | Promise<unknown>;
  onPublish?: (hook: WorkflowHook, saved?: unknown) => void | Promise<unknown>;
  onTest?: (hook: WorkflowHook, eventID: string) => void | Promise<unknown>;
  onDisable?: (hook: WorkflowHook) => void;
  onDiscard?: (hook: WorkflowHook) => void;
  onDelete?: (hook: WorkflowHook) => void;
  onDuplicate?: (hook: WorkflowHook) => void;
  onOpenHistory?: () => void;
  onBack?: () => void;
  canPublish?: boolean;
  runs?: HookRun[];
  directory?: HookDirectory;
  pending?: boolean;
}

export function HookEditor({ initialHook, onSave, onPublish, onTest, onDisable, onDiscard, onDelete, onDuplicate, onOpenHistory, onBack, canPublish = false, runs = [], directory = {}, pending = false }: HookEditorProps) {
  const [hook, setHook] = useState(initialHook);
  const [selected, setSelected] = useState<HookStepKey>(initialHook.events.length > 0 ? "guide" : "when");
  const [testing, setTesting] = useState(false);
  const [publishOpen, setPublishOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => setHook(initialHook), [initialHook]);
  const dirty = JSON.stringify(hook) !== JSON.stringify(initialHook);
  const validation = validateHook(hook);
  // A Managed hook is defined in code and is live from the moment it exists.
  // Offering Publish and "Before you can publish:" on it read as "this hook is
  // incomplete", when nothing was ever missing (FIR-4797).
  const managed = hook.mode === "managed" || hook.lifecycle.state === "managed";
  const publishEnabled = !dirty && canPublish && hook.baseline_run_count > 0 && !managed && validation.valid;
  const selectedIndex = HOOK_STEPS.findIndex((step) => step.key === selected);
  const selectStep = (step: HookStepKey) => {
    setSelected(step);
    setTesting(false);
    requestAnimationFrame(() => { if (typeof panelRef.current?.scrollIntoView === "function") panelRef.current.scrollIntoView({ behavior: "smooth", block: "start" }); });
  };
  const saveDraft = () => onSave({ ...hook, mode: "dry_run" });
  const publishRequirement = dirty ? "Save draft before testing or publishing." : validation.message ?? (hook.baseline_run_count < 1 ? "Run at least one dry-run test before publishing." : !canPublish ? "Complete a successful Dry run before publishing." : undefined);
  const hasLifecycleIdentity = Boolean(hook.family_id);
  const hasLive = hasLifecycleIdentity
    ? Boolean(hook.lifecycle.live_policy_id)
    : hook.mode === "enforce" || hook.mode === "managed" || hook.mode === "off";
  const hasDraft = hasLifecycleIdentity ? Boolean(hook.lifecycle.draft_id) : hook.mode === "dry_run";
  const liveVersion = hook.lifecycle.live_version ?? (hasLive ? hook.version : undefined);
  const identityLabel = hasLive && hasDraft
    ? `Live v${liveVersion} · Editing Draft v${hook.version}${hook.revision ? ` revision ${hook.revision}` : ""}`
    : hasLive
      ? hasLifecycleIdentity
        ? `Live v${liveVersion}`
        : `${({ enforce: "Enforced", managed: "Managed", off: "Off" } as const)[hook.mode as "enforce" | "managed" | "off"]} v${liveVersion}`
      : `Draft v${hook.version}${hook.revision ? ` revision ${hook.revision}` : ""}`;

  return <div className="flex h-full min-h-0 flex-col bg-background">
    <header className="flex min-h-14 flex-wrap items-center gap-3 border-b px-4 py-2">
      <Button variant="outline" size="icon" aria-label="Back to hooks" onClick={onBack}><ArrowLeft className="size-4" /></Button>
      <div className="min-w-0 flex-1 sm:flex-none"><Input aria-label="Hook name" placeholder="Untitled hook" className="h-8 w-full min-w-0 border-0 bg-transparent px-0 text-sm font-semibold shadow-none focus-visible:ring-0 sm:w-80" value={hook.name} onChange={(event) => setHook({ ...hook, name: event.target.value })} /><p className="text-[11px] text-muted-foreground">{identityLabel} · {failModeSummary(hook)}</p></div>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <span className="rounded-full bg-primary/10 px-2 py-1 text-xs font-semibold text-primary">{({ off: "Off", dry_run: "Dry run", enforce: "Enforced", managed: "Managed" })[hook.mode]}</span>
        {hook.id && <Button variant="outline" size="sm" onClick={() => { setTesting(true); onOpenHistory?.(); }}>History</Button>}
        <Button variant="outline" size="sm" disabled={!hook.id || !hook.revision || dirty || pending} onClick={() => setTesting(true)}><FlaskConical className="size-4" />Test</Button>
        {!managed && <Button variant="outline" size="sm" disabled={pending} onClick={saveDraft}>{pending ? "Saving…" : "Save draft"}</Button>}
        {!managed && <Button title={publishRequirement} size="sm" disabled={!publishEnabled || pending} onClick={() => setPublishOpen(true)}>Publish</Button>}
        {hook.id && (onDisable || onDiscard || onDelete || onDuplicate) && <DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" size="icon-sm" aria-label="More hook actions" />}><MoreHorizontal className="size-4" /></DropdownMenuTrigger><DropdownMenuContent align="end">
          {onDisable && <DropdownMenuItem onClick={() => setDisableOpen(true)}>Disable hook</DropdownMenuItem>}
          {onDiscard && hasDraft && <DropdownMenuItem onClick={() => setDiscardOpen(true)}>Discard draft</DropdownMenuItem>}
          {onDuplicate && <DropdownMenuItem onClick={() => onDuplicate(hook)}>Duplicate hook</DropdownMenuItem>}
          {onDelete && <><DropdownMenuSeparator /><DropdownMenuItem variant="destructive" onClick={() => setDeleteOpen(true)}>Delete hook</DropdownMenuItem></>}
        </DropdownMenuContent></DropdownMenu>}
      </div>
    </header>
    {hasLive && hasDraft && <p aria-label="What is enforcing" className="border-b bg-primary/5 px-4 py-2 text-xs text-muted-foreground"><strong className="text-foreground">You are editing a Draft. Nothing below is running yet.</strong> Live v{liveVersion} keeps running on every matching event until someone publishes this Draft. Anything reported below as missing or invalid is about the Draft — it does not affect Live v{liveVersion}.</p>}
    {hasLive && !hasDraft && <p aria-label="What is enforcing" className="border-b bg-success/5 px-4 py-2 text-xs text-muted-foreground"><strong className="text-foreground">This hook is live.</strong> {hook.mode === "managed" ? "It is owned by the workspace, so it runs on every matching event and cannot be published or edited here." : `Version ${liveVersion} runs on every matching event right now.`}</p>}
    {!managed && dirty && <p aria-label="Draft save requirement" className="border-b bg-muted/40 px-4 py-2 text-xs text-muted-foreground">Save draft before testing or publishing.</p>}
    {!managed && !publishEnabled && publishRequirement && <p aria-label="Publish requirements" className="border-b bg-muted/20 px-4 py-2 text-xs text-muted-foreground"><strong className="text-foreground">Before you can publish {hasLive ? "this draft" : "this hook"}:</strong> {publishRequirement}</p>}
    <div className="grid gap-2 border-b bg-muted/20 px-4 py-3 sm:grid-cols-[minmax(12rem,22rem)_minmax(0,1fr)] sm:items-center">
      <Input aria-label="Hook description" placeholder="Add a short description" value={hook.description} onChange={(event) => setHook({ ...hook, description: event.target.value })} />
      <p aria-label="What this hook does" className="text-sm text-muted-foreground"><strong className="text-foreground">What this hook does: </strong>{describeHook(hook, directory)}</p>
    </div>
    <div className="grid gap-3 border-b px-4 py-3 sm:grid-cols-2">
      <label className="grid gap-1 text-sm font-medium">Rule<Input aria-label="Rule" placeholder="What must be true?" value={hook.contract_rule} onChange={(event) => setHook({ ...hook, contract_rule: event.target.value })} /></label>
      <label className="grid gap-1 text-sm font-medium">How to satisfy it<Input aria-label="How to satisfy it" placeholder="What should the agent or person do?" value={hook.contract_satisfy} onChange={(event) => setHook({ ...hook, contract_satisfy: event.target.value })} /></label>
    </div>
    <main className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto md:grid-cols-[minmax(18rem,430px)_minmax(0,1fr)] md:overflow-hidden">
      <MobileHookStepper selected={selected} hook={hook} onSelect={selectStep} />
      <div className="hidden overflow-y-auto border-r bg-muted/30 md:block"><HookChain selected={selected} hook={hook} directory={directory} onSelect={selectStep} /></div>
      <div ref={panelRef} className="scroll-mt-2 overflow-y-auto">{testing ? <HookTestHistory events={hook.compatible_events ?? []} runs={runs} onTest={(eventID) => onTest?.(hook, eventID)} directory={directory} /> : <HookStepPanel step={selected} hook={hook} onChange={setHook} directory={directory} />}
        {!testing && <nav aria-label="Hook step navigation" className="sticky bottom-0 flex items-center justify-between gap-3 border-t bg-background/95 p-3 backdrop-blur md:hidden">
          <Button type="button" variant="outline" aria-label="Back one step" disabled={selectedIndex <= 0} onClick={() => selectStep(HOOK_STEPS[selectedIndex - 1]!.key)}><ArrowLeft className="size-4" />Back</Button>
          <span className="text-xs text-muted-foreground">Step {selectedIndex + 1} of {HOOK_STEPS.length}</span>
          <Button type="button" aria-label="Next step" disabled={selectedIndex >= HOOK_STEPS.length - 1} onClick={() => selectStep(HOOK_STEPS[selectedIndex + 1]!.key)}>Next<ArrowRight className="size-4" /></Button>
        </nav>}
      </div>
    </main>
    <Dialog open={publishOpen} onOpenChange={setPublishOpen}><DialogContent aria-label="Publish hook"><DialogHeader><DialogTitle>Publish hook</DialogTitle><DialogDescription>{hasLive && hasDraft ? `Publishing replaces Live v${liveVersion} with Draft v${hook.version}. Live v${liveVersion} stays active until this finishes.` : "Publishing switches this hook from Dry run to Enforced. Matching events can be blocked and configured actions can run."}</DialogDescription></DialogHeader>
      <div aria-label="Publish checklist" className="grid gap-2 rounded-lg border bg-muted/30 p-3 text-sm"><p><strong>Rule:</strong> {hook.contract_rule}</p><p><strong>How to satisfy it:</strong> {hook.contract_satisfy}</p><p><strong>Summary:</strong> {describeHook(hook, directory)}</p><p><strong>When:</strong> {stepSummary(hook, "when", directory)}</p><p><strong>Guide or enforce:</strong> {stepSummary(hook, "guide", directory)}</p><p><strong>Actions:</strong> {stepSummary(hook, "actions", directory)}</p><p><strong>If the check cannot run:</strong> {failModeSummary(hook)}</p><p><strong>Dry run evidence:</strong> {hook.baseline_run_count} observed {hook.baseline_run_count === 1 ? "run" : "runs"}</p></div>
      <DialogFooter><Button variant="outline" onClick={() => setPublishOpen(false)}>Cancel</Button><Button disabled={publishing} onClick={async () => { setPublishing(true); try { await onPublish?.({ ...hook, mode: "enforce" }); setPublishOpen(false); } finally { setPublishing(false); } }}>{publishing ? "Publishing…" : "Confirm publish"}</Button></DialogFooter>
    </DialogContent></Dialog>
    <Dialog open={discardOpen} onOpenChange={setDiscardOpen}><DialogContent aria-label="Discard draft"><DialogHeader><DialogTitle>Discard draft?</DialogTitle><DialogDescription>{hasLive ? `Live v${liveVersion} remains active.` : "This removes the current Draft changes."} Draft history remains available for audit.</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDiscardOpen(false)}>Cancel</Button><Button variant="destructive" onClick={() => { onDiscard?.(hook); setDiscardOpen(false); }}>Confirm discard</Button></DialogFooter></DialogContent></Dialog>
    <Dialog open={disableOpen} onOpenChange={setDisableOpen}><DialogContent aria-label="Disable hook"><DialogHeader><DialogTitle>Disable hook?</DialogTitle><DialogDescription>{hasLive ? `Live v${liveVersion} stops running on matching events.` : "This hook stops running on matching events."} {hasDraft ? `Draft v${hook.version} is kept and can still be published later.` : "Nothing is deleted."}</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDisableOpen(false)}>Cancel</Button><Button variant="destructive" onClick={() => { onDisable?.(hook); setDisableOpen(false); }}>Confirm disable</Button></DialogFooter></DialogContent></Dialog>
    <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}><DialogContent aria-label="Delete hook"><DialogHeader><DialogTitle>Delete hook?</DialogTitle><DialogDescription>This removes the hook and its saved versions. This action cannot be undone.</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>Cancel</Button><Button variant="destructive" onClick={() => { onDelete?.(hook); setDeleteOpen(false); }}>Confirm delete</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
