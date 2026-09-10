"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Bot,
  GripVertical,
  Plus,
  Trash2,
  User,
  Zap,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  agentListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import {
  manualEntryPolicy,
  moveWorkflowStatus,
  removeWorkflowStatus,
  workflowProblems,
  type WorkflowDraft,
  type WorkflowStatus,
} from "@multica/core/issue-workflows";
import type {
  IssueWorkflowAssigneeTarget,
  IssueWorkflowExecutorTarget,
  IssueWorkflowPhase,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface Choice {
  value: string;
  label: string;
}
function ChoiceField({
  id,
  label,
  value,
  items,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  items: Choice[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="text-caption font-medium">
        {label}
      </label>
      <Select
        items={items}
        value={value}
        onValueChange={(v) => {
          if (v !== null) onChange(v);
        }}
      >
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function WorkflowEditor({
  value,
  onChange,
  showErrors = false,
  disabled = false,
}: {
  value: WorkflowDraft;
  onChange: (draft: WorkflowDraft) => void;
  showErrors?: boolean;
  disabled?: boolean;
}) {
  const { t } = useT("projects");
  const id = useId();
  const wsId = useWorkspaceId();
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const [selectedKey, setSelectedKey] = useState(value.initialKey);
  const status =
    value.statuses.find((s) => s.key === selectedKey) ?? value.statuses[0];
  const index = status ? value.statuses.indexOf(status) : -1;
  const problems = workflowProblems(value);
  const actorChoices: Choice[] = [
    { value: "keep", label: t(($) => $.workflow.manual) },
    { value: "agent:", label: t(($) => $.workflow.choose_agent) },
    ...(members.data ?? []).map((m) => ({
      value: `human:${m.user_id}`,
      label: m.name,
    })),
    ...(agents.data ?? [])
      .filter((a) => !a.archived_at)
      .map((a) => ({
        value: `agent:${a.id}`,
        label: t(($) => $.workflow_rules.agent_target, { name: a.name }),
      })),
    ...(squads.data ?? [])
      .filter((s) => !s.archived_at)
      .map((s) => ({
        value: `squad:${s.id}`,
        label: t(($) => $.workflow_rules.squad_target, { name: s.name }),
      })),
  ];
  const actorValue = (s: WorkflowStatus) =>
    s.policy.executor.type !== "none"
      ? `${s.policy.executor.type}:${s.policy.executor.id}`
      : s.policy.assignee.type === "keep"
        ? "keep"
        : `${s.policy.assignee.type}:${s.policy.assignee.id}`;
  const nameFor = (s: WorkflowStatus) =>
    actorChoices.find((c) => c.value === actorValue(s))?.label ??
    t(($) => $.workflow.unavailable);
  const update = (patch: Partial<WorkflowStatus>) => {
    if (status)
      onChange({
        ...value,
        statuses: value.statuses.map((s) =>
          s.key === status.key ? { ...s, ...patch } : s,
        ),
      });
  };
  const updatePolicy = (patch: Partial<WorkflowStatus["policy"]>) => {
    if (status) update({ policy: { ...status.policy, ...patch } });
  };
  const add = (after: number) => {
    const key = `status_${crypto.randomUUID().replaceAll("-", "")}`;
    const previous = value.statuses[after];
    const statuses = [...value.statuses];
    const newStatus: WorkflowStatus = {
      key,
      name: "",
      description: "",
      color: "#6b7280",
      phase: "started",
      policy: {
        ...manualEntryPolicy(),
        next_status_key:
          previous?.policy.next_status_key || statuses[after + 1]?.key || "",
      },
    };
    statuses.splice(after + 1, 0, newStatus);
    if (
      previous &&
      previous.phase !== "completed" &&
      previous.phase !== "cancelled"
    )
      statuses[after] = {
        ...previous,
        policy: { ...previous.policy, next_status_key: key },
      };
    onChange({ ...value, initialKey: value.initialKey || key, statuses });
    setSelectedKey(key);
  };
  const move = (from: number, to: number) =>
    onChange(moveWorkflowStatus(value, from, to));
  const terminal = status?.phase === "completed" || status?.phase === "cancelled";
  const phases: IssueWorkflowPhase[] = [
    "backlog",
    "unstarted",
    "started",
    "completed",
    "cancelled",
  ];
  const phaseLabels = {
    backlog: t(($) => $.workflow_rules.phase_backlog),
    unstarted: t(($) => $.workflow_rules.phase_unstarted),
    started: t(($) => $.workflow_rules.phase_started),
    completed: t(($) => $.workflow_rules.phase_completed),
    cancelled: t(($) => $.workflow_rules.phase_cancelled),
  };
  const assignee = status?.policy.assignee;
  const ownerValue =
    assignee?.type === "keep" || !assignee
      ? "keep"
      : `${assignee.type}:${assignee.id}`;
  const ownerChoices = actorChoices.map((c) =>
    c.value === "keep"
      ? { ...c, label: t(($) => $.workflow_rules.keep_assignee) }
      : c,
  );
  if (!ownerChoices.some((c) => c.value === ownerValue))
    ownerChoices.push({
      value: ownerValue,
      label: t(($) => $.workflow.unavailable),
    });
  const executor = status?.policy.executor;
  const executorValue =
    !executor || executor.type === "none"
      ? "none"
      : `${executor.type}:${executor.id}`;
  const executorChoices = [
    { value: "none", label: t(($) => $.workflow_rules.no_automatic_run) },
    ...actorChoices.filter(
      (c) => c.value.startsWith("agent:") || c.value.startsWith("squad:"),
    ),
  ];
  if (!executorChoices.some((c) => c.value === executorValue))
    executorChoices.push({
      value: executorValue,
      label: t(($) => $.workflow.unavailable),
    });

  return (
    <fieldset disabled={disabled} className="min-w-0 space-y-4">
      <div className="grid min-w-0 gap-6 md:grid-cols-2">
        <section
          aria-label={t(($) => $.workflow.title)}
          className="min-w-0 space-y-1"
        >
          {value.statuses.map((s, i) => (
            <div
              key={s.key}
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault();
                if (disabled) return;
                const from = value.statuses.findIndex(
                  (item) =>
                    item.key ===
                    event.dataTransfer.getData(
                      "application/multica-workflow-status",
                    ),
                );
                if (from >= 0) move(from, i);
              }}
            >
              <div
                className={cn(
                  "flex items-start gap-1 rounded-lg border p-2",
                  s.key === status?.key && "border-foreground/40 bg-accent",
                )}
              >
                <button
                  type="button"
                  draggable={!disabled}
                  aria-label={t(($) => $.workflow.drag)}
                  onDragStart={(event) =>
                    event.dataTransfer.setData(
                      "application/multica-workflow-status",
                      s.key,
                    )
                  }
                  className="mt-1 shrink-0 cursor-grab rounded p-1 text-muted-foreground"
                >
                  <GripVertical className="size-3.5" />
                </button>
                <button
                  type="button"
                  aria-pressed={s.key === status?.key}
                  onClick={() => setSelectedKey(s.key)}
                  className="min-w-0 flex-1 space-y-1 rounded px-1 py-1 text-left"
                >
                  <span className="flex flex-wrap items-center gap-2 text-body font-medium">
                    <span
                      className="size-2 shrink-0 rounded-full"
                      style={{ backgroundColor: s.color }}
                    />
                    <span className="break-words">
                      {s.name || t(($) => $.workflow.new_status)}
                    </span>
                    {s.key === value.initialKey && (
                      <span className="text-caption font-normal text-muted-foreground">
                        {t(($) => $.workflow.start)}
                      </span>
                    )}
                  </span>
                  <span className="flex items-start gap-1.5 text-caption text-muted-foreground">
                    {s.policy.executor.type === "none" ? (
                      <User className="mt-0.5 size-3 shrink-0" />
                    ) : (
                      <Bot className="mt-0.5 size-3 shrink-0" />
                    )}
                    <span className="break-words">{nameFor(s)}</span>
                  </span>
                  {showErrors && problems.some((p) => p.key === s.key) && (
                    <span className="block text-caption text-destructive">
                      {t(
                        ($) =>
                          $.workflow.problems[
                            problems.find((p) => p.key === s.key)!.problem
                          ],
                      )}
                    </span>
                  )}
                </button>
              </div>
              <div className="ml-5 flex min-h-7 items-center justify-between border-l pl-3">
                <span className="text-caption text-muted-foreground">
                  {s.policy.next_status_key
                    ? `→ ${value.statuses.find((n) => n.key === s.policy.next_status_key)?.name || t(($) => $.workflow.new_status)}`
                    : ""}
                </span>
                {value.statuses.length < 50 && (
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    aria-label={`${t(($) => $.workflow.add)}: ${s.name}`}
                    onClick={() => add(i)}
                  >
                    <Plus className="size-3" />
                  </Button>
                )}
              </div>
            </div>
          ))}
          {!value.statuses.length && (
            <Button variant="outline" onClick={() => add(-1)}>
              {t(($) => $.workflow.add)}
            </Button>
          )}
          <p className="pt-3 text-caption text-muted-foreground">
            {t(($) => $.workflow.reorder_hint)}
          </p>
        </section>
        {status && (
          <section
            aria-label={t(($) => $.workflow.editor_title)}
            className="min-w-0 space-y-4 border-t pt-5 md:border-l md:border-t-0 md:pl-6 md:pt-0"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-caption text-muted-foreground">
                {index + 1} / {value.statuses.length}
              </span>
              <div className="flex gap-1">
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label={t(($) => $.workflow.move_up)}
                  disabled={index <= 0}
                  onClick={() => move(index, index - 1)}
                >
                  <ArrowUp />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label={t(($) => $.workflow.move_down)}
                  disabled={index >= value.statuses.length - 1}
                  onClick={() => move(index, index + 1)}
                >
                  <ArrowDown />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label={t(($) => $.workflow.remove)}
                  disabled={
                    status.key === value.initialKey || value.statuses.length <= 1
                  }
                  onClick={() =>
                    onChange(removeWorkflowStatus(value, status.key))
                  }
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
            <div className="space-y-1.5">
              <label
                htmlFor={`${id}-name`}
                className="text-caption font-medium"
              >
                {t(($) => $.workflow.status_name)}
              </label>
              <Input
                id={`${id}-name`}
                maxLength={64}
                value={status.name}
                onChange={(event) => update({ name: event.target.value })}
              />
            </div>
            <ChoiceField
              id={`${id}-owner`}
              label={t(($) => $.workflow.owner)}
              value={ownerValue}
              items={ownerChoices}
              onChange={(v) => {
                const [type, targetId = ""] = v.split(":");
                updatePolicy({
                  assignee:
                    type === "keep"
                      ? { type: "keep" }
                      : ({
                          type,
                          id: targetId,
                        } as IssueWorkflowAssigneeTarget),
                });
              }}
            />
            <h3 className="pt-3 text-body font-medium">
              {t(($) => $.workflow.action)}
            </h3>
            <ChoiceField
              id={`${id}-executor`}
              label={t(($) => $.workflow.run_on_entry)}
              value={executorValue}
              items={executorChoices}
              onChange={(v) => {
                const [type, targetId = ""] = v.split(":");
                updatePolicy({
                  ...(type === "none" ? { advance: "human_confirms" as const } : {}),
                  executor:
                    type === "none"
                      ? { type: "none" }
                      : ({
                          type,
                          id: targetId,
                        } as IssueWorkflowExecutorTarget),
                });
              }}
            />
            <p className="flex gap-2 text-caption text-muted-foreground">
              <Zap className="size-3.5 shrink-0" />
              {t(($) =>
                status.policy.executor.type === "none"
                  ? $.workflow.manual_hint
                  : $.workflow.auto_hint,
              )}
            </p>
            <div className="space-y-1.5">
              <label
                htmlFor={`${id}-instructions`}
                className="text-caption font-medium"
              >
                {t(($) => $.workflow.instructions)}
              </label>
              <Textarea
                id={`${id}-instructions`}
                rows={4}
                value={status.policy.instructions}
                onChange={(event) =>
                  updatePolicy({ instructions: event.target.value })
                }
              />
            </div>
            <h3 className="pt-3 text-body font-medium">
              {t(($) => $.workflow.transitions)}
            </h3>
            <ChoiceField
              id={`${id}-next`}
              label={t(($) => $.workflow.next_status)}
              value={status.policy.next_status_key ?? ""}
              items={[
                { value: "", label: t(($) => $.workflow.no_next) },
                ...value.statuses
                  .filter((s) => s.key !== status.key)
                  .map((s) => ({
                    value: s.key,
                    label: s.name || t(($) => $.workflow.new_status),
                  })),
              ]}
              onChange={(next_status_key) => updatePolicy({ next_status_key })}
            />
            {status.policy.executor.type !== "none" && (
              <ChoiceField
                id={`${id}-advance`}
                label={t(($) => $.workflow.advance)}
                value={status.policy.advance}
                items={[
                  {
                    value: "human_confirms",
                    label: t(($) => $.workflow.human_confirms),
                  },
                  {
                    value: "executor_may_transition",
                    label: t(($) => $.workflow.agent_advances),
                  },
                ]}
                onChange={(v) =>
                  updatePolicy({
                    advance: v as WorkflowStatus["policy"]["advance"],
                  })
                }
              />
            )}
            {terminal && (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.workflow.terminal_hint)}
              </p>
            )}
            <details className="space-y-3 text-caption">
              <summary className="cursor-pointer text-muted-foreground">
                {t(($) => $.workflow.advanced)}
              </summary>
              <ChoiceField
                id={`${id}-initial`}
                label={t(($) => $.workflow.start)}
                value={value.initialKey}
                items={value.statuses.map((s) => ({
                  value: s.key,
                  label: s.name || t(($) => $.workflow.new_status),
                }))}
                onChange={(initialKey) => onChange({ ...value, initialKey })}
              />
              <ChoiceField
                id={`${id}-phase`}
                label={t(($) => $.workflow.phase)}
                value={status.phase}
                items={phases.map((v) => ({ value: v, label: phaseLabels[v] }))}
                onChange={(v) => update({ phase: v as IssueWorkflowPhase })}
              />
              <div className="space-y-1.5">
                <label htmlFor={`${id}-color`}>
                  {t(($) => $.workflow_rules.color_label)}
                </label>
                <Input
                  id={`${id}-color`}
                  type="color"
                  value={status.color}
                  onChange={(event) => update({ color: event.target.value })}
                  className="w-16 p-1"
                />
              </div>
              <div className="space-y-1.5">
                <label htmlFor={`${id}-description`}>
                  {t(($) => $.workflow.description)}
                </label>
                <Textarea
                  id={`${id}-description`}
                  maxLength={256}
                  value={status.description}
                  onChange={(event) =>
                    update({ description: event.target.value })
                  }
                />
              </div>
            </details>
          </section>
        )}
      </div>
      {value.statuses.find((s) => s.key === value.initialKey)?.policy.executor
        .type !== "none" && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.workflow.initial_auto_hint)}
        </p>
      )}
      {showErrors && problems.length > 0 && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.workflow.problems[problems[0]!.problem])}
        </p>
      )}
    </fieldset>
  );
}
