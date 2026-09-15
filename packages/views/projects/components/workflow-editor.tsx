"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Bot,
  GripVertical,
  Plus,
  Trash2,
  User,
  ChevronRight,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
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
import type { IssueWorkflowPhase } from "@multica/core/types";
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
import { Field, FieldLabel, FieldDescription, FieldError, FieldGroup } from "@multica/ui/components/ui/field";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { WorkflowExecutorPicker } from "./workflow-executor-picker";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface Choice {
  value: string;
  label: string;
  disabled?: boolean;
}
function ChoiceField({
  id,
  label,
  value,
  items,
  onChange,
  placeholder,
}: {
  id: string;
  label: string;
  value: string;
  items: Choice[];
  placeholder?: string;
  onChange: (value: string) => void;
}) {
  return (
    <Field className="gap-1.5">
      <FieldLabel htmlFor={id} className="text-caption font-medium">
        {label}
      </FieldLabel>
      <Select
        items={items}
        value={value || null}
        onValueChange={(v) => {
          if (v !== null) onChange(v);
        }}
      >
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value} disabled={item.disabled}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

export function WorkflowEditor({
  value,
  onChange,
  showErrors = false,
  disabled = false,
  statusKey,
}: {
  value: WorkflowDraft;
  onChange: (draft: WorkflowDraft) => void;
  showErrors?: boolean;
  disabled?: boolean;
  statusKey?: string;
}) {
  const { t } = useT("projects");
  const id = useId();
  const editorRef = useRef<HTMLFieldSetElement>(null);
  useEffect(() => {
    if (showErrors) editorRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }, [showErrors]);
  const wsId = useWorkspaceId();
  const agents = useQuery(agentListOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const [selectedKey, setSelectedKey] = useState(value.initialKey);
  const status =
    statusKey
      ? value.statuses.find((s) => s.key === statusKey)
      : value.statuses.find((s) => s.key === selectedKey) ?? value.statuses[0];
  const index = status ? value.statuses.indexOf(status) : -1;
  const problems = workflowProblems(value);
  const agentChoices = (agents.data ?? [])
    .filter((agent) => !agent.archived_at)
    .map((agent) => ({ value: agent.id, label: agent.name }));
  const squadChoices = (squads.data ?? [])
    .filter((squad) => !squad.archived_at)
    .map((squad) => ({ value: squad.id, label: squad.name }));
  const nameFor = (s: WorkflowStatus) => {
    const executor = s.policy.executor;
    if (executor.type === "none") return t(($) => $.workflow_rules.no_automatic_run);
    if (!executor.id) return t(($) => executor.type === "agent" ? $.workflow.choose_agent : $.workflow.choose_squad);
    const name = (executor.type === "agent" ? agentChoices : squadChoices)
      .find((choice) => choice.value === executor.id)?.label;
    return name
      ? t(($) => executor.type === "agent" ? $.workflow_rules.agent_target : $.workflow_rules.squad_target, { name })
      : t(($) => $.workflow.unavailable);
  };
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
    const statuses = [...value.statuses];
    const newStatus: WorkflowStatus = {
      key,
      name: "",
      description: "",
      color: "#6b7280",
      phase: "started",
      policy: manualEntryPolicy(),
    };
    statuses.splice(after + 1, 0, newStatus);
    onChange({ ...value, initialKey: value.initialKey || key, statuses });
    setSelectedKey(key);
  };
  const move = (from: number, to: number) =>
    onChange(moveWorkflowStatus(value, from, to));
  const phases: IssueWorkflowPhase[] = [
    "unstarted",
    "started",
    "done",
    "closed",
  ];
  const phaseLabels = {
    unstarted: t(($) => $.workflow_rules.phase_unstarted),
    started: t(($) => $.workflow_rules.phase_started),
    done: t(($) => $.workflow_rules.phase_done),
    closed: t(($) => $.workflow_rules.phase_closed),
  };
  const executor = status?.policy.executor;
  const executorType = executor?.type ?? "none";
  const executorId = executor && executor.type !== "none" ? executor.id : "";
  const targetQuery = executorType === "agent" ? agents : squads;
  const fieldError = (field: "name" | "executor" | "instructions") => {
    const problem = showErrors && problems.find((p) => p.key === status?.key &&
      (p.problem === field || (field === "name" && p.problem === "duplicate")));
    return problem ? t(($) => $.workflow.problems[problem.problem]) : undefined;
  };

  return (
    <fieldset ref={editorRef} disabled={disabled} className="min-w-0 space-y-4">
      <div className={cn("grid min-w-0 gap-6", !statusKey && "md:grid-cols-2")}>
        {!statusKey && (
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
            </div>
          ))}
          {value.statuses.length < 50 && (
            <Button variant="outline" onClick={() => add(value.statuses.length - 1)}>
              <Plus className="size-3" />
              {t(($) => $.workflow.add)}
            </Button>
          )}
          <p className="pt-3 text-caption text-muted-foreground">
            {t(($) => $.workflow.reorder_hint)}
          </p>
        </section>
        )}
        {status && (
          <section
            aria-label={t(($) => $.workflow.editor_title)}
            className={cn("min-w-0 space-y-4", !statusKey && "border-t pt-5 md:border-l md:border-t-0 md:pl-6 md:pt-0")}
          >
            {!statusKey && (<div className="flex items-center justify-between gap-2">
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
            )}
            <FieldGroup>
              <Field className="gap-1.5" data-invalid={!!fieldError("name")}>
                <FieldLabel
                  htmlFor={`${id}-name`}
                  className="text-caption font-medium"
                >
                  {t(($) => $.workflow.status_name)}
                </FieldLabel>
                <Input
                  id={`${id}-name`}
                  aria-invalid={!!fieldError("name") || undefined}
                  aria-describedby={fieldError("name") ? `${id}-name-error` : undefined}
                  maxLength={64}
                  value={status.name}
                  onChange={(event) => update({ name: event.target.value })}
                />
                <FieldError id={`${id}-name-error`} className="text-caption">{fieldError("name")}</FieldError>
              </Field>
              <ChoiceField
                id={`${id}-action`}
                label={t(($) => $.workflow.run_on_entry)}
                value={executorType}
                items={[
                  { value: "none", label: t(($) => $.workflow_rules.no_automatic_run) },
                  { value: "agent", label: t(($) => $.workflow.run_agent) },
                  { value: "squad", label: t(($) => $.workflow.run_squad) },
                ]}
                onChange={(type) => {
                  if (type === executorType) return;
                  if (type === "none") {
                    updatePolicy({ executor: { type: "none" }, instructions: "" });
                  } else if (type === "agent" || type === "squad") {
                    updatePolicy({ executor: { type, id: "" } });
                  }
                }}
              />
              {executorType !== "none" && (
                <Field className="gap-1.5" data-invalid={!!fieldError("executor")}>
                  <FieldLabel htmlFor={`${id}-executor`} className="text-caption font-medium">
                    {t(($) => executorType === "agent" ? $.workflow.agent : $.workflow.squad)}
                  </FieldLabel>
                  <WorkflowExecutorPicker
                    key={`${status.key}-${executorType}`}
                    id={`${id}-executor`}
                    type={executorType}
                    value={executorId}
                    choices={executorType === "agent" ? agentChoices : squadChoices}
                    disabled={disabled}
                    invalid={!!fieldError("executor")}
                    loading={targetQuery.isPending}
                    loadError={targetQuery.isError}
                    onRetry={() => { void targetQuery.refetch(); }}
                    onChange={(targetId) => updatePolicy({ executor: { type: executorType, id: targetId } })}
                  />
                  <FieldError id={`${id}-executor-error`} className="text-caption">{fieldError("executor")}</FieldError>
                </Field>
              )}
              {status.policy.executor.type !== "none" && (
                <Field className="gap-1.5" data-invalid={!!fieldError("instructions")}>
                  <FieldLabel
                    htmlFor={`${id}-instructions`}
                    className="text-caption font-medium"
                  >
                    {t(($) => $.workflow.instructions)}
                  </FieldLabel>
                  <Textarea
                    id={`${id}-instructions`}
                    aria-invalid={!!fieldError("instructions") || undefined}
                    aria-describedby={fieldError("instructions") ? `${id}-instructions-error` : `${id}-instructions-hint`}
                    placeholder={t(($) => $.workflow.instructions_placeholder)}
                    rows={4}
                    value={status.policy.instructions}
                    onChange={(event) =>
                      updatePolicy({ instructions: event.target.value })
                    }
                  />
                  <FieldDescription id={`${id}-instructions-hint`} className="text-caption">
                    {t(($) => $.workflow.instructions_hint)}
                  </FieldDescription>
                  <FieldError id={`${id}-instructions-error`} className="text-caption">{fieldError("instructions")}</FieldError>
                </Field>
              )}
              <Collapsible>
                <CollapsibleTrigger render={<Button variant="ghost" size="sm" className="group -ml-2 text-muted-foreground" />}>
                  <ChevronRight className="size-3.5 group-data-[panel-open]:rotate-90" />
                  {t(($) => $.workflow.advanced)}
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <FieldGroup className="pt-4">
                    {!statusKey && (<ChoiceField
                      id={`${id}-initial`}
                      label={t(($) => $.workflow.start)}
                      value={value.initialKey}
                      items={value.statuses.map((s) => ({
                        value: s.key,
                        label: s.name || t(($) => $.workflow.new_status),
                      }))}
                      onChange={(initialKey) => onChange({ ...value, initialKey })}
                    />)}
                    <ChoiceField
                      id={`${id}-phase`}
                      label={t(($) => $.workflow.phase)}
                      value={status.phase}
                      items={phases.map((v) => ({ value: v, label: phaseLabels[v] }))}
                      onChange={(v) => update({ phase: v as IssueWorkflowPhase })}
                    />
                    <Field className="gap-1.5">
                      <FieldLabel htmlFor={`${id}-color`} className="text-caption font-medium">
                        {t(($) => $.workflow_rules.color_label)}
                      </FieldLabel>
                      <Input
                        id={`${id}-color`}
                        type="color"
                        value={status.color}
                        onChange={(event) => update({ color: event.target.value })}
                        className="max-w-16 p-1"
                      />
                    </Field>
                    <Field className="gap-1.5">
                      <FieldLabel htmlFor={`${id}-description`} className="text-caption font-medium">
                        {t(($) => $.workflow.description)}
                      </FieldLabel>
                      <Textarea
                        id={`${id}-description`}
                        maxLength={256}
                        value={status.description}
                        onChange={(event) =>
                          update({ description: event.target.value })
                        }
                      />
                    </Field>
                  </FieldGroup>
                </CollapsibleContent>
              </Collapsible>
            </FieldGroup>
          </section>
        )}
      </div>
      {!statusKey && value.statuses.find((s) => s.key === value.initialKey)?.policy.executor
        .type !== "none" && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.workflow.initial_auto_hint)}
        </p>
      )}
      {showErrors && problems.length > 0 && !problems.some((p) => p.key === status?.key && ["name", "duplicate", "executor", "instructions"].includes(p.problem)) && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.workflow.problems[problems[0]!.problem])}
        </p>
      )}
    </fieldset>
  );
}
