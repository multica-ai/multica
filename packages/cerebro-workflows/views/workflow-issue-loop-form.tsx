"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AgentPicker } from "@multica/views/autopilots/components/pickers/agent-picker";
import { AssigneePicker } from "@multica/views/issues/components";
import { ChevronDown, Eye, Gauge, GripVertical, Play, Plus, SquareTerminal, Trash2, User } from "lucide-react";

import { SkillNamePicker } from "./loop-pickers";
import { MachineControlNotice } from "./workflow-issue-loop-chain";
import {
  BLOCK_TYPES,
  blockMeta,
  blockSummary,
  nudgeBlock,
  reorderBlocks,
  totalSteps,
  type LoopBlockKind,
} from "./loop-chain-model";
import {
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
} from "../core";
import type {
  CerebroWorkflow,
  LoopBusyPolicy,
  LoopChainBlock,
  LoopChainPhase,
  LoopChainSpec,
  LoopBlockType,
  WorkflowWriteInput,
} from "../core/types";

const BUSY_OPTIONS: ReadonlyArray<{ value: LoopBusyPolicy; label: string }> = [
  { value: "wait", label: "Wait for an agent" },
  { value: "pause", label: "Pause the run" },
  { value: "wakeup", label: "Schedule a wakeup" },
  { value: "ping_member", label: "Notify a person" },
];

const TYPE_ICON: Record<LoopBlockType, typeof Play> = {
  session: Play,
  command: SquareTerminal,
  review: Eye,
  human: User,
  eval: Gauge,
};

// Machine steps read blue, human steps read gold — a clear two-tone split (the
// intent of the approved mockup) expressed with semantic tokens only, so the
// package's no-hardcoded-palette guard stays green.
const DOT_CLASS: Record<LoopBlockKind, string> = {
  machine: "bg-info",
  human: "bg-warning",
};
const BADGE_CLASS: Record<LoopBlockKind, string> = {
  machine: "bg-info/10 text-info",
  human: "bg-warning/15 text-warning",
};

function key(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 9)}`;
}

function newBlock(type: LoopBlockType = "session"): LoopChainBlock {
  const id = key(type);
  const block: LoopChainBlock = { id, type, name: blockMeta(type).label };
  if (type === "session" || type === "review") {
    block.agents = [];
    block.on_all_busy = "wait";
  }
  if (type === "command") block.expect = "exit_zero";
  if (type === "eval") block.eval_phase = "delivery";
  return block;
}

function newPhase(index = 0): LoopChainPhase {
  return {
    id: key("phase"),
    name: `Phase ${index + 1}`,
    blocks: [newBlock()],
    limits: { max_steps: 8, max_rounds: 3, no_progress_stalls: 2, max_wait_seconds: 600 },
  };
}

function newChain(): LoopChainSpec {
  return { version: 2, phases: [newPhase()], done_status: "done" };
}

function isChainSpec(spec: CerebroWorkflow["loop_spec"]): spec is LoopChainSpec {
  return spec?.version === 2 && Array.isArray((spec as LoopChainSpec).phases);
}

interface FormState {
  name: string;
  enabled: boolean;
  chain: LoopChainSpec;
  legacy: boolean;
}

export function formFromWorkflow(workflow: CerebroWorkflow): FormState {
  return {
    name: workflow.name,
    enabled: workflow.enabled,
    chain: isChainSpec(workflow.loop_spec) ? workflow.loop_spec : newChain(),
    legacy: Boolean(workflow.loop_spec && !isChainSpec(workflow.loop_spec)),
  };
}

interface Props {
  workflowId?: string;
  embedded?: boolean;
}

export function WorkflowIssueLoopForm({ workflowId, embedded }: Props) {
  const featureEnabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const wsId = workspace?.id ?? "";
  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: Boolean(workflowId && wsId),
  });
  const [form, setForm] = useState<FormState>({ name: "", enabled: true, chain: newChain(), legacy: false });
  const [openId, setOpenId] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (detail.data) {
      setForm(formFromWorkflow(detail.data));
      setDirty(false);
    }
  }, [detail.data]);

  const save = useMutation({
    mutationFn: async () => {
      const payload: WorkflowWriteInput = {
        name: form.name,
        enabled: form.enabled,
        workflow_type: "issue_loop",
        loop_spec: form.chain,
      };
      return workflowId ? updateWorkflow(workflowId, payload) : createWorkflow(payload);
    },
    onSuccess: (workflow) => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
      setDirty(false);
      if (workspace && !workflowId) navigation.push(`/${workspace.slug}/workflows/${workflow.id}`);
    },
    onError: (reason: unknown) => {
      setError(reason instanceof Error ? reason.message : "Could not save the Issue workflow");
    },
  });

  if (!featureEnabled) return null;
  if (!workspace) {
    return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace…</div>;
  }

  if (form.legacy) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="max-w-lg rounded-lg border border-warning/40 bg-warning/10 p-5 text-sm">
          <h2 className="font-semibold text-foreground">This workflow needs migration</h2>
          <p className="mt-2 text-muted-foreground">
            This workflow still uses the retired recipe format. It cannot be edited until the block-chain migration has run,
            preventing the existing recipe from being overwritten.
          </p>
        </div>
      </div>
    );
  }

  const update = (updater: (current: FormState) => FormState) => {
    setForm(updater);
    setDirty(true);
  };
  const setName = (name: string) => update((current) => ({ ...current, name }));
  const setEnabled = (enabled: boolean) => update((current) => ({ ...current, enabled }));
  const setChain = (updater: (chain: LoopChainSpec) => LoopChainSpec) =>
    update((current) => ({ ...current, chain: updater(current.chain) }));
  const setPhase = (phaseId: string, updater: (phase: LoopChainPhase) => LoopChainPhase) =>
    setChain((chain) => ({ ...chain, phases: chain.phases.map((phase) => (phase.id === phaseId ? updater(phase) : phase)) }));

  const steps = totalSteps(form.chain);

  const body = (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <form
        className="mx-auto flex max-w-2xl flex-col gap-5 p-5 sm:p-6"
        onSubmit={(event) => {
          event.preventDefault();
          setError(null);
          save.mutate();
        }}
      >
        <MachineControlNotice chain={form.chain} />

        <ChainRail
          chain={form.chain}
          openId={openId}
          onToggle={(id) => setOpenId((current) => (current === id ? null : id))}
          onReorder={(dragId, targetId, above) => setChain((chain) => ({ ...chain, phases: reorderBlocks(chain.phases, dragId, targetId, above) }))}
          onNudge={(blockId, offset) => setChain((chain) => ({ ...chain, phases: nudgeBlock(chain.phases, blockId, offset) }))}
          onPhaseChange={setPhase}
          onRemovePhase={(phaseId) => setChain((chain) => ({ ...chain, phases: chain.phases.filter((phase) => phase.id !== phaseId) }))}
          onAddPhase={() => setChain((chain) => ({ ...chain, phases: [...chain.phases, newPhase(chain.phases.length)] }))}
          onAddBlock={(phaseId) => {
            const block = newBlock();
            setPhase(phaseId, (phase) => ({ ...phase, blocks: [...phase.blocks, block] }));
            setOpenId(block.id);
          }}
          onRemoveBlock={(phaseId, blockId) => {
            setPhase(phaseId, (phase) => ({ ...phase, blocks: phase.blocks.filter((block) => block.id !== blockId) }));
            setOpenId((current) => (current === blockId ? null : current));
          }}
          doneStatus={form.chain.done_status ?? "done"}
          onDoneStatusChange={(value) => setChain((chain) => ({ ...chain, done_status: value || undefined }))}
        />

        {error && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{error}</div>}
      </form>
    </div>
  );

  const topBar = (
    <div className="flex w-full items-center justify-between gap-3">
      {!embedded && <MobileSidebarTrigger />}
      <div className="min-w-0 flex-1">
        <Input
          required
          value={form.name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Name this workflow"
          aria-label="Workflow name"
          className="h-8 border-0 bg-transparent px-0 text-[15px] font-semibold shadow-none focus-visible:ring-0"
        />
        <p className="truncate text-[11px] text-muted-foreground">
          Issue workflow · {steps} {steps === 1 ? "step" : "steps"}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <span className="text-[11px] text-muted-foreground">Enabled</span>
        <Switch checked={form.enabled} onCheckedChange={(checked) => setEnabled(checked === true)} aria-label="Enabled" />
      </div>
    </div>
  );

  const footer = (
    <div className="sticky bottom-0 flex items-center justify-between gap-3 border-t bg-background/95 px-5 py-3 backdrop-blur sm:px-6">
      <span className="text-xs text-muted-foreground">{dirty ? "Unsaved changes" : ""}</span>
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={() => navigation.push(`/${workspace.slug}/workflows`)}>Cancel</Button>
        <Button type="button" disabled={save.isPending} onClick={() => { setError(null); save.mutate(); }}>
          {save.isPending ? "Saving…" : "Save workflow"}
        </Button>
      </div>
    </div>
  );

  return (
    <div className="flex h-full flex-col">
      <div className="sticky top-0 z-10 flex shrink-0 items-center border-b bg-background px-4 py-2.5 sm:px-6">{topBar}</div>
      {body}
      {footer}
    </div>
  );
}

interface RailProps {
  chain: LoopChainSpec;
  openId: string | null;
  onToggle: (blockId: string) => void;
  onReorder: (dragId: string, targetId: string, above: boolean) => void;
  onNudge: (blockId: string, offset: -1 | 1) => void;
  onPhaseChange: (phaseId: string, updater: (phase: LoopChainPhase) => LoopChainPhase) => void;
  onRemovePhase: (phaseId: string) => void;
  onAddPhase: () => void;
  onAddBlock: (phaseId: string) => void;
  onRemoveBlock: (phaseId: string, blockId: string) => void;
  doneStatus: string;
  onDoneStatusChange: (value: string) => void;
}

function ChainRail(props: RailProps) {
  const { chain, openId, onReorder, onNudge } = props;
  // Pointer-based drag reorder (mouse + touch), matching the approved mockup.
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropHint, setDropHint] = useState<{ id: string; above: boolean } | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  const startDrag = (blockId: string) => (event: React.PointerEvent) => {
    event.preventDefault();
    setDragId(blockId);
    const onMove = (moveEvent: PointerEvent) => {
      const steps = listRef.current?.querySelectorAll<HTMLElement>("[data-step-id]");
      if (!steps) return;
      let hint: { id: string; above: boolean } | null = null;
      steps.forEach((element) => {
        const id = element.dataset.stepId;
        if (!id || id === blockId) return;
        const rect = element.getBoundingClientRect();
        if (moveEvent.clientY >= rect.top && moveEvent.clientY <= rect.bottom) {
          hint = { id, above: moveEvent.clientY < rect.top + rect.height / 2 };
        }
      });
      setDropHint(hint);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      setDropHint((current) => {
        if (current) onReorder(blockId, current.id, current.above);
        return null;
      });
      setDragId(null);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  return (
    <div ref={listRef} data-testid="issue-loop-chain" className="relative pl-7">
      <span className="absolute bottom-8 left-[9px] top-3 w-0.5 rounded bg-border" aria-hidden="true" />
      {chain.phases.map((phase, phaseIndex) => (
        <PhaseGroup
          key={phase.id}
          phase={phase}
          phaseCount={chain.phases.length}
          openId={openId}
          dragId={dragId}
          dropHint={dropHint}
          onToggle={props.onToggle}
          onStartDrag={startDrag}
          onNudge={onNudge}
          onChange={(updater) => props.onPhaseChange(phase.id, updater)}
          onRemovePhase={() => props.onRemovePhase(phase.id)}
          onAddBlock={() => props.onAddBlock(phase.id)}
          onRemoveBlock={(blockId) => props.onRemoveBlock(phase.id, blockId)}
          isLast={phaseIndex === chain.phases.length - 1}
          onAddPhase={props.onAddPhase}
        />
      ))}
      <div className="relative mt-5 pl-1 text-xs text-muted-foreground">
        <span className="absolute -left-[22px] top-1 size-2.5 rounded-full bg-foreground" aria-hidden="true" />
        When the chain finishes → issue becomes{" "}
        <span className="inline-flex items-center">
          <Input
            value={props.doneStatus}
            onChange={(event) => props.onDoneStatusChange(event.target.value)}
            aria-label="Done status"
            className="ml-1 h-6 w-28 px-2 text-xs"
          />
        </span>
      </div>
    </div>
  );
}

function PhaseGroup({
  phase,
  phaseCount,
  openId,
  dragId,
  dropHint,
  onToggle,
  onStartDrag,
  onNudge,
  onChange,
  onRemovePhase,
  onAddBlock,
  onRemoveBlock,
  isLast,
  onAddPhase,
}: {
  phase: LoopChainPhase;
  phaseCount: number;
  openId: string | null;
  dragId: string | null;
  dropHint: { id: string; above: boolean } | null;
  onToggle: (blockId: string) => void;
  onStartDrag: (blockId: string) => (event: React.PointerEvent) => void;
  onNudge: (blockId: string, offset: -1 | 1) => void;
  onChange: (updater: (phase: LoopChainPhase) => LoopChainPhase) => void;
  onRemovePhase: () => void;
  onAddBlock: () => void;
  onRemoveBlock: (blockId: string) => void;
  isLast: boolean;
  onAddPhase: () => void;
}) {
  const [showLimits, setShowLimits] = useState(false);
  const setBlock = (blockId: string, updater: (block: LoopChainBlock) => LoopChainBlock) =>
    onChange((current) => ({ ...current, blocks: current.blocks.map((block) => (block.id === blockId ? updater(block) : block)) }));

  return (
    <div>
      <div className="relative mb-3 mt-6 first:mt-1">
        <span className="absolute -left-[22px] top-0.5 size-3 rounded-full border-2 border-background bg-foreground ring-2 ring-border" aria-hidden="true" />
        <div className="flex items-center gap-2">
          <Input
            value={phase.name ?? ""}
            onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))}
            aria-label="Phase name"
            className="h-7 flex-1 border-0 bg-transparent px-0 text-xs font-bold uppercase tracking-wide text-muted-foreground shadow-none focus-visible:ring-0"
          />
          <button type="button" onClick={() => setShowLimits((value) => !value)} className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground">
            limits <ChevronDown className={cn("size-3 transition-transform", showLimits && "rotate-180")} />
          </button>
          {phaseCount > 1 && (
            <Button type="button" variant="ghost" size="icon" aria-label="Remove phase" onClick={onRemovePhase}>
              <Trash2 className="size-4" />
            </Button>
          )}
        </div>
        {showLimits && (
          <div className="mt-3 flex flex-col gap-3 rounded-md border bg-muted/20 p-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <NumberField label="Max steps" value={phase.limits.max_steps} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_steps: value } }))} />
              <NumberField label="Max rounds" value={phase.limits.max_rounds} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_rounds: value } }))} />
              <NumberField label="No-progress rounds" value={phase.limits.no_progress_stalls} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, no_progress_stalls: value } }))} />
              <NumberField label="Max wait (seconds)" value={phase.limits.max_wait_seconds ?? 0} min={0} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_wait_seconds: value || undefined } }))} />
            </div>
            <Field label="Starts when issue status is (optional)">
              <Input value={phase.status ?? ""} onChange={(event) => onChange((current) => ({ ...current, status: event.target.value || undefined }))} placeholder="in_progress" />
            </Field>
          </div>
        )}
      </div>

      {phase.blocks.map((block) => (
        <StepCard
          key={block.id}
          block={block}
          open={openId === block.id}
          dragging={dragId === block.id}
          dropHint={dropHint?.id === block.id ? dropHint.above ? "above" : "below" : null}
          canDelete={phase.blocks.length > 1}
          onToggle={() => onToggle(block.id)}
          onStartDrag={onStartDrag(block.id)}
          onNudge={(offset) => onNudge(block.id, offset)}
          onChange={(updater) => setBlock(block.id, updater)}
          onDelete={() => onRemoveBlock(block.id)}
        />
      ))}

      <div className="relative mb-3">
        <button type="button" onClick={onAddBlock} className="flex min-h-11 w-full items-center justify-center gap-2 rounded-xl border border-dashed text-xs text-muted-foreground hover:bg-muted/40">
          <Plus className="size-4" /> Add step
        </button>
      </div>

      {isLast && (
        <div className="relative mb-3">
          <button type="button" onClick={onAddPhase} className="flex min-h-11 w-full items-center justify-center gap-2 rounded-xl border border-dotted text-xs text-muted-foreground hover:bg-muted/40">
            <Plus className="size-4" /> Add phase
          </button>
        </div>
      )}
    </div>
  );
}

function StepCard({
  block,
  open,
  dragging,
  dropHint,
  canDelete,
  onToggle,
  onStartDrag,
  onNudge,
  onChange,
  onDelete,
}: {
  block: LoopChainBlock;
  open: boolean;
  dragging: boolean;
  dropHint: "above" | "below" | null;
  canDelete: boolean;
  onToggle: () => void;
  onStartDrag: (event: React.PointerEvent) => void;
  onNudge: (offset: -1 | 1) => void;
  onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void;
  onDelete: () => void;
}) {
  const meta = blockMeta(block.type);
  return (
    <div
      data-step-id={block.id}
      className={cn(
        "relative mb-3",
        dragging && "opacity-40",
      )}
    >
      <span className="absolute -left-[19px] top-5 size-2.5 rounded-full border-2 border-foreground bg-background" aria-hidden="true" />
      <div
        className={cn(
          "overflow-hidden rounded-xl border bg-card shadow-sm",
          dropHint === "above" && "shadow-[0_-3px_0_0_var(--color-foreground)]",
          dropHint === "below" && "shadow-[0_3px_0_0_var(--color-foreground)]",
        )}
      >
        <div className="flex min-h-12 items-center gap-2.5 px-3 py-3">
          <button
            type="button"
            aria-label="Drag to reorder"
            onPointerDown={onStartDrag}
            onKeyDown={(event) => {
              if (event.key === "ArrowUp") { event.preventDefault(); onNudge(-1); }
              if (event.key === "ArrowDown") { event.preventDefault(); onNudge(1); }
            }}
            className="flex size-7 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground/60 hover:text-muted-foreground active:cursor-grabbing"
            style={{ touchAction: "none" }}
          >
            <GripVertical className="size-4" />
          </button>
          <span className={cn("size-2.5 shrink-0 rounded-full", DOT_CLASS[meta.kind])} aria-hidden="true" />
          <button type="button" onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2.5 text-left">
            <span className="min-w-0 flex-1">
              <span className="block break-words text-sm font-semibold">{block.name || meta.label}</span>
              <span className="mt-0.5 block break-words text-xs text-muted-foreground">{blockSummary(block)}</span>
            </span>
            <span className={cn("shrink-0 rounded-md px-2 py-1 text-[10px] font-bold uppercase tracking-wide", BADGE_CLASS[meta.kind])}>{meta.label}</span>
            <ChevronDown className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} />
          </button>
        </div>
        {open && (
          <div className="flex flex-col gap-4 border-t px-3.5 pb-4 pt-4">
            <Field label="Step type">
              <div className="flex flex-wrap gap-1.5">
                {BLOCK_TYPES.map((option) => {
                  const OptionIcon = TYPE_ICON[option.value];
                  const selected = block.type === option.value;
                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => onChange((current) => ({ ...newBlock(option.value), id: current.id, name: current.name || blockMeta(option.value).label }))}
                      className={cn(
                        "inline-flex min-h-9 items-center gap-1.5 rounded-lg border px-3 text-[13px]",
                        selected ? "border-foreground font-semibold text-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]" : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      <OptionIcon className="size-3.5" /> {option.label}
                    </button>
                  );
                })}
              </div>
              <p className="text-xs text-muted-foreground">{meta.hint}</p>
            </Field>

            <Field label="Name">
              <Input value={block.name ?? ""} onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))} placeholder="Name this step" />
            </Field>

            <BlockFields block={block} onChange={onChange} />

            <AdvancedDisclosure block={block} onChange={onChange} />

            <div className="flex items-center border-t pt-3">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="ml-auto text-destructive hover:text-destructive"
                disabled={!canDelete}
                onClick={onDelete}
              >
                <Trash2 className="size-3.5" /> Delete step
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function BlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  if (block.type === "session") {
    return (
      <>
        <AgentCandidates block={block} onChange={onChange} />
        <Field label="Skill"><SkillNamePicker value={block.skill ?? ""} onChange={(skill) => onChange((current) => ({ ...current, skill }))} placeholder="Select the skill this session runs" /></Field>
        <Field label="What should this step accomplish?"><Textarea rows={3} value={block.goal ?? ""} onChange={(event) => onChange((current) => ({ ...current, goal: event.target.value }))} placeholder="Describe the goal…" /></Field>
        <AllowMoreSteps block={block} onChange={onChange} />
      </>
    );
  }
  if (block.type === "command") {
    return (
      <Field label="Command">
        <Input value={(block.check ?? []).join(" ")} onChange={(event) => onChange((current) => ({ ...current, check: event.target.value.trim().split(/\s+/).filter(Boolean), expect: "exit_zero" }))} placeholder="make test" />
        <p className="text-xs text-muted-foreground">Must exit with code 0.</p>
      </Field>
    );
  }
  if (block.type === "review") {
    return (
      <>
        <AgentCandidates block={block} onChange={onChange} />
        <Field label="Review skill (optional)"><SkillNamePicker value={block.skill ?? ""} onChange={(skill) => onChange((current) => ({ ...current, skill }))} placeholder="Leave empty to use the rubric" /></Field>
        <Field label="What should they check?"><Textarea rows={3} value={block.rubric ?? ""} onChange={(event) => onChange((current) => ({ ...current, rubric: event.target.value }))} placeholder="Review against the plan…" /></Field>
        <AllowMoreSteps block={block} onChange={onChange} />
      </>
    );
  }
  if (block.type === "human") {
    return (
      <>
        <Field label="Approver"><AssigneePicker assigneeType={block.approver_id ? block.approver_type ?? null : null} assigneeId={block.approver_id ?? null} onUpdate={(update) => onChange((current) => ({ ...current, approver_type: update.assignee_type === "agent" || update.assignee_type === "member" ? update.assignee_type : undefined, approver_id: update.assignee_id ?? undefined }))} align="start" /></Field>
        <Field label="Approval request"><Textarea rows={3} value={block.prompt ?? ""} onChange={(event) => onChange((current) => ({ ...current, prompt: event.target.value }))} placeholder="What should the approver confirm?" /></Field>
      </>
    );
  }
  if (block.type === "eval") {
    return (
      <>
        <Field label="Quality gate"><Input value={block.eval_key ?? ""} onChange={(event) => onChange((current) => ({ ...current, eval_key: event.target.value }))} placeholder="delivery-quality" /><p className="text-xs text-muted-foreground">The server scores the result — nobody can claim a pass.</p></Field>
        <Field label="Eval phase"><NativeSelect value={block.eval_phase ?? "delivery"} onChange={(event) => onChange((current) => ({ ...current, eval_phase: event.target.value as "plan" | "delivery" | "monitor" }))}><option value="plan">Plan</option><option value="delivery">Delivery</option><option value="monitor">Monitor</option></NativeSelect></Field>
      </>
    );
  }
  return null;
}

function AllowMoreSteps({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border bg-muted/20 p-3">
      <div>
        <p className="text-xs font-medium">Allow follow-up steps</p>
        <p className="text-[11px] text-muted-foreground">The agent may open another step of this same block.</p>
      </div>
      <div className="flex items-center gap-2">
        <Switch checked={block.steps?.allowed === true} onCheckedChange={(checked) => onChange((current) => ({ ...current, steps: { allowed: checked === true, max: checked ? current.steps?.max ?? 2 : undefined } }))} />
        {block.steps?.allowed && <Input aria-label="Maximum block steps" type="number" min={1} className="h-8 w-16" value={block.steps.max ?? 2} onChange={(event) => onChange((current) => ({ ...current, steps: { allowed: true, max: positive(event.target.value) } }))} />}
      </div>
    </div>
  );
}

function AdvancedDisclosure({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col gap-3">
      <button type="button" onClick={() => setOpen((value) => !value)} className="flex items-center gap-2 self-start text-xs text-muted-foreground hover:text-foreground">
        Advanced — block ID, engine keys <ChevronDown className={cn("size-3 transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <Field label="Stable block ID">
          <Input value={block.id} onChange={(event) => onChange((current) => ({ ...current, id: event.target.value }))} />
        </Field>
      )}
    </div>
  );
}

function AgentCandidates({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const agents = block.agents ?? [];
  return (
    <div className="flex flex-col gap-2 rounded-md border bg-muted/20 p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-medium">Who runs it?</p>
          <p className="text-[11px] text-muted-foreground">The first available agent runs this step.</p>
        </div>
        <Button type="button" size="sm" variant="outline" onClick={() => onChange((current) => ({ ...current, agents: [...(current.agents ?? []), { agent_id: "" }] }))}><Plus className="size-3.5" /> Add agent</Button>
      </div>
      {agents.length === 0 && <p className="text-[11px] text-muted-foreground">No agent pinned — use the issue assignee.</p>}
      {agents.map((agent, index) => (
        <div key={`${index}-${agent.agent_id}`} className="flex items-center gap-2">
          <span className="w-4 text-[10px] text-muted-foreground">{index + 1}</span>
          <div className="min-w-0 flex-1">
            <AgentPicker agentId={agent.agent_id || null} onChange={(agentId) => onChange((current) => ({ ...current, agents: (current.agents ?? []).map((item, itemIndex) => (itemIndex === index ? { ...item, agent_id: agentId } : item)) }))} />
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label="Remove agent" onClick={() => onChange((current) => ({ ...current, agents: (current.agents ?? []).filter((_, itemIndex) => itemIndex !== index) }))}><Trash2 className="size-4" /></Button>
        </div>
      ))}
      <Field label="When every agent is busy">
        <NativeSelect value={block.on_all_busy ?? "wait"} onChange={(event) => onChange((current) => ({ ...current, on_all_busy: event.target.value as LoopBusyPolicy }))}>
          {BUSY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </NativeSelect>
      </Field>
    </div>
  );
}

function positive(value: string) { return Math.max(1, Number.parseInt(value, 10) || 1); }

function NumberField({ label, value, min = 1, onChange }: { label: string; value: number; min?: number; onChange: (value: number) => void }) {
  return <Field label={label}><Input type="number" min={min} value={value} onChange={(event) => onChange(min === 0 ? Math.max(0, Number.parseInt(event.target.value, 10) || 0) : positive(event.target.value))} /></Field>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="flex flex-col gap-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}
