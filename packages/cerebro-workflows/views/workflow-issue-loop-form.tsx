"use client";

import { createContext, useContext, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { commandsListOptions } from "@multica/cerebro-commands";
import { cn } from "@multica/ui/lib/utils";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AgentPicker } from "@multica/views/autopilots/components/pickers/agent-picker";
import { AssigneePicker } from "@multica/views/issues/components";
import { ContentEditor } from "@multica/views/editor";
import { ChevronDown, Eye, Gauge, GripVertical, Play, Plus, SquareTerminal, Trash2, User } from "lucide-react";

import { MachineControlNotice } from "./workflow-issue-loop-chain";
import {
  BLOCK_TYPES,
  ISSUE_STATUS_OPTIONS,
  applyCommandSelection,
  issueStatusLabel,
  blockMeta,
  blockSummary,
  nudgeBlock,
  nudgePhase,
  reorderBlocks,
  reorderPhases,
  skillNamesFromPrompt,
  totalSteps,
  type LoopBlockKind,
} from "./loop-chain-model";
import {
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
  workflowEvalBindingsOptions,
} from "../core";
import type {
  WorkflowEvalBinding,
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

type EvalPhase = "plan" | "delivery" | "monitor";

// What each eval phase actually does to a run, in the user's words. Monitor can
// never block — the server drops blocking on monitor bindings (validation.go).
const EVAL_PHASE_HELP: Record<EvalPhase, string> = {
  plan: "Plan — gates the plan before any work starts.",
  delivery: "Delivery — gates the delivered result.",
  monitor: "Monitor — advisory only, it warns but never blocks the run.",
};

const EVAL_PHASE_LABEL: Record<EvalPhase, string> = {
  plan: "Plan",
  delivery: "Delivery",
  monitor: "Monitor",
};

// One radius per level, so the editor reads as three nested surfaces instead of
// four competing corner sizes: outer step card, controls, inner panels.
const CARD_RADIUS = "rounded-[14px]";
const CONTROL_RADIUS = "rounded-[10px]";
const PANEL_RADIUS = "rounded-lg";

// One control height, matching the shared Input (h-8 / 32px). The only taller
// controls are the full-width Add step / Add phase buttons at 44px.
const CONTROL_HEIGHT = "h-8";
// Input bakes in `md:text-sm`, and class merging does not drop a `md:` variant.
// Any font size set on an Input therefore needs its own md: twin or the desktop
// rendering silently falls back to 14px. These pairs are locked by a render test.
const INPUT_TEXT_SM = "text-sm md:text-sm";
const INPUT_TEXT_XS = "text-xs md:text-xs";

// A picker trigger that matches the Input siblings exactly, so a picker never
// falls back to its bare-link default inside a form field.
const PICKER_TRIGGER_CLASS = cn(
  "flex w-full items-center justify-between gap-2 border border-input bg-transparent px-2.5 text-left text-sm transition-colors hover:bg-accent/30",
  CONTROL_HEIGHT,
  CONTROL_RADIUS,
);

// Timeline geometry — one axis, everything derived from it. RAIL_CENTER_PX is
// the rail's centre line measured from the chain container's left edge;
// CHAIN_INSET_PX is that container's left padding (pl-7), which every row sits
// behind. Before this, the rail and the three dot sizes each carried their own
// hand-tuned offset and drifted apart by up to 4px.
const RAIL_CENTER_PX = 10;
const CHAIN_INSET_PX = 28;
const RAIL_WIDTH_PX = 2;
// Half of the step card's header row (min-h-12), so the step dot lands on the
// header's centre line rather than a fixed distance from the card's top.
const STEP_HEADER_CENTER_PX = 24;

function railDotStyle(sizePx: number): React.CSSProperties {
  return { left: RAIL_CENTER_PX - CHAIN_INSET_PX - sizePx / 2 };
}

function railLineStyle(): React.CSSProperties {
  return { left: RAIL_CENTER_PX - RAIL_WIDTH_PX / 2, width: RAIL_WIDTH_PX };
}

// Two-tone split from the approved mockup: machine steps read teal, human
// steps read violet. The colours live as component-scoped CSS variables set on
// the editor root through a scoped <style> element rather than an inline style
// object, because inline styles cannot express a dark-theme variant and beat
// every stylesheet rule on specificity. Nothing outside this component is
// touched, so no shared design token moves.
const DOT_CLASS: Record<LoopBlockKind, string> = {
  machine: "bg-[var(--wf-machine)]",
  human: "bg-[var(--wf-human)]",
};
const BADGE_CLASS: Record<LoopBlockKind, string> = {
  machine: "bg-[var(--wf-machine-soft)] text-[var(--wf-machine)]",
  human: "bg-[var(--wf-human-soft)] text-[var(--wf-human)]",
};

// Exact approved-mockup teal + violet expressed as rgb() so the guard's
// hex/palette-class check never trips; alpha-baked soft fills stay theme-safe.
// The dark variants are lifted so 12px bold badge text clears 4.5:1 against the
// dark card surface — the light-theme pair only reaches ~3.1:1 there.
export const WF_STEP_COLOR_CSS = `
[data-wf-editor] {
  --wf-machine: rgb(14 116 144);
  --wf-machine-soft: rgb(14 116 144 / 0.12);
  --wf-human: rgb(124 58 237);
  --wf-human-soft: rgb(124 58 237 / 0.14);
  --wf-rail: var(--color-border);
}
.dark [data-wf-editor] {
  --wf-machine: rgb(103 232 249);
  --wf-machine-soft: rgb(103 232 249 / 0.16);
  --wf-human: rgb(196 181 253);
  --wf-human-soft: rgb(196 181 253 / 0.18);
  --wf-rail: rgb(255 255 255 / 0.22);
}
`;

// Workflow-scoped context so the deeply nested step fields can reach the two
// facts they need (which workflow is being edited, and where Workflow gates
// lives) without threading props through four component levels.
interface EditorScope {
  workflowId?: string;
  gatesHref: string;
  commandsHref: string;
}

const EditorScopeContext = createContext<EditorScope>({ gatesHref: "", commandsHref: "" });

function key(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 9)}`;
}

// A new Human step is born with a usable request instead of an empty box.
export const DEFAULT_APPROVAL_PROMPT = `Work completed:
{{previous.output}}

Evidence:
{{previous.evidence}}

Approval requested:
Confirm the work is correct and the workflow may continue.`;

function newBlock(type: LoopBlockType = "session"): LoopChainBlock {
  const id = key(type);
  const block: LoopChainBlock = { id, type, name: blockMeta(type).label };
  if (type === "session" || type === "review") {
    block.agents = [];
    block.on_all_busy = "wait";
  }
  if (type === "command") block.expect = "exit_zero";
  if (type === "eval") block.eval_phase = "delivery";
  if (type === "human") block.prompt = DEFAULT_APPROVAL_PROMPT;
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
        <div className={cn("max-w-lg border border-warning/40 bg-warning/10 p-5 text-sm", PANEL_RADIUS)}>
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
          onReorderPhase={(dragId, targetId, above) => setChain((chain) => ({ ...chain, phases: reorderPhases(chain.phases, dragId, targetId, above) }))}
          onNudgePhase={(phaseId, offset) => setChain((chain) => ({ ...chain, phases: nudgePhase(chain.phases, phaseId, offset) }))}
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

        {error && <div className={cn("border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive", PANEL_RADIUS)}>{error}</div>}
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
          className={cn("h-8 border-0 bg-transparent px-0 font-semibold shadow-none focus-visible:ring-0", INPUT_TEXT_SM)}
        />
        <p className="truncate text-xs text-muted-foreground">
          Issue workflow · {steps} {steps === 1 ? "step" : "steps"}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <span className="text-xs text-muted-foreground">Enabled</span>
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
    <EditorScopeContext.Provider value={{ workflowId, gatesHref: `/${workspace.slug}/workflows/evals`, commandsHref: `/${workspace.slug}/workflows/commands` }}>
      <div data-wf-editor="" className="flex h-full flex-col">
        <style>{WF_STEP_COLOR_CSS}</style>
        <div className="sticky top-0 z-10 flex shrink-0 items-center border-b bg-background px-4 py-2.5 sm:px-6">{topBar}</div>
        {body}
        {footer}
      </div>
    </EditorScopeContext.Provider>
  );
}

// A drag in progress: either a step being moved between phases, or a whole
// phase being moved. One state, two kinds, so the two drags can never collide.
type DragKind = "step" | "phase";
interface DragHint {
  kind: DragKind;
  id: string;
  above: boolean;
}

interface RailProps {
  chain: LoopChainSpec;
  openId: string | null;
  onToggle: (blockId: string) => void;
  onReorder: (dragId: string, targetId: string, above: boolean) => void;
  onNudge: (blockId: string, offset: -1 | 1) => void;
  onReorderPhase: (dragId: string, targetId: string, above: boolean) => void;
  onNudgePhase: (phaseId: string, offset: -1 | 1) => void;
  onPhaseChange: (phaseId: string, updater: (phase: LoopChainPhase) => LoopChainPhase) => void;
  onRemovePhase: (phaseId: string) => void;
  onAddPhase: () => void;
  onAddBlock: (phaseId: string) => void;
  onRemoveBlock: (phaseId: string, blockId: string) => void;
  doneStatus: string;
  onDoneStatusChange: (value: string) => void;
}

// Exported as the editor's render-test seam: it is the whole chain surface
// (rail, phases, step cards) without the workspace/query/navigation shell.
export function ChainRail(props: RailProps) {
  const { chain, openId, onReorder, onNudge, onReorderPhase, onNudgePhase } = props;
  // Pointer-based drag reorder (mouse + touch), matching the approved mockup.
  const [drag, setDrag] = useState<{ kind: DragKind; id: string } | null>(null);
  const [dropHint, setDropHint] = useState<DragHint | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  const startDrag = (kind: DragKind, dragId: string) => (event: React.PointerEvent) => {
    event.preventDefault();
    setDrag({ kind, id: dragId });
    const onMove = (moveEvent: PointerEvent) => {
      const targets = listRef.current?.querySelectorAll<HTMLElement>(`[data-drag-kind="${kind}"]`);
      if (!targets) return;
      let hint: DragHint | null = null;
      targets.forEach((element) => {
        const id = element.dataset.dragId;
        if (!id || id === dragId) return;
        const rect = element.getBoundingClientRect();
        if (moveEvent.clientY >= rect.top && moveEvent.clientY <= rect.bottom) {
          hint = { kind, id, above: moveEvent.clientY < rect.top + rect.height / 2 };
        }
      });
      setDropHint(hint);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      setDropHint((current) => {
        if (current?.kind === "step") onReorder(dragId, current.id, current.above);
        if (current?.kind === "phase") onReorderPhase(dragId, current.id, current.above);
        return null;
      });
      setDrag(null);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  return (
    <div ref={listRef} data-testid="issue-loop-chain" className="relative pl-7">
      <div className="relative">
        <span className="absolute top-3 bottom-0 rounded bg-[var(--wf-rail)]" style={railLineStyle()} aria-hidden="true" />
        {chain.phases.map((phase, phaseIndex) => (
          <PhaseGroup
            key={phase.id}
            phase={phase}
            phaseCount={chain.phases.length}
            openId={openId}
            drag={drag}
            dropHint={dropHint}
            onToggle={props.onToggle}
            onStartDrag={startDrag}
            onNudge={onNudge}
            onNudgePhase={onNudgePhase}
            onChange={(updater) => props.onPhaseChange(phase.id, updater)}
            onRemovePhase={() => props.onRemovePhase(phase.id)}
            onAddBlock={() => props.onAddBlock(phase.id)}
            onRemoveBlock={(blockId) => props.onRemoveBlock(phase.id, blockId)}
            isLast={phaseIndex === chain.phases.length - 1}
            onAddPhase={props.onAddPhase}
          />
        ))}
      </div>
      {/* The finish row carries its own rail segment: it bridges the mt-5 gap
          above and stops exactly on the dot, so the rail never leaves a hole. */}
      <div className="relative mt-5 flex flex-wrap items-center gap-x-1 pl-1 text-xs text-muted-foreground">
        <span className="absolute -top-5 h-[calc(50%+1.25rem)] rounded bg-[var(--wf-rail)]" style={railLineStyle()} aria-hidden="true" />
        <span className="absolute top-1/2 size-2.5 -translate-y-1/2 rounded-full bg-foreground" style={railDotStyle(10)} aria-hidden="true" />
        <span>When the chain finishes → issue becomes</span>
        <IssueStatusSelect
          value={props.doneStatus}
          onChange={(status) => props.onDoneStatusChange(status ?? "done")}
          ariaLabel="Done status"
          clearable={false}
          className="ml-1 w-36 text-xs"
        />
      </div>
    </div>
  );
}

function PhaseGroup({
  phase,
  phaseCount,
  openId,
  drag,
  dropHint,
  onToggle,
  onStartDrag,
  onNudge,
  onNudgePhase,
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
  drag: { kind: DragKind; id: string } | null;
  dropHint: DragHint | null;
  onToggle: (blockId: string) => void;
  onStartDrag: (kind: DragKind, id: string) => (event: React.PointerEvent) => void;
  onNudge: (blockId: string, offset: -1 | 1) => void;
  onNudgePhase: (phaseId: string, offset: -1 | 1) => void;
  onChange: (updater: (phase: LoopChainPhase) => LoopChainPhase) => void;
  onRemovePhase: () => void;
  onAddBlock: () => void;
  onRemoveBlock: (blockId: string) => void;
  isLast: boolean;
  onAddPhase: () => void;
}) {
  const [showSettings, setShowSettings] = useState(false);
  const setBlock = (blockId: string, updater: (block: LoopChainBlock) => LoopChainBlock) =>
    onChange((current) => ({ ...current, blocks: current.blocks.map((block) => (block.id === blockId ? updater(block) : block)) }));

  const draggingThisPhase = drag?.kind === "phase" && drag.id === phase.id;
  // While a step is being dragged, highlight the phase it would land in — the
  // old feedback only marked one card and never said which phase you were in.
  const landingPhase =
    drag?.kind === "step" && dropHint?.kind === "step" && phase.blocks.some((block) => block.id === dropHint.id);
  const phaseDropHint = dropHint?.kind === "phase" && dropHint.id === phase.id ? dropHint : null;

  return (
    <div
      data-drag-kind="phase"
      data-drag-id={phase.id}
      className={cn(
        "relative",
        draggingThisPhase && "opacity-40",
        landingPhase && cn("bg-[var(--wf-machine-soft)] ring-1 ring-[var(--wf-machine)]/40", PANEL_RADIUS),
      )}
    >
      {phaseDropHint && <DropIndicator position={phaseDropHint.above ? "above" : "below"} />}
      <div className="mb-3 mt-6 first:mt-1">
        <div className="relative flex items-center gap-2">
          <span
            className="absolute top-1/2 size-3 -translate-y-1/2 rounded-full border-2 border-background bg-foreground ring-2 ring-border"
            style={railDotStyle(12)}
            aria-hidden="true"
          />
          <button
            type="button"
            aria-label="Drag to reorder phase"
            onPointerDown={onStartDrag("phase", phase.id)}
            onKeyDown={(event) => {
              if (event.key === "ArrowUp") { event.preventDefault(); onNudgePhase(phase.id, -1); }
              if (event.key === "ArrowDown") { event.preventDefault(); onNudgePhase(phase.id, 1); }
            }}
            className="flex size-9 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground hover:text-foreground active:cursor-grabbing sm:size-7"
            style={{ touchAction: "none" }}
          >
            <GripVertical className="size-4" />
          </button>
          <Input
            value={phase.name ?? ""}
            onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))}
            aria-label="Phase name"
            placeholder="Name this phase"
            className={cn(
              // Reads as the static heading it is at rest, but shows a real
              // field on hover and focus so it is discoverably editable.
              "h-7 flex-1 border-transparent bg-transparent px-1.5 font-bold uppercase tracking-wide text-muted-foreground shadow-none transition-colors hover:border-input hover:bg-background focus-visible:border-ring focus-visible:bg-background",
              CONTROL_RADIUS,
              INPUT_TEXT_XS,
            )}
          />
          <button type="button" onClick={() => setShowSettings((value) => !value)} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            Settings <ChevronDown className={cn("size-3 transition-transform", showSettings && "rotate-180")} />
          </button>
          {phaseCount > 1 && (
            <Button type="button" variant="ghost" size="icon" aria-label="Remove phase" onClick={onRemovePhase}>
              <Trash2 className="size-4" />
            </Button>
          )}
        </div>
        {showSettings && (
          <div className={cn("mt-3 flex flex-col gap-3 border bg-muted/20 p-3", PANEL_RADIUS)}>
            <div className="grid gap-3 sm:grid-cols-2">
              <NumberField label="Max steps" hint="How many steps this phase may open in total." value={phase.limits.max_steps} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_steps: value } }))} />
              <NumberField label="Max rounds" hint="How many times this phase may start over." value={phase.limits.max_rounds} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_rounds: value } }))} />
              <NumberField label="No-progress rounds" hint="How many rounds without progress before the phase stops." value={phase.limits.no_progress_stalls} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, no_progress_stalls: value } }))} />
              <NumberField label="Max wait (seconds)" hint="How long to wait for a free agent before giving up." value={phase.limits.max_wait_seconds ?? 0} min={0} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_wait_seconds: value || undefined } }))} />
            </div>
            <Field label="Issue status while this phase runs" hint="Set before the phase opens its first step. Leave unchanged to keep the status the previous phase left behind.">
              <IssueStatusSelect
                value={phase.status}
                onChange={(status) => onChange((current) => ({ ...current, status }))}
                ariaLabel="Phase status"
              />
            </Field>
          </div>
        )}
      </div>

      {phase.blocks.map((block) => (
        <StepCard
          key={block.id}
          block={block}
          open={openId === block.id}
          dragging={drag?.kind === "step" && drag.id === block.id}
          dropHint={dropHint?.kind === "step" && dropHint.id === block.id ? (dropHint.above ? "above" : "below") : null}
          canDelete={phase.blocks.length > 1}
          onToggle={() => onToggle(block.id)}
          onStartDrag={onStartDrag("step", block.id)}
          onNudge={(offset) => onNudge(block.id, offset)}
          onChange={(updater) => setBlock(block.id, updater)}
          onDelete={() => onRemoveBlock(block.id)}
        />
      ))}

      <div className="relative mb-3">
        <button type="button" onClick={onAddBlock} className={cn("flex min-h-11 w-full items-center justify-center gap-2 border border-dashed text-xs text-muted-foreground hover:bg-muted/40", CARD_RADIUS)}>
          <Plus className="size-4" /> Add step
        </button>
      </div>

      {isLast && (
        <div className="relative mb-3">
          <button type="button" onClick={onAddPhase} className={cn("flex min-h-11 w-full items-center justify-center gap-2 border border-dotted text-xs text-muted-foreground hover:bg-muted/40", CARD_RADIUS)}>
            <Plus className="size-4" /> Add phase
          </button>
        </div>
      )}
    </div>
  );
}

// A single continuous landing line drawn across the whole row, replacing the
// old 3px box-shadow that only appeared on one card edge.
function DropIndicator({ position }: { position: "above" | "below" }) {
  return (
    <span
      data-testid="drop-indicator"
      aria-hidden="true"
      className={cn(
        "pointer-events-none absolute inset-x-0 z-10 h-0.5 rounded-full bg-foreground",
        position === "above" ? "-top-1" : "-bottom-1",
      )}
      style={{ left: RAIL_CENTER_PX - CHAIN_INSET_PX }}
    />
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
      data-drag-kind="step"
      data-drag-id={block.id}
      className={cn(
        "relative mb-3",
        dragging && "opacity-40",
      )}
    >
      {dropHint && <DropIndicator position={dropHint} />}
      <span
        className="absolute size-2.5 -translate-y-1/2 rounded-full border-2 border-foreground bg-background"
        style={{ top: STEP_HEADER_CENTER_PX, ...railDotStyle(10) }}
        aria-hidden="true"
      />
      <div className={cn("overflow-hidden border bg-card shadow-sm", CARD_RADIUS)}>
        <div className="flex min-h-12 items-center gap-2.5 px-3.5 py-3">
          <button
            type="button"
            aria-label="Drag to reorder"
            onPointerDown={onStartDrag}
            onKeyDown={(event) => {
              if (event.key === "ArrowUp") { event.preventDefault(); onNudge(-1); }
              if (event.key === "ArrowDown") { event.preventDefault(); onNudge(1); }
            }}
            className="flex size-9 shrink-0 cursor-grab touch-none items-center justify-center text-muted-foreground hover:text-foreground active:cursor-grabbing sm:size-7"
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
            <span className={cn("shrink-0 px-2 py-1 text-xs font-bold uppercase tracking-wide", CONTROL_RADIUS, BADGE_CLASS[meta.kind])}>{meta.label}</span>
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
                      onClick={() => onChange((current) => ({
                        ...newBlock(option.value),
                        id: current.id,
                        name: current.name || blockMeta(option.value).label,
                        // Statuses belong to the step's place in the chain, not
                        // to what it does, so they survive a type change.
                        status_on_start: current.status_on_start,
                        status_on_done: current.status_on_done,
                      }))}
                      className={cn(
                        "inline-flex items-center gap-1.5 border px-3 text-sm",
                        CONTROL_HEIGHT,
                        CONTROL_RADIUS,
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
              <Input value={block.name ?? ""} onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))} placeholder="Name this step" className={cn(CONTROL_HEIGHT, CONTROL_RADIUS, INPUT_TEXT_SM)} />
            </Field>

            <BlockFields block={block} onChange={onChange} />

            <StepStatusFields block={block} onChange={onChange} />

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

/**
 * Per-step status control. Until now the only status a chain could set was the
 * phase's and the chain's final one, so a run could not show "in review" while
 * a review step waited. The engine applies these at the step boundary — never
 * while the step runs — so the board follows the run without flapping.
 */
export function StepStatusFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return (
    <div className={cn("grid gap-3 border bg-muted/20 p-3 sm:grid-cols-2", PANEL_RADIUS)}>
      <Field label="Before this step → issue becomes">
        <IssueStatusSelect
          value={block.status_on_start}
          onChange={(status) => onChange((current) => ({ ...current, status_on_start: status }))}
          ariaLabel="Status before this step"
        />
      </Field>
      <Field label="After this step → issue becomes">
        <IssueStatusSelect
          value={block.status_on_done}
          onChange={(status) => onChange((current) => ({ ...current, status_on_done: status }))}
          ariaLabel="Status after this step"
        />
      </Field>
    </div>
  );
}

function BlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  if (block.type === "session") {
    return <SessionBlockFields block={block} onChange={onChange} />;
  }
  if (block.type === "command") {
    return <CommandBlockFields block={block} onChange={onChange} />;
  }
  if (block.type === "review") {
    return <ReviewBlockFields block={block} onChange={onChange} />;
  }
  if (block.type === "human") {
    return (
      <>
        <Field label="Approver">
          <AssigneePicker
            assigneeType={block.approver_id ? block.approver_type ?? null : null}
            assigneeId={block.approver_id ?? null}
            onUpdate={(update) => onChange((current) => ({ ...current, approver_type: update.assignee_type === "agent" || update.assignee_type === "member" ? update.assignee_type : undefined, approver_id: update.assignee_id ?? undefined }))}
            align="start"
            triggerRender={<button type="button" className={PICKER_TRIGGER_CLASS} />}
          />
        </Field>
        <ApprovalTemplateField block={block} onChange={onChange} />
      </>
    );
  }
  if (block.type === "eval") {
    return <EvalBlockFields block={block} onChange={onChange} />;
  }
  return null;
}

export function ApprovalTemplateField({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return <Field label="Approval request template" hint="The workflow fills {{previous.output}} and {{previous.evidence}} from the preceding agent step before asking for approval."><Textarea rows={8} value={block.prompt ?? ""} onChange={(event) => onChange((current) => ({ ...current, prompt: event.target.value }))} placeholder="What should the approver confirm?" /></Field>;
}

function CommandBlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const scope = useContext(EditorScopeContext);
  if (!scope.commandsHref) return <CommandFields block={block} commands={[]} commandsHref="" onChange={onChange} />;
  return <ConnectedCommandFields block={block} commandsHref={scope.commandsHref} onChange={onChange} />;
}

function ConnectedCommandFields({ block, commandsHref, onChange }: { block: LoopChainBlock; commandsHref: string; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const wsId = useWorkspaceId();
  const { data: commands = [] } = useQuery(commandsListOptions(wsId));
  return <CommandFields block={block} commands={commands} commandsHref={commandsHref} onChange={onChange} />;
}

function CommandFields({ block, commands, commandsHref, onChange }: { block: LoopChainBlock; commands: Array<{ id: string; title: string; argv: string[] }>; commandsHref: string; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const selected = commands.find((command) => command.id === block.command_id);
  return (
    <>
      <Field label="Library command" hint="Choose a reusable command, or keep Custom command for this workflow only.">
        <Select
          value={selected?.id ?? "custom"}
          onValueChange={(value) => {
            if (value === "custom") {
              onChange((current) => ({ ...current, command_id: undefined }));
              return;
            }
            const command = commands.find((item) => item.id === value);
            if (command) onChange((current) => applyCommandSelection(current, command));
          }}
        >
          <SelectTrigger className={cn("w-full", CONTROL_HEIGHT, CONTROL_RADIUS)} aria-label="Library command"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="custom">Custom command</SelectItem>{commands.map((command) => <SelectItem key={command.id} value={command.id}>{command.title}</SelectItem>)}</SelectContent>
        </Select>
        {commands.length === 0 && <p className="text-xs text-muted-foreground">No reusable commands yet. {commandsHref && <AppLink href={commandsHref} className="font-medium underline underline-offset-2">Open Command library</AppLink>}</p>}
      </Field>
      <Field label="Resolved arguments">
        <Input value={(block.check ?? []).join(" ")} onChange={(event) => onChange((current) => ({ ...current, command_id: undefined, check: event.target.value.trim().split(/\s+/).filter(Boolean), expect: "exit_zero" }))} placeholder="make test" className={cn(CONTROL_HEIGHT, CONTROL_RADIUS, INPUT_TEXT_SM)} />
        <p className="text-xs text-muted-foreground">Stored with this workflow and must exit with code 0.</p>
      </Field>
    </>
  );
}

function SkillPromptEditor({
  value,
  placeholder,
  onChange,
}: {
  value: string;
  placeholder: string;
  onChange: (markdown: string, skills: string[]) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  return (
    <ContentEditor
      defaultValue={value}
      onUpdate={(markdown) => onChange(markdown, skillNamesFromPrompt(markdown, workspaceSkills))}
      placeholder={placeholder}
      // Same border, radius and focus ring as the Input/Textarea siblings in
      // this column — without them the goal field is the only control in the
      // step card that gives no feedback when it is focused.
      className={cn(
        "min-h-24 border border-input bg-transparent px-2.5 py-2 text-sm transition-colors",
        "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
        CONTROL_RADIUS,
      )}
      debounceMs={0}
      disableMentions
      enableSlashCommands
      showBubbleMenu={false}
    />
  );
}

function SessionBlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return (
    <>
      <AgentCandidates block={block} onChange={onChange} />
      <Field label="Goal and skills" hint="Type / in the goal to add every skill this step requires.">
        <SkillPromptEditor
          value={block.goal ?? ""}
          placeholder="Describe the goal and type / to add skills…"
          onChange={(goal, skills) => onChange((current) => ({ ...current, goal, skills, skill: undefined }))}
        />
      </Field>
      <AllowMoreSteps block={block} onChange={onChange} />
    </>
  );
}

function ReviewBlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return (
    <>
      <AgentCandidates block={block} onChange={onChange} />
      <Field label="Review brief and skills" hint="Type / to add one or more skills. Leave skills out to review only against the brief.">
        <SkillPromptEditor
          value={block.rubric ?? ""}
          placeholder="Describe what to review and type / to add skills…"
          onChange={(rubric, skills) => onChange((current) => ({ ...current, rubric, skills, skill: undefined }))}
        />
      </Field>
      <AllowMoreSteps block={block} onChange={onChange} />
    </>
  );
}

// Reads the workflow's own gate bindings so the quality-gate field can only
// offer keys the engine will actually resolve. Kept as a thin wrapper around
// the pure EvalGateFields below, which is what the tests render.
function EvalBlockFields({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const wsId = useWorkspaceId();
  const scope = useContext(EditorScopeContext);
  const { data: bindings = [] } = useQuery(workflowEvalBindingsOptions(wsId, scope.workflowId ?? ""));
  return <EvalGateFields block={block} onChange={onChange} bindings={bindings} gatesHref={scope.gatesHref} />;
}

// A gate is identified by key AND phase: the same eval can be bound to this
// workflow at more than one phase, and the engine resolves on the pair
// (block_runner.go matches e.eval_key AND b.phase). The picker therefore
// carries both in one option value.
export function gateOptionValue(evalKey: string, phase: string) {
  return [evalKey, phase].join("::");
}

// Applies a picked gate to the block. Both fields move together, so the phase
// can never end up contradicting the binding the key came from.
export function applyGateSelection(block: LoopChainBlock, optionValue: string): LoopChainBlock {
  const [evalKey = "", phase = "delivery"] = optionValue.split("::");
  if (!evalKey) return block;
  return { ...block, eval_key: evalKey, eval_phase: phase as EvalPhase };
}

/**
 * The Eval step's fields. A quality gate is picked from the gates already bound
 * to this workflow, never typed free-hand: an unbound key used to save fine and
 * only blow up mid-run as `resolve eval block "…": no rows in result set`.
 * The phase is not an independent choice either — it comes from the binding.
 */
export function EvalGateFields({
  block,
  bindings,
  gatesHref,
  onChange,
}: {
  block: LoopChainBlock;
  bindings: WorkflowEvalBinding[];
  gatesHref: string;
  onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void;
}) {
  const selectedKey = block.eval_key ?? "";
  const selectedPhase = (block.eval_phase ?? "delivery") as EvalPhase;
  const selectedValue = selectedKey ? gateOptionValue(selectedKey, selectedPhase) : "";
  const stillBound = bindings.some(
    (binding) => binding.eval_key === selectedKey && binding.phase === selectedPhase,
  );

  if (bindings.length === 0) {
    return (
      <Field label="Quality gate">
        <div className={cn("border border-warning/40 bg-warning/10 p-3 text-xs", PANEL_RADIUS)}>
          <p className="font-medium text-foreground">No quality gate is bound to this workflow yet.</p>
          <p className="mt-1 text-muted-foreground">
            An Eval step can only run a gate that is bound to this workflow. Bind one first, then pick it here.
          </p>
          <AppLink href={gatesHref} className="mt-2 inline-block font-medium underline underline-offset-2">
            Open Workflow gates
          </AppLink>
        </div>
      </Field>
    );
  }

  return (
    <>
      <Field label="Quality gate">
        <Select
          value={selectedValue}
          onValueChange={(value) => {
            if (typeof value !== "string" || !value) return;
            onChange((current) => applyGateSelection(current, value));
          }}
        >
          <SelectTrigger aria-label="Quality gate" className={cn("w-full", CONTROL_HEIGHT, CONTROL_RADIUS)}>
            <SelectValue placeholder="Select a bound gate…" />
          </SelectTrigger>
          <SelectContent>
            {selectedKey && !stillBound && (
              <SelectItem value={selectedValue}>
                {selectedKey} — no longer bound to this workflow
              </SelectItem>
            )}
            {bindings.map((binding) => (
              <SelectItem key={binding.id} value={gateOptionValue(binding.eval_key, binding.phase)}>
                {binding.eval_title} · {binding.eval_key} · {EVAL_PHASE_LABEL[binding.phase as EvalPhase] ?? binding.phase}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">The server scores the result — nobody can claim a pass.</p>
      </Field>
      <Field label="Eval phase">
        <p className="text-sm">{EVAL_PHASE_LABEL[selectedPhase] ?? selectedPhase}</p>
        <p className="text-xs text-muted-foreground">
          {EVAL_PHASE_HELP[selectedPhase] ?? EVAL_PHASE_HELP.delivery} It follows the gate you picked and is set under{" "}
          <AppLink href={gatesHref} className="underline underline-offset-2">Workflow gates</AppLink>.
        </p>
      </Field>
    </>
  );
}

function AllowMoreSteps({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  return (
    <div className={cn("flex items-center justify-between gap-3 border bg-muted/20 p-3", PANEL_RADIUS)}>
      <div>
        <p className="text-xs font-medium">Allow follow-up steps</p>
        <p className="text-xs text-muted-foreground">The agent may open another step of this same block.</p>
      </div>
      <div className="flex items-center gap-2">
        <Switch checked={block.steps?.allowed === true} onCheckedChange={(checked) => onChange((current) => ({ ...current, steps: { allowed: checked === true, max: checked ? current.steps?.max ?? 2 : undefined } }))} />
        {block.steps?.allowed && <Input aria-label="Maximum block steps" type="number" min={1} className={cn("w-16", CONTROL_HEIGHT, CONTROL_RADIUS, INPUT_TEXT_SM)} value={block.steps.max ?? 2} onChange={(event) => onChange((current) => ({ ...current, steps: { allowed: true, max: positive(event.target.value) } }))} />}
      </div>
    </div>
  );
}

export function AgentCandidates({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const agents = block.agents ?? [];
  return (
    <div className={cn("flex flex-col gap-2 border bg-muted/20 p-3", PANEL_RADIUS)}>
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-medium">Who runs it?</p>
          <p className="text-xs text-muted-foreground">The first available agent runs this step.</p>
        </div>
      </div>
      {agents.length === 0 && <p className="text-xs text-muted-foreground">No agent pinned — use the issue assignee.</p>}
      {agents.map((agent, index) => (
        <div key={`${index}-${agent.agent_id}`} className="flex items-center gap-2">
          <span className="w-4 text-xs text-muted-foreground">{index + 1}</span>
          <div className="min-w-0 flex-1">
            <AgentPicker
              agentId={agent.agent_id || null}
              onChange={(agentId) => onChange((current) => ({ ...current, agents: (current.agents ?? []).map((item, itemIndex) => (itemIndex === index ? { ...item, agent_id: agentId } : item)) }))}
              triggerRender={<button type="button" className={PICKER_TRIGGER_CLASS} />}
            />
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label="Remove agent" onClick={() => onChange((current) => ({ ...current, agents: (current.agents ?? []).filter((_, itemIndex) => itemIndex !== index) }))}><Trash2 className="size-4" /></Button>
        </div>
      ))}
      <Button type="button" size="sm" variant="outline" className="self-start" onClick={() => onChange((current) => ({ ...current, agents: [...(current.agents ?? []), { agent_id: "" }] }))}><Plus className="size-3.5" /> Add agent</Button>
      <Field label="When every agent is busy">
        <Select
          value={block.on_all_busy ?? "wait"}
          onValueChange={(value) => {
            if (typeof value !== "string" || !value) return;
            onChange((current) => ({ ...current, on_all_busy: value as LoopBusyPolicy }));
          }}
        >
          <SelectTrigger aria-label="When every agent is busy" className={cn("w-full", CONTROL_HEIGHT, CONTROL_RADIUS)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {BUSY_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </Field>
    </div>
  );
}

function positive(value: string) { return Math.max(1, Number.parseInt(value, 10) || 1); }

function NumberField({ label, hint, value, min = 1, onChange }: { label: string; hint?: string; value: number; min?: number; onChange: (value: number) => void }) {
  return (
    <Field label={label} hint={hint}>
      <Input
        type="number"
        min={min}
        value={value}
        className={cn(CONTROL_HEIGHT, CONTROL_RADIUS, INPUT_TEXT_SM)}
        onChange={(event) => onChange(min === 0 ? Math.max(0, Number.parseInt(event.target.value, 10) || 0) : positive(event.target.value))}
      />
    </Field>
  );
}

// "Leave it alone" needs a value of its own: an empty string is not a
// selectable option value, so the sentinel carries it and is translated back
// to undefined on the way into the spec.
const STATUS_UNCHANGED = "__unchanged__";

/**
 * The one control that decides an issue status anywhere in this editor. Every
 * status the chain can set is picked from the board's own statuses — typing
 * one by hand used to be possible and produced a status the board does not
 * have, which the run then parked the issue in.
 */
export function IssueStatusSelect({
  value,
  onChange,
  ariaLabel,
  clearable = true,
  placeholder = "Leave unchanged",
  className,
}: {
  value: string | undefined;
  onChange: (status: string | undefined) => void;
  ariaLabel: string;
  clearable?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const current = value || (clearable ? STATUS_UNCHANGED : "");
  const known = ISSUE_STATUS_OPTIONS.some((option) => option.value === value);
  return (
    <Select value={current} onValueChange={(next: string | null | undefined) => onChange(!next || next === STATUS_UNCHANGED ? undefined : next)}>
      <SelectTrigger aria-label={ariaLabel} className={cn(CONTROL_HEIGHT, CONTROL_RADIUS, "w-full text-sm", className)}>
        {/* The trigger shows the board's own wording ("In progress"), not the
            stored key — Base UI renders the raw value unless it is mapped. */}
        <SelectValue placeholder={placeholder}>
          {(selected: string) => (!selected || selected === STATUS_UNCHANGED ? placeholder : issueStatusLabel(selected))}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {clearable && <SelectItem value={STATUS_UNCHANGED}>{placeholder}</SelectItem>}
        {/* A status saved by an older recipe stays selectable instead of being
            silently rewritten the first time the recipe is opened. */}
        {value && !known && <SelectItem value={value}>{issueStatusLabel(value)}</SelectItem>}
        {ISSUE_STATUS_OPTIONS.map((option) => (
          <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
