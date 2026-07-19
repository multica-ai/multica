"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AgentPicker } from "@multica/views/autopilots/components/pickers/agent-picker";
import { AssigneePicker } from "@multica/views/issues/components";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";

import { SkillNamePicker } from "./loop-pickers";
import { MachineControlNotice } from "./workflow-issue-loop-chain";
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

const BLOCK_OPTIONS: ReadonlyArray<{ value: LoopBlockType; label: string; hint: string }> = [
  { value: "session", label: "Session", hint: "Run a skill on the issue" },
  { value: "command", label: "Command", hint: "Run a machine-controlled check" },
  { value: "review", label: "Review", hint: "Ask an agent to review the work" },
  { value: "human", label: "Human approval", hint: "Wait for a person or agent to approve" },
  { value: "eval", label: "Eval", hint: "Require a server-scored eval to pass" },
];

const BUSY_OPTIONS: ReadonlyArray<{ value: LoopBusyPolicy; label: string }> = [
  { value: "wait", label: "Wait for an agent" },
  { value: "pause", label: "Pause the run" },
  { value: "wakeup", label: "Schedule a wakeup" },
  { value: "ping_member", label: "Notify a person" },
];

function key(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 9)}`;
}

function newBlock(type: LoopBlockType = "session"): LoopChainBlock {
  const id = key(type);
  const block: LoopChainBlock = { id, type, name: BLOCK_OPTIONS.find((item) => item.value === type)?.label };
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
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (detail.data) setForm(formFromWorkflow(detail.data));
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

  const setChain = (updater: (chain: LoopChainSpec) => LoopChainSpec) =>
    setForm((current) => ({ ...current, chain: updater(current.chain) }));
  const setPhase = (phaseId: string, updater: (phase: LoopChainPhase) => LoopChainPhase) =>
    setChain((chain) => ({ ...chain, phases: chain.phases.map((phase) => phase.id === phaseId ? updater(phase) : phase) }));

  const body = (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <form className="mx-auto flex max-w-3xl flex-col gap-5 p-6" onSubmit={(event) => {
        event.preventDefault();
        setError(null);
        save.mutate();
      }}>
        <Section title="Workflow">
          <Field label="Name">
            <Input required value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="E.g. Ship a feature end-to-end" />
          </Field>
          <div className="flex items-center justify-between gap-4 rounded-md border bg-muted/20 p-3">
            <div>
              <p className="text-xs font-medium">Enabled</p>
              <p className="text-[11px] text-muted-foreground">Allow new issues to start this chain.</p>
            </div>
            <Switch checked={form.enabled} onCheckedChange={(checked) => setForm({ ...form, enabled: checked === true })} />
          </div>
        </Section>

        <MachineControlNotice chain={form.chain} />

        <div className="flex flex-col gap-4" data-testid="issue-loop-chain">
          {form.chain.phases.map((phase, phaseIndex) => (
            <PhaseEditor
              key={phase.id}
              phase={phase}
              index={phaseIndex}
              phaseCount={form.chain.phases.length}
              onChange={(updater) => setPhase(phase.id, updater)}
              onMove={(offset) => setChain((chain) => ({ ...chain, phases: move(chain.phases, phaseIndex, phaseIndex + offset) }))}
              onRemove={() => setChain((chain) => ({ ...chain, phases: chain.phases.filter((item) => item.id !== phase.id) }))}
            />
          ))}
        </div>

        <Button type="button" variant="outline" className="self-start" onClick={() => setChain((chain) => ({ ...chain, phases: [...chain.phases, newPhase(chain.phases.length)] }))}>
          <Plus className="size-4" /> Add phase
        </Button>

        <Section title="Completion">
          <Field label="Done status">
            <Input value={form.chain.done_status ?? ""} onChange={(event) => setChain((chain) => ({ ...chain, done_status: event.target.value || undefined }))} placeholder="done" />
          </Field>
        </Section>

        {error && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{error}</div>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => navigation.push(`/${workspace.slug}/workflows`)}>Cancel</Button>
          <Button type="submit" disabled={save.isPending}>{save.isPending ? "Saving…" : "Save chain"}</Button>
        </div>
      </form>
    </div>
  );

  if (embedded) return body;
  return <div className="flex h-full flex-col">
    <PageHeader className="justify-between gap-3">
      <div className="flex min-w-0 flex-col">
        <h1 className="text-sm font-semibold">{workflowId ? "Edit Issue workflow" : "New Issue workflow"}</h1>
        <p className="truncate text-[11px] text-muted-foreground">Phases contain the ordered blocks the engine runs.</p>
      </div>
    </PageHeader>
    {body}
  </div>;
}

function PhaseEditor({ phase, index, phaseCount, onChange, onMove, onRemove }: {
  phase: LoopChainPhase;
  index: number;
  phaseCount: number;
  onChange: (updater: (phase: LoopChainPhase) => LoopChainPhase) => void;
  onMove: (offset: number) => void;
  onRemove: () => void;
}) {
  const setBlock = (blockId: string, updater: (block: LoopChainBlock) => LoopChainBlock) =>
    onChange((current) => ({ ...current, blocks: current.blocks.map((block) => block.id === blockId ? updater(block) : block) }));
  return <section className="rounded-lg border bg-card">
    <div className="flex items-center gap-2 border-b px-4 py-3">
      <span className="flex size-6 items-center justify-center rounded-full bg-foreground text-[11px] font-semibold text-background">{index + 1}</span>
      <Input className="h-8 flex-1 border-0 bg-transparent px-1 font-medium shadow-none" value={phase.name ?? ""} onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))} aria-label={`Phase ${index + 1} name`} />
      <OrderButtons index={index} count={phaseCount} label="phase" onMove={onMove} />
      {phaseCount > 1 && <IconButton label="Remove phase" onClick={onRemove}><Trash2 className="size-4" /></IconButton>}
    </div>
    <div className="flex flex-col gap-4 p-4">
      <div className="grid gap-3 sm:grid-cols-4">
        <NumberField label="Max steps" value={phase.limits.max_steps} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_steps: value } }))} />
        <NumberField label="Max rounds" value={phase.limits.max_rounds} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_rounds: value } }))} />
        <NumberField label="No-progress rounds" value={phase.limits.no_progress_stalls} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, no_progress_stalls: value } }))} />
        <NumberField label="Max wait (seconds)" value={phase.limits.max_wait_seconds ?? 0} min={0} onChange={(value) => onChange((current) => ({ ...current, limits: { ...current.limits, max_wait_seconds: value || undefined } }))} />
      </div>
      <Field label="Starts when issue status is (optional)">
        <Input value={phase.status ?? ""} onChange={(event) => onChange((current) => ({ ...current, status: event.target.value || undefined }))} placeholder="in_progress" />
      </Field>
      <div className="flex flex-col gap-3 border-l border-border/70 pl-4">
        {phase.blocks.map((block, blockIndex) => <BlockEditor
          key={block.id}
          block={block}
          index={blockIndex}
          count={phase.blocks.length}
          onChange={(updater) => setBlock(block.id, updater)}
          onMove={(offset) => onChange((current) => ({ ...current, blocks: move(current.blocks, blockIndex, blockIndex + offset) }))}
          onRemove={() => onChange((current) => ({ ...current, blocks: current.blocks.filter((item) => item.id !== block.id) }))}
        />)}
      </div>
      <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => onChange((current) => ({ ...current, blocks: [...current.blocks, newBlock()] }))}>
        <Plus className="size-4" /> Add block
      </Button>
    </div>
  </section>;
}

function BlockEditor({ block, index, count, onChange, onMove, onRemove }: {
  block: LoopChainBlock;
  index: number;
  count: number;
  onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void;
  onMove: (offset: number) => void;
  onRemove: () => void;
}) {
  const needsAgent = block.type === "session" || block.type === "review";
  return <div className="rounded-md border bg-background p-3">
    <div className="mb-3 flex items-center gap-2">
      <span className="font-mono text-[10px] text-muted-foreground">{String(index + 1).padStart(2, "0")}</span>
      <Input className="h-8 flex-1" value={block.name ?? ""} onChange={(event) => onChange((current) => ({ ...current, name: event.target.value }))} placeholder="Block name" />
      <OrderButtons index={index} count={count} label="block" onMove={onMove} />
      {count > 1 && <IconButton label="Remove block" onClick={onRemove}><Trash2 className="size-4" /></IconButton>}
    </div>
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label="Block type">
        <NativeSelect value={block.type} onChange={(event) => {
          const replacement = newBlock(event.target.value as LoopBlockType);
          onChange((current) => ({ ...replacement, id: current.id, name: current.name || replacement.name }));
        }}>
          {BLOCK_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label} — {option.hint}</option>)}
        </NativeSelect>
      </Field>
      <Field label="Stable block ID">
        <Input value={block.id} onChange={(event) => onChange((current) => ({ ...current, id: event.target.value }))} />
      </Field>
    </div>

    {needsAgent && <AgentCandidates block={block} onChange={onChange} />}
    {block.type === "session" && <>
      <Field label="Skill"><SkillNamePicker value={block.skill ?? ""} onChange={(skill) => onChange((current) => ({ ...current, skill }))} placeholder="Select the skill this session runs" /></Field>
      <Field label="Instruction"><Textarea rows={3} value={block.goal ?? ""} onChange={(event) => onChange((current) => ({ ...current, goal: event.target.value }))} placeholder="Describe what this session must accomplish." /></Field>
    </>}
    {block.type === "command" && <Field label="Command (argv, separated by spaces)"><Input value={(block.check ?? []).join(" ")} onChange={(event) => onChange((current) => ({ ...current, check: event.target.value.trim().split(/\s+/).filter(Boolean), expect: "exit_zero" }))} placeholder="make test" /></Field>}
    {block.type === "review" && <>
      <Field label="Review skill (optional)"><SkillNamePicker value={block.skill ?? ""} onChange={(skill) => onChange((current) => ({ ...current, skill }))} placeholder="Leave empty to use the rubric" /></Field>
      <Field label="Rubric"><Textarea rows={3} value={block.rubric ?? ""} onChange={(event) => onChange((current) => ({ ...current, rubric: event.target.value }))} placeholder="What should the reviewer check?" /></Field>
    </>}
    {block.type === "human" && <>
      <Field label="Approver"><AssigneePicker assigneeType={block.approver_id ? block.approver_type ?? null : null} assigneeId={block.approver_id ?? null} onUpdate={(update) => onChange((current) => ({ ...current, approver_type: update.assignee_type === "agent" || update.assignee_type === "member" ? update.assignee_type : undefined, approver_id: update.assignee_id ?? undefined }))} align="start" /></Field>
      <Field label="Approval request"><Textarea rows={3} value={block.prompt ?? ""} onChange={(event) => onChange((current) => ({ ...current, prompt: event.target.value }))} placeholder="What should the approver confirm?" /></Field>
    </>}
    {block.type === "eval" && <>
      <Field label="Eval key"><Input value={block.eval_key ?? ""} onChange={(event) => onChange((current) => ({ ...current, eval_key: event.target.value }))} placeholder="delivery-quality" /></Field>
      <Field label="Eval phase"><NativeSelect value={block.eval_phase ?? "delivery"} onChange={(event) => onChange((current) => ({ ...current, eval_phase: event.target.value as "plan" | "delivery" | "monitor" }))}><option value="plan">Plan</option><option value="delivery">Delivery</option><option value="monitor">Monitor</option></NativeSelect></Field>
    </>}

    {(block.type === "session" || block.type === "review") && <div className="mt-3 flex items-center justify-between gap-3 rounded-md border bg-muted/20 p-3">
      <div><p className="text-xs font-medium">Allow more steps</p><p className="text-[11px] text-muted-foreground">The agent may open another step of this same block.</p></div>
      <div className="flex items-center gap-2">
        <Switch checked={block.steps?.allowed === true} onCheckedChange={(checked) => onChange((current) => ({ ...current, steps: { allowed: checked === true, max: checked ? current.steps?.max ?? 2 : undefined } }))} />
        {block.steps?.allowed && <Input aria-label="Maximum block steps" type="number" min={1} className="h-8 w-16" value={block.steps.max ?? 2} onChange={(event) => onChange((current) => ({ ...current, steps: { allowed: true, max: positive(event.target.value) } }))} />}
      </div>
    </div>}
  </div>;
}

function AgentCandidates({ block, onChange }: { block: LoopChainBlock; onChange: (updater: (block: LoopChainBlock) => LoopChainBlock) => void }) {
  const agents = block.agents ?? [];
  return <div className="my-3 flex flex-col gap-2 rounded-md border bg-muted/20 p-3">
    <div className="flex items-center justify-between gap-3"><div><p className="text-xs font-medium">Agent preference</p><p className="text-[11px] text-muted-foreground">The first available agent runs this block.</p></div><Button type="button" size="sm" variant="outline" onClick={() => onChange((current) => ({ ...current, agents: [...(current.agents ?? []), { agent_id: "" }] }))}><Plus className="size-3.5" /> Add agent</Button></div>
    {agents.length === 0 && <p className="text-[11px] text-muted-foreground">No agent pinned — use the issue assignee.</p>}
    {agents.map((agent, index) => <div key={`${index}-${agent.agent_id}`} className="flex items-center gap-2">
      <span className="w-4 text-[10px] text-muted-foreground">{index + 1}</span>
      <div className="min-w-0 flex-1"><AgentPicker agentId={agent.agent_id || null} onChange={(agentId) => onChange((current) => ({ ...current, agents: (current.agents ?? []).map((item, itemIndex) => itemIndex === index ? { ...item, agent_id: agentId } : item) }))} /></div>
      <IconButton label="Remove agent" onClick={() => onChange((current) => ({ ...current, agents: (current.agents ?? []).filter((_, itemIndex) => itemIndex !== index) }))}><Trash2 className="size-4" /></IconButton>
    </div>)}
    <Field label="When every agent is busy"><NativeSelect value={block.on_all_busy ?? "wait"} onChange={(event) => onChange((current) => ({ ...current, on_all_busy: event.target.value as LoopBusyPolicy }))}>{BUSY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</NativeSelect></Field>
  </div>;
}

function move<T>(items: T[], from: number, to: number): T[] {
  if (to < 0 || to >= items.length) return items;
  const next = [...items];
  const [item] = next.splice(from, 1);
  if (item !== undefined) next.splice(to, 0, item);
  return next;
}

function positive(value: string) { return Math.max(1, Number.parseInt(value, 10) || 1); }

function NumberField({ label, value, min = 1, onChange }: { label: string; value: number; min?: number; onChange: (value: number) => void }) {
  return <Field label={label}><Input type="number" min={min} value={value} onChange={(event) => onChange(min === 0 ? Math.max(0, Number.parseInt(event.target.value, 10) || 0) : positive(event.target.value))} /></Field>;
}

function OrderButtons({ index, count, label, onMove }: { index: number; count: number; label: string; onMove: (offset: number) => void }) {
  return <div className="flex items-center"><IconButton label={`Move ${label} up`} disabled={index === 0} onClick={() => onMove(-1)}><ArrowUp className="size-3.5" /></IconButton><IconButton label={`Move ${label} down`} disabled={index === count - 1} onClick={() => onMove(1)}><ArrowDown className="size-3.5" /></IconButton></div>;
}

function IconButton({ label, disabled, onClick, children }: { label: string; disabled?: boolean; onClick: () => void; children: React.ReactNode }) {
  return <Button type="button" variant="ghost" size="icon" aria-label={label} disabled={disabled} onClick={onClick}>{children}</Button>;
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return <fieldset className="flex flex-col gap-3 rounded-lg border bg-card p-4"><legend className="px-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</legend>{children}</fieldset>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="flex flex-col gap-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}
