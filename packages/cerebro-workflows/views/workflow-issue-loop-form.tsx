"use client";

// FIR-2283 — "Issue workflow" design surface. Built with the exact same
// primitives workflow-form.tsx already uses (Section/Field fieldset-legend
// pattern, Switch, NativeSelect, Input, Textarea, Button, PageHeader) so it
// sits inside the existing Workflows page as a workflow TYPE, not a
// separately-styled surface. Structured as the flow itself:
// Plan -> Build -> Delivery gate -> Done, plus a control strip for watching
// a compiled recipe run on a specific issue.

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
import { Plus, Trash2 } from "lucide-react";

import { SkillNamePicker } from "./loop-pickers";

import {
  DEFAULT_LOOP_CAPS,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
} from "../core";
import type {
  CerebroWorkflow,
  LoopAssigneeType,
  LoopCheckType,
  LoopSpec,
  LoopVerification,
  WorkflowWriteInput,
} from "../core/types";

// FIR-2283 v2 point 6 — the editor no longer exposes the wire's three-way
// "Confirmed by" (command / AI review / person). A condition is now either
// "A command" or "A review", and WHO is assigned to the review decides the
// rest: an agent assignee compiles to a wire "judge" check, a person to a
// "human" check. So the UI kind is 2-way; the wire type is still 3-way and
// derived at serialization time (wireTypeForRow).
type RowKind = "programmatic" | "review";

const ROW_KIND_OPTIONS: ReadonlyArray<{ value: RowKind; label: string }> = [
  { value: "programmatic", label: "A command" },
  { value: "review", label: "A review" },
];

interface VerificationRow {
  key: string; // React key + the verification id sent to the server
  label: string;
  kind: RowKind;
  command: string; // space-separated argv, edited under "Advanced"
  // The review instruction — what the reviewer should check. Maps to the
  // wire's `rubric` for an agent review and `prompt` for a person.
  prompt: string;
  // Optional skill an AGENT review runs instead of judging from the prompt.
  // Ignored for a person review (people don't run skills).
  skill: string;
  assigneeType: LoopAssigneeType;
  assigneeId: string;
}

function newRow(): VerificationRow {
  return {
    key: `check-${Math.random().toString(36).slice(2, 10)}`,
    label: "",
    kind: "programmatic",
    command: "",
    prompt: "",
    skill: "",
    assigneeType: "agent",
    assigneeId: "",
  };
}

// A build phase in the editor (FIR-2283 followup point 6): its own build
// skill/agent, an optional prompt, and its own delivery gate (verification).
interface PhaseRow {
  key: string;
  name: string;
  buildSkill: string;
  buildAgentId: string;
  goal: string;
  verification: VerificationRow[];
}

function newPhase(seed?: Partial<PhaseRow>): PhaseRow {
  return {
    key: `phase-${Math.random().toString(36).slice(2, 10)}`,
    name: "",
    buildSkill: "",
    buildAgentId: "",
    goal: "",
    verification: [newRow()],
    ...seed,
  };
}

// The wire's LoopCheckType, derived from the UI kind + who is assigned.
function wireTypeForRow(row: VerificationRow): LoopCheckType {
  if (row.kind === "programmatic") return "programmatic";
  return row.assigneeType === "member" ? "human" : "judge";
}

interface LoopFormState {
  name: string;
  enabled: boolean;
  // FIR-2283 v2 point 4 — one skill-taggable prompt replaces Goal +
  // Definition of done. Stored on the wire as `goal`.
  goal: string;
  planning: boolean;
  planSkill: string;
  buildAgentId: string;
  buildSkill: string;
  maxAttempts: string;
  noProgressStalls: string;
  verification: VerificationRow[];
  // Gates on the Plan step (FIR-2283 v2 point 6) — only shown/sent when
  // planning is true. Empty means no plan gate: the agent advances Plan ->
  // Build on its own judgment, same as before this feature existed.
  planGate: VerificationRow[];
  // Build phases (FIR-2283 followup point 6). Empty = single-phase loop (the
  // buildSkill/buildAgentId/verification fields above drive it). Non-empty =
  // the build is split into these ordered phases, each with its own gate.
  phases: PhaseRow[];
}

const EMPTY_LOOP_FORM: LoopFormState = {
  name: "",
  enabled: true,
  goal: "",
  planning: false,
  planSkill: "",
  buildAgentId: "",
  buildSkill: "",
  maxAttempts: String(DEFAULT_LOOP_CAPS.max_iterations),
  noProgressStalls: String(DEFAULT_LOOP_CAPS.no_progress_stalls),
  verification: [newRow()],
  planGate: [],
  phases: [],
};

// rowsFromVerification/verificationFromRows convert between the wire shape
// (LoopVerification[]) and the editable form rows — shared by the Delivery
// gate (verification) and the Plan gate (plan_gate), which use the exact
// same per-condition shape.
function rowsFromVerification(list: LoopVerification[]): VerificationRow[] {
  return list.map((v) => ({
    key: v.id,
    label: v.label ?? "",
    kind: v.type === "programmatic" ? "programmatic" : "review",
    command: (v.check ?? []).join(" "),
    // Both wire review types collapse into one editable instruction: a
    // judge stores it as `rubric`, a person as `prompt`.
    prompt: v.rubric ?? v.prompt ?? "",
    skill: v.skill ?? "",
    // Default an old judge check (no explicit assignee_type) to an agent,
    // an old human check to a person.
    assigneeType: v.assignee_type ?? (v.type === "human" ? "member" : "agent"),
    assigneeId: v.assignee_id ?? "",
  }));
}

function verificationFromRows(rows: VerificationRow[]): LoopVerification[] {
  return rows.map((row, i) => {
    const wireType = wireTypeForRow(row);
    const base: LoopVerification = {
      id: row.key || `check-${i}`,
      type: wireType,
      label: row.label || undefined,
    };
    if (wireType === "programmatic") {
      base.check = row.command.trim().split(/\s+/).filter(Boolean);
      base.expect = "exit_zero";
    } else if (wireType === "human") {
      base.assignee_type = "member";
      base.assignee_id = row.assigneeId;
      base.prompt = row.prompt;
    } else {
      // judge — an agent review. Runs a skill if one is chosen, otherwise
      // judges from the free-text instruction.
      base.assignee_type = "agent";
      base.assignee_id = row.assigneeId;
      if (row.skill) base.skill = row.skill;
      base.rubric = row.prompt || row.label || row.skill;
    }
    return base;
  });
}

function formStateFromWorkflow(wf: CerebroWorkflow): LoopFormState {
  const spec = wf.loop_spec;
  if (!spec) return { ...EMPTY_LOOP_FORM, name: wf.name, enabled: wf.enabled };
  return {
    name: wf.name,
    enabled: wf.enabled,
    // Fold any legacy definition_of_done into the single prompt so old
    // recipes still show their full instruction after the point-4 collapse.
    goal: [spec.goal, spec.definition_of_done].filter(Boolean).join("\n\n"),
    planning: spec.planning === true,
    planSkill: spec.plan_skill ?? "",
    buildAgentId: spec.build_agent_id ?? "",
    buildSkill: spec.build_skill ?? "",
    maxAttempts: String(spec.caps?.max_iterations ?? DEFAULT_LOOP_CAPS.max_iterations),
    noProgressStalls: String(
      spec.caps?.no_progress_stalls ?? DEFAULT_LOOP_CAPS.no_progress_stalls,
    ),
    verification:
      spec.verification.length > 0 ? rowsFromVerification(spec.verification) : [newRow()],
    planGate: spec.plan_gate && spec.plan_gate.length > 0 ? rowsFromVerification(spec.plan_gate) : [],
    phases:
      spec.phases && spec.phases.length > 0
        ? spec.phases.map((p) =>
            newPhase({
              name: p.name ?? "",
              buildSkill: p.build_skill ?? "",
              buildAgentId: p.build_agent_id ?? "",
              goal: p.goal ?? "",
              verification:
                p.verification.length > 0 ? rowsFromVerification(p.verification) : [newRow()],
            }),
          )
        : [],
  };
}

function buildLoopSpec(form: LoopFormState): LoopSpec {
  const attempts = Math.max(1, Number.parseInt(form.maxAttempts, 10) || DEFAULT_LOOP_CAPS.max_iterations);
  const stalls = Math.max(
    1,
    Number.parseInt(form.noProgressStalls, 10) || DEFAULT_LOOP_CAPS.no_progress_stalls,
  );

  const multiPhase = form.phases.length > 0;
  const spec: LoopSpec = {
    version: 1,
    // Point 4 — the single prompt is stored as `goal`; definition_of_done is
    // no longer authored (left unset on the wire).
    goal: form.goal,
    // In multi-phase mode each phase carries its own gate; the top-level
    // verification is unused (the backend ignores it when phases is set).
    verification: multiPhase ? [] : verificationFromRows(form.verification),
    caps: {
      max_iterations: attempts,
      max_revisions: attempts,
      no_progress_stalls: stalls,
    },
    planning: form.planning,
    plan_skill: form.planning ? form.planSkill || form.buildSkill : undefined,
    plan_gate:
      form.planning && form.planGate.length > 0 ? verificationFromRows(form.planGate) : undefined,
    // In multi-phase mode phase 0 drives the loop; keep build_skill pointing at
    // it so a downgrade to single-phase still has a skill to fall back on.
    build_agent_id: multiPhase ? (form.phases[0]?.buildAgentId ?? "") : form.buildAgentId,
    build_skill: multiPhase ? (form.phases[0]?.buildSkill ?? "") : form.buildSkill,
  };
  if (multiPhase) {
    spec.phases = form.phases.map((p) => ({
      name: p.name || undefined,
      build_skill: p.buildSkill,
      build_agent_id: p.buildAgentId || undefined,
      goal: p.goal || undefined,
      verification: verificationFromRows(p.verification),
    }));
  }
  return spec;
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
    enabled: !!workflowId && !!wsId,
  });

  const [form, setForm] = useState<LoopFormState>(EMPTY_LOOP_FORM);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!detail.data) return;
    setForm(formStateFromWorkflow(detail.data));
  }, [detail.data]);

  const save = useMutation({
    mutationFn: async () => {
      const payload: WorkflowWriteInput = {
        name: form.name,
        enabled: form.enabled,
        workflow_type: "issue_loop",
        loop_spec: buildLoopSpec(form),
      };
      if (workflowId) return updateWorkflow(workflowId, payload);
      return createWorkflow(payload);
    },
    onSuccess: (wf) => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
      if (workspace && !workflowId) navigation.push(`/${workspace.slug}/workflows/${wf.id}`);
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Could not save the Issue workflow");
    },
  });

  if (!featureEnabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading workspace…
      </div>
    );
  }

  // Shared row editing for both gate lists (Delivery gate = "verification",
  // Plan gate = "planGate") — same VerificationRow shape, same operations.
  const rowOps = (field: "verification" | "planGate") => ({
    set: (key: string, patch: Partial<VerificationRow>) =>
      setForm((f) => ({
        ...f,
        [field]: f[field].map((r) => (r.key === key ? { ...r, ...patch } : r)),
      })),
    add: () => setForm((f) => ({ ...f, [field]: [...f[field], newRow()] })),
    remove: (key: string) =>
      setForm((f) => ({ ...f, [field]: f[field].filter((r) => r.key !== key) })),
  });
  const verificationOps = rowOps("verification");
  const planGateOps = rowOps("planGate");
  const setRow = verificationOps.set;

  // Build phases (FIR-2283 followup point 6). usePhases is derived from the
  // presence of phases; the toggle seeds the first phase from the single-build
  // fields (so nothing is lost switching modes) and clearing them returns to a
  // single-phase loop.
  const usePhases = form.phases.length > 0;
  const phaseOps = {
    toggle: (on: boolean) =>
      setForm((f) => {
        if (on && f.phases.length === 0) {
          return {
            ...f,
            phases: [
              newPhase({
                name: "Phase 1",
                buildSkill: f.buildSkill,
                buildAgentId: f.buildAgentId,
                goal: f.goal,
                verification: f.verification.length > 0 ? f.verification : [newRow()],
              }),
            ],
          };
        }
        if (!on) return { ...f, phases: [] };
        return f;
      }),
    add: () =>
      setForm((f) => ({
        ...f,
        phases: [...f.phases, newPhase({ name: `Phase ${f.phases.length + 1}` })],
      })),
    remove: (key: string) =>
      setForm((f) => ({ ...f, phases: f.phases.filter((p) => p.key !== key) })),
    set: (key: string, patch: Partial<PhaseRow>) =>
      setForm((f) => ({
        ...f,
        phases: f.phases.map((p) => (p.key === key ? { ...p, ...patch } : p)),
      })),
    setVerification: (
      phaseKey: string,
      updater: (rows: VerificationRow[]) => VerificationRow[],
    ) =>
      setForm((f) => ({
        ...f,
        phases: f.phases.map((p) =>
          p.key === phaseKey ? { ...p, verification: updater(p.verification) } : p,
        ),
      })),
  };

  const body = (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <form
        className="mx-auto flex max-w-2xl flex-col gap-6 p-6"
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          save.mutate();
        }}
      >
        <Section title="Basics">
          <Field label="Name">
            <Input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
              placeholder='E.g. "Ship a small feature end-to-end"'
            />
          </Field>
        </Section>

        {/* FIR-2283 followup point 4 — the loop controls (Loop on/off, Watch,
            Activate on this issue, live loop state) moved OUT of this editor
            form. They now live on the run-history page, reached from its
            three-dot menu (WorkflowLoopControls). They made no sense inline
            here next to the recipe fields. */}

        <Section title="Plan">
          <Field label="Planning mode">
            <Switch
              checked={form.planning}
              onCheckedChange={(v) => setForm({ ...form, planning: v === true })}
              data-testid="issue-loop-planning-mode"
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            When enabled, the agent must produce and post a plan before Build starts.
          </p>
          {form.planning && (
            <>
              <Field label="Plan skill (defaults to the build skill below)">
                <SkillNamePicker
                  value={form.planSkill}
                  onChange={(name) => setForm({ ...form, planSkill: name })}
                  placeholder="Same as the build skill"
                />
              </Field>

              <div className="flex flex-col gap-2 border-t pt-3">
                <p className="text-[11px] text-muted-foreground">
                  Optional — Build only starts when… (e.g. an adversarial AI review of the
                  plan). Leave empty to let the agent move to Build on its own judgment.
                </p>
                {form.planGate.length > 0 && (
                  <div className="flex flex-col gap-4">
                    {form.planGate.map((row, i) => (
                      <VerificationRowEditor
                        key={row.key}
                        row={row}
                        index={i}
                        canRemove
                        onChange={(patch) => planGateOps.set(row.key, patch)}
                        onRemove={() => planGateOps.remove(row.key)}
                      />
                    ))}
                  </div>
                )}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="self-start"
                  onClick={planGateOps.add}
                >
                  <Plus className="size-4" />
                  Add plan gate
                </Button>
              </div>
            </>
          )}
        </Section>

        <Section title="Build">
          {/* FIR-2283 followup point 6 — split the build into phases. */}
          <Field label="Split the build into phases">
            <Switch
              checked={usePhases}
              onCheckedChange={(v) => phaseOps.toggle(v === true)}
              data-testid="issue-loop-build-phases"
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            Off = one build then one review. On = an ordered chain of build phases,
            each with its own review that must pass before the next phase starts.
          </p>
          {!usePhases && (
            <>
              <Field label="Worker skill">
                <SkillNamePicker
                  value={form.buildSkill}
                  onChange={(name) => setForm({ ...form, buildSkill: name })}
                  placeholder="Select the skill the worker runs"
                />
              </Field>
              <Field label="Agent (must have the skill attached)">
                <AgentPicker
                  agentId={form.buildAgentId || null}
                  onChange={(id) => setForm({ ...form, buildAgentId: id })}
                />
              </Field>
              <Field label="Prompt">
                <Textarea
                  value={form.goal}
                  onChange={(e) => setForm({ ...form, goal: e.target.value })}
                  rows={4}
                  placeholder="Describe how the agent should work. Tag a skill with @, or use the picker below."
                  data-testid="issue-loop-prompt"
                />
                <div className="mt-2 flex items-center gap-2">
                  <span className="text-[11px] text-muted-foreground">Tag a skill:</span>
                  <SkillNamePicker
                    value=""
                    onChange={(name) => {
                      if (!name) return;
                      setForm((f) => ({
                        ...f,
                        goal: f.goal.trim() ? `${f.goal.trimEnd()} @${name} ` : `@${name} `,
                      }));
                    }}
                    placeholder="Add a skill…"
                  />
                </div>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  Replaces the old Goal / Definition of done. The recipe describes HOW
                  to work — &quot;done&quot; is decided by the gates below, not a fixed text.
                </p>
              </Field>
            </>
          )}
        </Section>

        {usePhases && (
          <Section title="Build phases">
            <p className="text-[11px] text-muted-foreground">
              Each phase runs its own build, then its own review. The next phase only
              starts once the current phase&apos;s review passes.
            </p>
            {form.phases.map((phase, pi) => (
              <div key={phase.key} className="flex flex-col gap-3 rounded-md border p-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-muted-foreground">
                    Phase {pi + 1}
                  </span>
                  {form.phases.length > 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label="Remove phase"
                      onClick={() => phaseOps.remove(phase.key)}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  )}
                </div>
                <Field label="Name">
                  <Input
                    value={phase.name}
                    onChange={(e) => phaseOps.set(phase.key, { name: e.target.value })}
                    placeholder='E.g. "Backend"'
                  />
                </Field>
                <Field label="Worker skill">
                  <SkillNamePicker
                    value={phase.buildSkill}
                    onChange={(name) => phaseOps.set(phase.key, { buildSkill: name })}
                    placeholder="Select the skill this phase runs"
                  />
                </Field>
                <Field label="Agent (must have the skill attached)">
                  <AgentPicker
                    agentId={phase.buildAgentId || null}
                    onChange={(id) => phaseOps.set(phase.key, { buildAgentId: id })}
                  />
                </Field>
                <Field label="Prompt">
                  <Textarea
                    value={phase.goal}
                    onChange={(e) => phaseOps.set(phase.key, { goal: e.target.value })}
                    rows={3}
                    placeholder="Describe how the agent should work in this phase."
                  />
                </Field>
                <div className="flex flex-col gap-2 border-t pt-3">
                  <p className="text-[11px] text-muted-foreground">This phase is done when…</p>
                  <div className="flex flex-col gap-4">
                    {phase.verification.map((row, i) => (
                      <VerificationRowEditor
                        key={row.key}
                        row={row}
                        index={i}
                        canRemove={phase.verification.length > 1}
                        onChange={(patch) =>
                          phaseOps.setVerification(phase.key, (rows) =>
                            rows.map((r) => (r.key === row.key ? { ...r, ...patch } : r)),
                          )
                        }
                        onRemove={() =>
                          phaseOps.setVerification(phase.key, (rows) =>
                            rows.filter((r) => r.key !== row.key),
                          )
                        }
                      />
                    ))}
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="self-start"
                    onClick={() =>
                      phaseOps.setVerification(phase.key, (rows) => [...rows, newRow()])
                    }
                  >
                    <Plus className="size-4" />
                    Add condition
                  </Button>
                </div>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="self-start"
              onClick={phaseOps.add}
            >
              <Plus className="size-4" />
              Add build phase
            </Button>

            <div className="flex flex-col gap-2 border-t pt-3">
              <p className="text-xs text-muted-foreground">
                Give each phase up to{" "}
                <Input
                  type="number"
                  min={1}
                  value={form.maxAttempts}
                  onChange={(e) => setForm({ ...form, maxAttempts: e.target.value })}
                  className="inline-block h-7 w-16 px-2 py-1 text-center"
                />{" "}
                attempts, then stop.
              </p>
              <p className="text-xs text-muted-foreground">
                If nothing improves in{" "}
                <Input
                  type="number"
                  min={1}
                  value={form.noProgressStalls}
                  onChange={(e) => setForm({ ...form, noProgressStalls: e.target.value })}
                  className="inline-block h-7 w-16 px-2 py-1 text-center"
                />{" "}
                attempts in a row, stop early and ask me.
              </p>
              <p className="text-[11px] text-muted-foreground">
                Each phase needs at least one condition confirmed by <strong>a command</strong>.
              </p>
            </div>
          </Section>
        )}

        {!usePhases && (
        <Section title="Delivery gate">
          <p className="text-[11px] text-muted-foreground">
            This is done when…
          </p>
          <div className="flex flex-col gap-4">
            {form.verification.map((row, i) => (
              <VerificationRowEditor
                key={row.key}
                row={row}
                index={i}
                canRemove={form.verification.length > 1}
                onChange={(patch) => setRow(row.key, patch)}
                onRemove={() =>
                  setForm((f) => ({
                    ...f,
                    verification: f.verification.filter((r) => r.key !== row.key),
                  }))
                }
              />
            ))}
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="self-start"
            onClick={() => setForm((f) => ({ ...f, verification: [...f.verification, newRow()] }))}
          >
            <Plus className="size-4" />
            Add condition
          </Button>

          <div className="flex flex-col gap-2 border-t pt-3">
            <p className="text-xs text-muted-foreground">
              Give it up to{" "}
              <Input
                type="number"
                min={1}
                value={form.maxAttempts}
                onChange={(e) => setForm({ ...form, maxAttempts: e.target.value })}
                className="inline-block h-7 w-16 px-2 py-1 text-center"
              />{" "}
              attempts, then stop.
            </p>
            <p className="text-xs text-muted-foreground">
              If nothing improves in{" "}
              <Input
                type="number"
                min={1}
                value={form.noProgressStalls}
                onChange={(e) => setForm({ ...form, noProgressStalls: e.target.value })}
                className="inline-block h-7 w-16 px-2 py-1 text-center"
              />{" "}
              attempts in a row, stop early and ask me.
            </p>
          </div>

          <p className="text-[11px] text-muted-foreground">
            At least one condition confirmed by <strong>a command</strong> is required — it is the
            only thing the engine can prove on its own; everything else is a judgment call.
          </p>
        </Section>
        )}

        {error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigation.push(`/${workspace.slug}/workflows`)}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save"}
          </Button>
        </div>

        <p className="text-[11px] text-muted-foreground">
          Only the engine can set this to Done — and only once a command it ran itself is green.
        </p>
      </form>
    </div>
  );

  if (embedded) return body;

  const heading = workflowId ? "Edit Issue workflow" : "New Issue workflow";
  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">{heading}</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Plan → Build → Delivery gate → Done.
          </p>
        </div>
      </PageHeader>
      {body}
    </div>
  );
}

// FIR-2283 followup point 5 — the presets ARE the interface for a non-developer.
// Each option pairs a plain-language meaning with the exact command the engine
// runs, so someone who doesn't know "make test" still understands "Run the
// tests". `label` seeds the condition's name; `command` is what actually runs.
const COMMAND_PRESETS: ReadonlyArray<{ label: string; command: string; hint: string }> = [
  { label: "Run the tests", command: "make test", hint: "the project's automated tests all pass" },
  { label: "Build the project", command: "make build", hint: "the project compiles with no errors" },
  { label: "Check the code types", command: "pnpm typecheck", hint: "the code has no type mistakes" },
];

function VerificationRowEditor({
  row,
  index,
  canRemove,
  onChange,
  onRemove,
}: {
  row: VerificationRow;
  index: number;
  canRemove: boolean;
  onChange: (patch: Partial<VerificationRow>) => void;
  onRemove: () => void;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <div className="flex items-center gap-2">
        <Input
          value={row.label}
          onChange={(e) => onChange({ label: e.target.value })}
          placeholder={`Condition ${index + 1}, e.g. "The tests pass"`}
          className="flex-1"
        />
        {canRemove && (
          <Button type="button" variant="ghost" size="icon" onClick={onRemove} aria-label="Remove condition">
            <Trash2 className="size-4" />
          </Button>
        )}
      </div>

      <Field label="Confirmed by">
        <NativeSelect
          value={row.kind}
          onChange={(e) => onChange({ kind: e.target.value as RowKind })}
        >
          {ROW_KIND_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </NativeSelect>
      </Field>

      {row.kind === "programmatic" && (
        // FIR-2283 followup point 5 — "commands" were hidden behind an
        // "Advanced" toggle and explained in developer terms (exit codes,
        // Makefile targets). A non-developer never opened it. Now the plain-
        // language meaning is the default, the ready-made checks are the
        // primary interface, and the exact command is a clearly-labelled
        // secondary field for people who know exactly what to run.
        <div className="flex flex-col gap-2 rounded-md border bg-muted/30 p-3 text-xs">
          <p className="text-muted-foreground">
            A check is something the computer runs by itself to <strong>prove the
            work is really finished</strong> — for example that the tests pass or
            the project builds. Pick a ready-made check, or type the exact one your
            project uses. Only the computer can mark the issue Done, and only when
            this check comes back clean.
          </p>
          <Field label="Which check?">
            <NativeSelect
              value={COMMAND_PRESETS.find((p) => p.command === row.command)?.label ?? ""}
              onChange={(e) => {
                const preset = COMMAND_PRESETS.find((p) => p.label === e.target.value);
                if (preset) onChange({ command: preset.command, label: row.label || preset.label });
              }}
            >
              <option value="">Choose a ready-made check…</option>
              {COMMAND_PRESETS.map((p) => (
                <option key={p.label} value={p.label}>
                  {p.label} — checks that {p.hint}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <Field label="Or type the exact command your project runs">
            <Input
              value={row.command}
              onChange={(e) => onChange({ command: e.target.value })}
              placeholder="e.g. make test"
              data-testid="loop-check-command"
            />
          </Field>
          <p className="text-[11px] text-muted-foreground">
            The computer runs this in the project and treats the check as passed
            only when it finishes with no error. Anything the project can already
            run works here — there is nothing to register first.
          </p>
        </div>
      )}

      {/* FIR-2283 v2 point 6 — one "A review" branch. The assignee picker
          (agents + people, shared component) decides everything: an agent is
          an AI review that can run a skill; a person is a human sign-off. */}
      {row.kind === "review" && (
        <>
          <AssigneeFields row={row} onChange={onChange} />
          {row.assigneeType === "agent" && (
            <Field label="Run a skill (optional)">
              <SkillNamePicker
                value={row.skill}
                onChange={(name) => onChange({ skill: name })}
                placeholder="Leave empty to review from the instruction below"
              />
            </Field>
          )}
          <Field label={row.assigneeType === "member" ? "What should they sign off on?" : "What should the review check?"}>
            <Textarea
              value={row.prompt}
              onChange={(e) => onChange({ prompt: e.target.value })}
              rows={2}
              placeholder={
                row.assigneeType === "member"
                  ? "What should the person confirm before this is done?"
                  : "What should the AI review judge? (ignored if a skill is chosen above)"
              }
            />
          </Field>
        </>
      )}
    </div>
  );
}

function AssigneeFields({
  row,
  onChange,
}: {
  row: VerificationRow;
  onChange: (patch: Partial<VerificationRow>) => void;
}) {
  return (
    <Field label="Assign to">
      <div className="w-fit max-w-full">
        <AssigneePicker
          assigneeType={row.assigneeId ? row.assigneeType : null}
          assigneeId={row.assigneeId || null}
          onUpdate={(u) => {
            const type = u.assignee_type;
            if (type === "agent" || type === "member") {
              onChange({ assigneeType: type, assigneeId: u.assignee_id ?? "" });
            } else {
              onChange({ assigneeId: "" });
            }
          }}
          align="start"
        />
      </div>
    </Field>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="flex flex-col gap-3 rounded-md border bg-card p-4">
      <legend className="px-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </legend>
      {children}
    </fieldset>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}
