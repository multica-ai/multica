"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, clientErrorMessage } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useSetWorkflowHandoff, workflowNextStatus } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { IssueWorkflow, IssueWorkflowStep, Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ChevronDown } from "lucide-react";
import { ActorAvatar } from "../common/actor-avatar";
import { PickerEmpty, PickerItem, PickerSection, PropertyPicker } from "../issues/components/pickers/property-picker";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";

const NOT_SET = "__not_set__";

type HandoffMode = "none" | "agent" | "member";

function modeOf(handler: IssueWorkflowStep["handler"]): HandoffMode {
  if (handler.type === "agent") return "agent";
  if (handler.type === "member" || handler.type === "project_lead") return "member";
  return "none";
}

/**
 * The parts of a handoff that are saved. Instructions and next/back statuses
 * only reach an agent's brief, so they are kept for agent handoffs alone.
 */
export function savedHandoff<T extends Pick<IssueWorkflowStep, "handler" | "instructions" | "next_status_key" | "back_status_key">>(
  step: T,
): T {
  if (step.handler.type === "agent") return step;
  return { ...step, instructions: "", next_status_key: undefined, back_status_key: undefined };
}

function sameStep(a: IssueWorkflowStep, b: IssueWorkflowStep): boolean {
  return (
    a.handler.type === b.handler.type &&
    (a.handler.id ?? "") === (b.handler.id ?? "") &&
    a.instructions === b.instructions &&
    (a.next_status_key ?? "") === (b.next_status_key ?? "") &&
    (a.back_status_key ?? "") === (b.back_status_key ?? "")
  );
}

/**
 * The other projects using `workflow` besides `projectId`, and a short label
 * naming them ("A、B" or "A、B、C and 2 more").
 */
export function useWorkflowSharing(workflow: IssueWorkflow, projectId: string | undefined) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  return useMemo(() => {
    const others = workflow.project_ids
      .filter((id) => id !== projectId)
      .map((id) => projects.find((p) => p.id === id))
      .filter((p): p is Project => !!p);
    const separator = t(($) => $.workflows.list_separator);
    const names = others.slice(0, 3).map((p) => p.title).join(separator);
    const label =
      others.length > 3 ? t(($) => $.workflows.handoff.scope_more, { names, count: others.length }) : names;
    return { others, label };
  }, [workflow.project_ids, projectId, projects, t]);
}

type SaveScope = "all" | "project";

/**
 * One column's handoff (MUL-7420): who entering the column hands the issue
 * to, what they are told, and where it goes next. It belongs to the workflow,
 * so saving applies to every project using it — unless, opened from one of
 * several projects sharing a named workflow, the admin chooses to keep the
 * change to that project: it then lands on a copy of the workflow only that
 * project uses. The board's column popover and the full-flow panel both
 * render this form.
 */
export function WorkflowHandoffForm({
  workflow,
  statusKey,
  canEdit,
  project,
  onSaved,
}: {
  workflow: IssueWorkflow;
  statusKey: string;
  canEdit: boolean;
  /** The project the form is opened from: the brief preview is for it. */
  project?: Pick<Project, "id" | "title">;
  onSaved?: () => void;
}) {
  const { t } = useT("issues");
  const sharing = useWorkflowSharing(workflow, project?.id);
  const [scope, setScope] = useState<SaveScope>("all");
  const saved = useMemo<IssueWorkflowStep>(
    () =>
      workflow.steps.find((s) => s.status_key === statusKey) ?? {
        status_key: statusKey,
        handler: { type: "none" },
        instructions: "",
      },
    [statusKey, workflow.steps],
  );
  const [draft, setDraft] = useState<IssueWorkflowStep>(saved);
  const [previewOpen, setPreviewOpen] = useState(false);
  const setHandoff = useSetWorkflowHandoff();
  useEffect(() => setDraft(saved), [saved]);

  const handler = draft.handler;
  const needsTarget = (handler.type === "agent" || handler.type === "member") && !handler.id;
  const dirty = !sameStep(savedHandoff(draft), savedHandoff(saved));
  const askScope = canEdit && dirty && !!project && !workflow.is_default && sharing.others.length > 0;
  useEffect(() => {
    if (!dirty) setScope("all");
  }, [dirty]);

  const save = async () => {
    const out = savedHandoff(draft);
    const copyFor = askScope && scope === "project" ? project : undefined;
    try {
      const result = await setHandoff.mutateAsync({
        workflowId: workflow.id,
        statusKey,
        handler: out.handler,
        instructions: out.instructions,
        next_status_key: out.next_status_key,
        back_status_key: out.back_status_key,
        copyForProjectId: copyFor?.id,
      });
      toast.success(
        copyFor
          ? t(($) => $.workflows.handoff.saved_copy, { workflow: result.name, project: copyFor.title })
          : t(($) => $.workflows.handoff.saved),
      );
      onSaved?.();
    } catch (err) {
      toast.error(clientErrorMessage(err) ?? t(($) => $.workflows.handoff.error));
    }
  };

  return (
    <div className="space-y-4">
      <HandoffFields
        step={draft}
        workflow={workflow}
        canEdit={canEdit}
        onPatch={(patch) => setDraft((d) => ({ ...d, ...patch }))}
        onPreview={() => setPreviewOpen(true)}
      />
      {askScope && project && (
        <div className="rounded-lg border border-surface-border bg-muted/40 p-2">
          <p className="px-1.5 pb-1.5 pt-0.5 text-caption font-medium text-muted-foreground">
            {t(($) => $.workflows.handoff.scope_question)}
          </p>
          <div role="radiogroup" aria-label={t(($) => $.workflows.handoff.scope_question)} className="space-y-1">
            <ScopeOption
              selected={scope === "all"}
              onSelect={() => setScope("all")}
              title={t(($) => $.workflows.handoff.scope_all, {
                workflow: workflow.name,
                count: sharing.others.length + 1,
              })}
              hint={t(($) => $.workflows.handoff.scope_all_hint, {
                projects: [project.title, sharing.label].join(t(($) => $.workflows.list_separator)),
              })}
            />
            <ScopeOption
              selected={scope === "project"}
              onSelect={() => setScope("project")}
              title={t(($) => $.workflows.handoff.scope_project, { project: project.title })}
              hint={t(($) => $.workflows.handoff.scope_project_hint, {
                copy: `${workflow.name} (${project.title})`,
                project: project.title,
              })}
            />
          </div>
        </div>
      )}
      {canEdit ? (
        <div className="flex items-center justify-end gap-2">
          {dirty && (
            <Button variant="ghost" size="sm" onClick={() => setDraft(saved)}>
              {t(($) => $.workflows.editor.cancel)}
            </Button>
          )}
          <Button size="sm" onClick={() => void save()} disabled={!dirty || needsTarget || setHandoff.isPending}>
            {setHandoff.isPending && <Spinner className="size-3.5" />}
            {t(($) => $.workflows.editor.save)}
          </Button>
        </div>
      ) : (
        <p className="text-caption text-muted-foreground">{t(($) => $.workflows.handoff.read_only)}</p>
      )}
      <BriefPreviewDialog
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        workflowId={workflow.id}
        projectId={project?.id}
        step={draft}
      />
    </div>
  );
}

function RadioDot({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border",
        selected ? "border-brand" : "border-input",
      )}
    >
      {selected && <span className="size-2 rounded-full bg-brand" />}
    </span>
  );
}

function ScopeOption({
  selected,
  onSelect,
  title,
  hint,
}: {
  selected: boolean;
  onSelect: () => void;
  title: string;
  hint: string;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors",
        selected ? "bg-background shadow-xs ring-1 ring-surface-border" : "hover:bg-accent/60",
      )}
    >
      <RadioDot selected={selected} />
      <span className="min-w-0">
        <span className="block text-body font-medium">{title}</span>
        <span className="block text-caption leading-5 text-muted-foreground">{hint}</span>
      </span>
    </button>
  );
}

/** The handoff fields of one column; the caller owns saving. */
export function HandoffFields({
  step,
  workflow,
  canEdit,
  onPatch,
  onPreview,
}: {
  step: IssueWorkflowStep;
  workflow: Pick<IssueWorkflow, "steps">;
  canEdit: boolean;
  onPatch: (patch: Partial<IssueWorkflowStep>) => void;
  onPreview?: () => void;
}) {
  const { t } = useT("issues");
  const mode = modeOf(step.handler);
  const nextDefault = workflowNextStatus({ steps: workflow.steps.map((s) => ({ ...s, next_status_key: undefined })) }, step.status_key);
  const choose = (m: HandoffMode) => {
    if (m === mode) return;
    onPatch({ handler: m === "none" ? { type: "none" } : { type: m } });
  };
  const options: { mode: HandoffMode; title: string; hint: string }[] = [
    { mode: "none", title: t(($) => $.workflows.handoff.mode_none), hint: t(($) => $.workflows.handoff.mode_none_hint) },
    { mode: "agent", title: t(($) => $.workflows.handoff.mode_agent), hint: t(($) => $.workflows.handoff.mode_agent_hint) },
    { mode: "member", title: t(($) => $.workflows.handoff.mode_member), hint: t(($) => $.workflows.handoff.mode_member_hint) },
  ];

  return (
    <div className="space-y-4">
      <div role="radiogroup" aria-label={t(($) => $.workflows.handoff.to)} className="space-y-1.5">
        {options.map((option) => {
          const selected = option.mode === mode;
          return (
            <div
              key={option.mode}
              className={cn(
                "rounded-lg border transition-colors",
                selected ? "border-brand/50 bg-brand/5" : "border-surface-border",
              )}
            >
              <button
                type="button"
                role="radio"
                aria-checked={selected}
                disabled={!canEdit}
                onClick={() => choose(option.mode)}
                className="flex w-full items-start gap-2.5 px-3 py-2.5 text-left disabled:cursor-default"
              >
                <RadioDot selected={selected} />
                <span className="min-w-0">
                  <span className="block text-body font-medium">{option.title}</span>
                  <span className="block text-caption text-muted-foreground">{option.hint}</span>
                </span>
              </button>
              {selected && option.mode !== "none" && (
                <div className="px-3 pb-3 pl-9">
                  <HandlerPicker
                    kind={option.mode}
                    value={step.handler}
                    disabled={!canEdit}
                    onChange={(handler) => onPatch({ handler })}
                  />
                  {step.handler.type === "project_lead" && (
                    <p className="mt-1.5 text-caption leading-5 text-muted-foreground">
                      {t(($) => $.workflows.handoff.project_lead_hint)}
                    </p>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {mode === "agent" && (
        <>
          <Field
            label={t(($) => $.workflows.handoff.instructions)}
            hint={t(($) => $.workflows.editor.instructions_hint)}
            htmlFor={`workflow-handoff-instructions-${step.status_key}`}
          >
            <Textarea
              id={`workflow-handoff-instructions-${step.status_key}`}
              rows={4}
              maxLength={8000}
              readOnly={!canEdit}
              value={step.instructions}
              placeholder={t(($) => $.workflows.editor.instructions_placeholder)}
              onChange={(e) => onPatch({ instructions: e.target.value })}
            />
          </Field>
          <NextBackFields
            step={step}
            workflow={workflow}
            canEdit={canEdit}
            nextDefault={nextDefault}
            onPatch={onPatch}
            onPreview={onPreview}
          />
        </>
      )}
    </div>
  );
}

/**
 * The agent, or the member (project lead included), a column hands the issue
 * to. Squads are left out on purpose — they route work themselves. (MUL-7420)
 */
function HandlerPicker({
  kind,
  value,
  disabled,
  onChange,
}: {
  kind: "agent" | "member";
  value: IssueWorkflowStep["handler"];
  disabled: boolean;
  onChange: (handler: IssueWorkflowStep["handler"]) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const matches = (name: string) => !q || name.toLowerCase().includes(q);
  const agentRows = kind === "agent" ? agents.filter((a) => !a.archived_at && matches(a.name)) : [];
  const memberRows = kind === "member" ? members.filter((m) => matches(m.name)) : [];
  const leadLabel = t(($) => $.workflows.handler.project_lead);
  const pick = (handler: IssueWorkflowStep["handler"]) => {
    onChange(handler);
    setOpen(false);
  };
  const is = (type: string, id?: string) => value.type === type && (id === undefined || value.id === id);
  const current =
    value.type === "project_lead"
      ? leadLabel
      : value.id
        ? (value.type === "agent" ? agents.find((a) => a.id === value.id)?.name : members.find((m) => m.user_id === value.id)?.name) ?? ""
        : "";
  const placeholder = kind === "agent" ? t(($) => $.workflows.handoff.pick_agent) : t(($) => $.workflows.handoff.pick_member);

  return (
    <PropertyPicker
      open={open && !disabled}
      onOpenChange={setOpen}
      align="start"
      width="w-72"
      searchable
      searchPlaceholder={kind === "agent" ? t(($) => $.workflows.handoff.search_agents) : t(($) => $.workflows.handoff.search_members)}
      onSearchChange={setQuery}
      triggerRender={
        <button
          type="button"
          disabled={disabled}
          aria-label={placeholder}
          className="flex h-8 w-full items-center gap-2 rounded-md border border-input bg-background px-2.5 text-left text-body disabled:cursor-default"
        />
      }
      trigger={
        <>
          {(value.type === "agent" || value.type === "member") && value.id && (
            <ActorAvatar actorType={value.type} actorId={value.id} size="xs" profileLink={false} />
          )}
          <span className={cn("min-w-0 flex-1 truncate", !current && "text-muted-foreground")}>{current || placeholder}</span>
          <ChevronDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        </>
      }
    >
      {kind === "member" && !q && (
        <PickerItem selected={is("project_lead")} onClick={() => pick({ type: "project_lead" })}>
          <span className="truncate">{leadLabel}</span>
        </PickerItem>
      )}
      {agentRows.map((agent) => (
        <PickerItem key={agent.id} selected={is("agent", agent.id)} onClick={() => pick({ type: "agent", id: agent.id })}>
          <ActorAvatar actorType="agent" actorId={agent.id} size="xs" profileLink={false} />
          <span className="truncate">{agent.name}</span>
        </PickerItem>
      ))}
      {memberRows.length > 0 && (
        <PickerSection label={t(($) => $.workflows.handoff.members)}>
          {memberRows.map((member) => (
            <PickerItem
              key={member.user_id}
              selected={is("member", member.user_id)}
              onClick={() => pick({ type: "member", id: member.user_id })}
            >
              <ActorAvatar actorType="member" actorId={member.user_id} size="xs" profileLink={false} />
              <span className="truncate">{member.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}
      {agentRows.length === 0 && memberRows.length === 0 && (q || kind === "agent") && <PickerEmpty />}
    </PropertyPicker>
  );
}

function NextBackFields({
  step,
  workflow,
  canEdit,
  nextDefault,
  onPatch,
  onPreview,
}: {
  step: IssueWorkflowStep;
  workflow: Pick<IssueWorkflow, "steps">;
  canEdit: boolean;
  nextDefault: string | null;
  onPatch: (patch: Partial<IssueWorkflowStep>) => void;
  onPreview?: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const others = workflow.steps.filter((s) => s.status_key !== step.status_key).map((s) => s.status_key);
  const statusIcon = (key: string) => (
    <StatusIcon
      status={key}
      category={catalog.categoryOf(key)}
      color={catalog.colorOf(key)}
      icon={catalog.iconOf(key)}
      className="h-3.5 w-3.5 shrink-0"
    />
  );

  return (
    <div className="space-y-2">
      <div className="grid gap-3 sm:grid-cols-2">
        {(["next", "back"] as const).map((kind) => {
          const field = kind === "next" ? "next_status_key" : "back_status_key";
          const value = step[field];
          const unsetLabel =
            kind === "next" && nextDefault
              ? t(($) => $.workflows.handoff.next_default, { status: labelOf(nextDefault) })
              : t(($) => $.workflows.editor.none_option);
          const options = [{ value: NOT_SET, label: unsetLabel }, ...others.map((key) => ({ value: key, label: labelOf(key) }))];
          return (
            <div key={kind} className="min-w-0 space-y-1.5">
              <span className="block text-caption font-medium">{t(($) => $.workflows.editor[kind])}</span>
              <Select
                items={options}
                value={value ?? NOT_SET}
                disabled={!canEdit}
                onValueChange={(v) => onPatch({ [field]: !v || v === NOT_SET ? undefined : v })}
              >
                <SelectTrigger aria-label={t(($) => $.workflows.editor[kind])} className="w-full">
                  {value ? (
                    <span className="flex min-w-0 items-center gap-2">
                      {statusIcon(value)}
                      <span className="truncate">{labelOf(value)}</span>
                    </span>
                  ) : (
                    <span className="truncate text-muted-foreground">{unsetLabel}</span>
                  )}
                </SelectTrigger>
                <SelectContent>
                  {options.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      <span className="flex items-center gap-2">
                        {option.value !== NOT_SET && statusIcon(option.value)}
                        <span>{option.label}</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          );
        })}
      </div>
      <p className="text-caption leading-5 text-muted-foreground">
        {t(($) => $.workflows.editor.next_hint)}
        {onPreview && (
          <>
            {" "}
            <button type="button" onClick={onPreview} className="text-brand hover:underline">
              {t(($) => $.workflows.editor.preview_brief)}
            </button>
          </>
        )}
      </p>
    </div>
  );
}

function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="block text-caption font-medium">
        {label}
      </label>
      {children}
      {hint && <p className="text-caption leading-5 text-muted-foreground">{hint}</p>}
    </div>
  );
}

/**
 * The brief a column's handler receives, rendered by the server from the
 * unsaved handoff on the saved workflow.
 */
export function BriefPreviewDialog({
  open,
  onOpenChange,
  workflowId,
  projectId,
  step,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workflowId: string;
  projectId?: string;
  step: IssueWorkflowStep;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const { data: brief, isPending } = useQuery({
    queryKey: ["issue-workflows", wsId, "brief-preview", workflowId, projectId ?? "", JSON.stringify(step)],
    queryFn: () =>
      api.previewWorkflowBrief(workflowId, {
        status_key: step.status_key,
        ...(projectId ? { project_id: projectId } : {}),
        handler: step.handler,
        instructions: step.instructions,
        next_status_key: step.next_status_key,
        back_status_key: step.back_status_key,
      }),
    enabled: open,
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.workflows.editor.preview_title, { status: labelOf(step.status_key) })}</DialogTitle>
          <DialogDescription>{t(($) => $.workflows.editor.preview_description)}</DialogDescription>
        </DialogHeader>
        <BriefBlock brief={brief} loading={isPending} />
      </DialogContent>
    </Dialog>
  );
}

/** A handoff brief, shown as the agent reads it. */
export function BriefBlock({ brief, loading }: { brief: string | undefined; loading?: boolean }) {
  return (
    <div className="max-h-[50dvh] overflow-auto rounded-lg border border-surface-border bg-muted/40 p-4">
      {loading ? (
        <Spinner className="size-4" />
      ) : (
        <pre className="whitespace-pre-wrap break-words font-mono text-caption leading-5 text-foreground">{brief}</pre>
      )}
    </div>
  );
}
