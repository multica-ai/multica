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
  CONFIRMED_BY_OPTIONS,
  DEFAULT_LOOP_CAPS,
  cerebroLoopStateOptions,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
  useApproveHumanCheckMutation,
} from "../core";
import type {
  CerebroWorkflow,
  LoopAssigneeType,
  LoopCheckType,
  LoopSpec,
  LoopVerification,
  WorkflowWriteInput,
} from "../core/types";

interface VerificationRow {
  key: string; // React key + the verification id sent to the server
  label: string;
  type: LoopCheckType;
  command: string; // space-separated argv, edited under "Advanced"
  runsAs: "skill" | "prompt";
  skill: string;
  rubric: string;
  prompt: string;
  assigneeType: LoopAssigneeType;
  assigneeId: string;
}

function newRow(): VerificationRow {
  return {
    key: `check-${Math.random().toString(36).slice(2, 10)}`,
    label: "",
    type: "programmatic",
    command: "",
    runsAs: "skill",
    skill: "",
    rubric: "",
    prompt: "",
    assigneeType: "agent",
    assigneeId: "",
  };
}

interface LoopFormState {
  name: string;
  enabled: boolean;
  goal: string;
  definitionOfDone: string;
  planning: boolean;
  planSkill: string;
  buildAgentId: string;
  buildSkill: string;
  maxAttempts: string;
  noProgressStalls: string;
  verification: VerificationRow[];
}

const EMPTY_LOOP_FORM: LoopFormState = {
  name: "",
  enabled: true,
  goal: "",
  definitionOfDone: "",
  planning: false,
  planSkill: "",
  buildAgentId: "",
  buildSkill: "",
  maxAttempts: String(DEFAULT_LOOP_CAPS.max_iterations),
  noProgressStalls: String(DEFAULT_LOOP_CAPS.no_progress_stalls),
  verification: [newRow()],
};

function formStateFromWorkflow(wf: CerebroWorkflow): LoopFormState {
  const spec = wf.loop_spec;
  if (!spec) return { ...EMPTY_LOOP_FORM, name: wf.name, enabled: wf.enabled };
  return {
    name: wf.name,
    enabled: wf.enabled,
    goal: spec.goal ?? "",
    definitionOfDone: spec.definition_of_done ?? "",
    planning: spec.planning === true,
    planSkill: spec.plan_skill ?? "",
    buildAgentId: spec.build_agent_id ?? "",
    buildSkill: spec.build_skill ?? "",
    maxAttempts: String(spec.caps?.max_iterations ?? DEFAULT_LOOP_CAPS.max_iterations),
    noProgressStalls: String(
      spec.caps?.no_progress_stalls ?? DEFAULT_LOOP_CAPS.no_progress_stalls,
    ),
    verification:
      spec.verification.length > 0
        ? spec.verification.map((v) => ({
            key: v.id,
            label: v.label ?? "",
            type: v.type,
            command: (v.check ?? []).join(" "),
            runsAs: v.skill ? "skill" : "prompt",
            skill: v.skill ?? "",
            rubric: v.rubric ?? "",
            prompt: v.prompt ?? "",
            assigneeType: v.assignee_type ?? "agent",
            assigneeId: v.assignee_id ?? "",
          }))
        : [newRow()],
  };
}

function buildLoopSpec(form: LoopFormState): LoopSpec {
  const attempts = Math.max(1, Number.parseInt(form.maxAttempts, 10) || DEFAULT_LOOP_CAPS.max_iterations);
  const stalls = Math.max(
    1,
    Number.parseInt(form.noProgressStalls, 10) || DEFAULT_LOOP_CAPS.no_progress_stalls,
  );
  const verification: LoopVerification[] = form.verification.map((row, i) => {
    const base: LoopVerification = {
      id: row.key || `check-${i}`,
      type: row.type,
      label: row.label || undefined,
    };
    if (row.type === "programmatic") {
      base.check = row.command.trim().split(/\s+/).filter(Boolean);
      base.expect = "exit_zero";
    } else if (row.type === "judge") {
      base.assignee_type = row.assigneeType;
      base.assignee_id = row.assigneeId;
      if (row.runsAs === "skill") {
        base.skill = row.skill;
        base.rubric = row.label || row.skill;
      } else {
        base.rubric = row.rubric;
      }
    } else {
      base.assignee_type = row.assigneeType;
      base.assignee_id = row.assigneeId;
      base.prompt = row.prompt;
    }
    return base;
  });

  return {
    version: 1,
    goal: form.goal,
    definition_of_done: form.definitionOfDone,
    verification,
    caps: {
      max_iterations: attempts,
      max_revisions: attempts,
      no_progress_stalls: stalls,
    },
    planning: form.planning,
    plan_skill: form.planning ? form.planSkill || form.buildSkill : undefined,
    build_agent_id: form.buildAgentId,
    build_skill: form.buildSkill,
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
    enabled: !!workflowId && !!wsId,
  });

  const [form, setForm] = useState<LoopFormState>(EMPTY_LOOP_FORM);
  const [error, setError] = useState<string | null>(null);
  const [watchIssueId, setWatchIssueId] = useState("");

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

  const toggleEnabled = useMutation({
    mutationFn: async (enabled: boolean) => {
      if (!workflowId) return;
      const payload: WorkflowWriteInput = {
        name: form.name,
        enabled,
        workflow_type: "issue_loop",
        loop_spec: buildLoopSpec(form),
      };
      await updateWorkflow(workflowId, payload);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
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

  const setRow = (key: string, patch: Partial<VerificationRow>) => {
    setForm((f) => ({
      ...f,
      verification: f.verification.map((r) => (r.key === key ? { ...r, ...patch } : r)),
    }));
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

        {workflowId && (
          <ControlStrip
            workflowId={workflowId}
            wsId={wsId}
            enabled={form.enabled}
            onToggle={(v) => {
              setForm({ ...form, enabled: v });
              toggleEnabled.mutate(v);
            }}
            watchIssueId={watchIssueId}
            onChangeWatchIssueId={setWatchIssueId}
          />
        )}

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
            <Field label="Plan skill (defaults to the build skill below)">
              <SkillNamePicker
                value={form.planSkill}
                onChange={(name) => setForm({ ...form, planSkill: name })}
                placeholder="Same as the build skill"
              />
            </Field>
          )}
        </Section>

        <Section title="Build">
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
          <Field label="Goal">
            <Textarea
              value={form.goal}
              onChange={(e) => setForm({ ...form, goal: e.target.value })}
              rows={3}
              required
              placeholder="What should the agent build?"
            />
          </Field>
          <Field label="Definition of done">
            <Textarea
              value={form.definitionOfDone}
              onChange={(e) => setForm({ ...form, definitionOfDone: e.target.value })}
              rows={3}
              required
              placeholder="What does 'done' look like?"
            />
          </Field>
        </Section>

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

const COMMAND_PRESETS: ReadonlyArray<{ label: string; command: string }> = [
  { label: "Test suite", command: "make test" },
  { label: "Build", command: "make build" },
  { label: "Type check", command: "pnpm typecheck" },
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
          value={row.type}
          onChange={(e) => onChange({ type: e.target.value as LoopCheckType })}
        >
          {CONFIRMED_BY_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </NativeSelect>
      </Field>

      {row.type === "programmatic" && (
        <details className="rounded-md border bg-muted/30 p-2 text-xs">
          <summary className="cursor-pointer select-none text-muted-foreground">Advanced</summary>
          <div className="mt-2 flex flex-col gap-2">
            <NativeSelect
              defaultValue=""
              onChange={(e) => {
                const preset = COMMAND_PRESETS.find((p) => p.label === e.target.value);
                if (preset) onChange({ command: preset.command, label: row.label || preset.label });
              }}
            >
              <option value="">Command preset…</option>
              {COMMAND_PRESETS.map((p) => (
                <option key={p.label} value={p.label}>
                  {p.label}
                </option>
              ))}
            </NativeSelect>
            <Input
              value={row.command}
              onChange={(e) => onChange({ command: e.target.value })}
              placeholder="make test"
              data-testid="loop-check-command"
            />
          </div>
        </details>
      )}

      {row.type === "judge" && (
        <>
          <Field label="Runs">
            <NativeSelect
              value={row.runsAs}
              onChange={(e) => onChange({ runsAs: e.target.value as "skill" | "prompt" })}
            >
              <option value="skill">A skill</option>
              <option value="prompt">A prompt</option>
            </NativeSelect>
          </Field>
          {row.runsAs === "skill" ? (
            <Field label="Skill">
              <SkillNamePicker
                value={row.skill}
                onChange={(name) => onChange({ skill: name })}
                placeholder="Select the review skill"
              />
            </Field>
          ) : (
            <Field label="Prompt">
              <Textarea
                value={row.rubric}
                onChange={(e) => onChange({ rubric: e.target.value })}
                rows={2}
                placeholder="What should the review judge?"
              />
            </Field>
          )}
          <AssigneeFields row={row} onChange={onChange} />
        </>
      )}

      {row.type === "human" && (
        <>
          <Field label="Prompt">
            <Textarea
              value={row.prompt}
              onChange={(e) => onChange({ prompt: e.target.value })}
              rows={2}
              placeholder="What should the person sign off on?"
            />
          </Field>
          <AssigneeFields row={row} onChange={onChange} />
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
      />
    </Field>
  );
}

function ControlStrip({
  workflowId,
  wsId,
  enabled,
  onToggle,
  watchIssueId,
  onChangeWatchIssueId,
}: {
  workflowId: string;
  wsId: string;
  enabled: boolean;
  onToggle: (v: boolean) => void;
  watchIssueId: string;
  onChangeWatchIssueId: (v: string) => void;
}) {
  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();
  const loopState = useQuery({
    ...cerebroLoopStateOptions(wsId, workflowId, watchIssueId),
  });
  const approve = useApproveHumanCheckMutation(wsId, workflowId, watchIssueId);

  return (
    <div className="flex flex-col gap-3 rounded-md border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center gap-3">
        <Field label="Loop">
          <Switch checked={enabled} onCheckedChange={(v) => onToggle(v === true)} />
        </Field>
        <Field label="Watch issue (paste an issue id to follow it here)">
          <Input
            value={watchIssueId}
            onChange={(e) => onChangeWatchIssueId(e.target.value)}
            placeholder="<issue uuid>"
            className="w-56"
          />
        </Field>
        {workspace && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => navigation.push(`/${workspace.slug}/workflows/${workflowId}/runs`)}
          >
            View run history
          </Button>
        )}
      </div>

      {watchIssueId && loopState.data && (
        <div className="flex flex-col gap-2 text-xs">
          <p className="text-muted-foreground">
            {loopState.data.stopped ? (
              <span className="font-medium text-amber-600 dark:text-amber-400">
                Paused · escalated to owner{loopState.data.stop_reason ? ` — ${loopState.data.stop_reason}` : ""}
              </span>
            ) : (
              <>
                Round {loopState.data.round}
                {loopState.data.max_iterations ? ` of ${loopState.data.max_iterations}` : ""}
              </>
            )}
          </p>
          {loopState.data.pending_human_checks.map((check) => (
            <div
              key={check.check_id}
              className="flex items-center justify-between gap-2 rounded-md border bg-background p-2"
            >
              <span className="min-w-0 truncate">Waiting on you: {check.prompt}</span>
              <div className="flex shrink-0 gap-1">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => approve.mutate({ checkId: check.check_id, approved: true })}
                >
                  Approve
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    approve.mutate({
                      checkId: check.check_id,
                      approved: false,
                      note: "Rejected from the control strip",
                    })
                  }
                >
                  Reject
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
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
