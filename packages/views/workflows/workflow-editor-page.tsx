"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ChevronRight,
  Copy,
  CornerDownRight,
  Flag,
  GripVertical,
  MoreHorizontal,
  Plus,
  Trash2,
  User,
} from "lucide-react";
import { toast } from "sonner";
import { ApiError, api, clientErrorMessage } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { statusColumnKeys } from "@multica/core/issues";
import {
  useCreateIssueWorkflow,
  useDeleteIssueWorkflow,
  useIssueWorkflows,
  useUpdateIssueWorkflow,
} from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type {
  IssueWorkflow,
  IssueWorkflowHandlerType,
  IssueWorkflowMappingPlan,
  IssueWorkflowStep,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";
import { WorkflowMappingDialog } from "./workflow-mapping-dialog";
import { useStepHandlerLabel } from "./step-handler";

const HANDLER_TYPES: IssueWorkflowHandlerType[] = [
  "none",
  "agent",
  "squad",
  "member",
  "project_lead",
  "creator",
];
const NOT_SET = "__not_set__";
/** A new workflow starts from the common delivery loop instead of nothing. */
const NEW_WORKFLOW_STEPS = ["todo", "in_progress", "in_review", "done"];

interface Draft {
  name: string;
  description: string;
  initial: string;
  steps: IssueWorkflowStep[];
}

function draftFrom(workflow: IssueWorkflow): Draft {
  return {
    name: workflow.name,
    description: workflow.description,
    initial: workflow.initial_status_key,
    steps: workflow.steps.map((s) => ({ ...s, handler: { ...s.handler } })),
  };
}

function sameDraft(a: Draft, b: Draft): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** Next/back pointers and the starting step must name steps still present. */
function normalizeDraft(draft: Draft): Draft {
  const keys = new Set(draft.steps.map((s) => s.status_key));
  return {
    ...draft,
    initial: keys.has(draft.initial) ? draft.initial : draft.steps[0]?.status_key ?? "",
    steps: draft.steps.map((s) => ({
      ...s,
      next_status_key: s.next_status_key && keys.has(s.next_status_key) ? s.next_status_key : undefined,
      back_status_key: s.back_status_key && keys.has(s.back_status_key) ? s.back_status_key : undefined,
    })),
  };
}

function mappingPlanFrom(err: unknown): IssueWorkflowMappingPlan | null {
  if (!(err instanceof ApiError) || err.status !== 409 || !err.body || typeof err.body !== "object") return null;
  const body = err.body as { code?: unknown; plan?: IssueWorkflowMappingPlan };
  if (body.code !== "workflow_status_mapping_required" || !body.plan) return null;
  return { ...body.plan, unchanged: body.plan.unchanged ?? [] };
}

/**
 * The workflow editor (MUL-7420): a full settings page, not a dialog, because
 * a workflow is read as a whole — its steps on the left in board order, the
 * selected step's handoff on the right.
 *
 * `workflowId` is null for a new workflow; `seed` pre-fills one (duplicate).
 */
export function WorkflowEditorPage({
  workflowId,
  seed,
  canEdit,
  onBack,
  onOpen,
}: {
  workflowId: string | null;
  seed: IssueWorkflow | null;
  canEdit: boolean;
  onBack: () => void;
  /** Navigates to another workflow's editor (after create, or to a duplicate). */
  onOpen: (target: { id: string } | { duplicateOf: string }) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const { workflows, isLoaded } = useIssueWorkflows(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const createWorkflow = useCreateIssueWorkflow();
  const updateWorkflow = useUpdateIssueWorkflow();
  const deleteWorkflow = useDeleteIssueWorkflow();

  const saved = workflowId ? workflows.find((w) => w.id === workflowId) ?? null : null;
  const baseline = useMemo<Draft>(() => {
    if (saved) return draftFrom(saved);
    if (seed) {
      return { ...draftFrom(seed), name: t(($) => $.workflows.editor.copy_name, { name: seed.name }) };
    }
    return {
      name: "",
      description: "",
      initial: NEW_WORKFLOW_STEPS[0]!,
      steps: NEW_WORKFLOW_STEPS.map((key) => ({ status_key: key, handler: { type: "none" }, instructions: "" })),
    };
    // Re-seed only when the target changes or the saved row is refreshed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [saved?.updated_at, workflowId, seed?.id]);

  const [draft, setDraft] = useState<Draft>(baseline);
  const [selected, setSelected] = useState<string | null>(baseline.steps[0]?.status_key ?? null);
  const [mappingPlan, setMappingPlan] = useState<IssueWorkflowMappingPlan | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);

  useEffect(() => {
    setDraft(baseline);
    setSelected((current) =>
      baseline.steps.some((s) => s.status_key === current)
        ? current
        : baseline.steps.find((s) => s.handler.type !== "none")?.status_key ?? baseline.steps[0]?.status_key ?? null,
    );
  }, [baseline]);

  const dirty = !workflowId || !sameDraft(draft, baseline);
  const pending = createWorkflow.isPending || updateWorkflow.isPending;
  const canSave = canEdit && dirty && draft.name.trim().length > 0 && draft.steps.length > 0 && !pending;
  const usedBy = saved ? projects.filter((p) => saved.project_ids.includes(p.id)) : [];
  const selectedStep = draft.steps.find((s) => s.status_key === selected);
  const stepKeys = draft.steps.map((s) => s.status_key);
  const addable = statusColumnKeys(catalog).filter((key) => !stepKeys.includes(key));

  const update = (fn: (d: Draft) => Draft) => setDraft((current) => normalizeDraft(fn(current)));
  const patchStep = (key: string, patch: Partial<IssueWorkflowStep>) =>
    update((d) => ({ ...d, steps: d.steps.map((s) => (s.status_key === key ? { ...s, ...patch } : s)) }));

  // Swapping the status a step uses keeps its handoff and moves every
  // pointer to it along. (MUL-7420)
  const swapStatus = (from: string, to: string) => {
    update((d) => ({
      ...d,
      initial: d.initial === from ? to : d.initial,
      steps: d.steps.map((s) => ({
        ...s,
        status_key: s.status_key === from ? to : s.status_key,
        next_status_key: s.next_status_key === from ? to : s.next_status_key,
        back_status_key: s.back_status_key === from ? to : s.back_status_key,
      })),
    }));
    setSelected(to);
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    update((d) => ({
      ...d,
      steps: arrayMove(d.steps, stepKeys.indexOf(String(active.id)), stepKeys.indexOf(String(over.id))),
    }));
  };

  const save = async (mapping?: Record<string, string>) => {
    const body = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      initial_status_key: draft.initial,
      steps: draft.steps,
      ...(mapping ? { status_mapping: mapping } : {}),
    };
    try {
      if (workflowId) {
        await updateWorkflow.mutateAsync({ id: workflowId, ...body });
        toast.success(t(($) => $.workflows.editor.saved));
      } else {
        const created = await createWorkflow.mutateAsync(body);
        toast.success(t(($) => $.workflows.editor.created));
        onOpen({ id: created.id });
      }
      setMappingPlan(null);
    } catch (err) {
      const plan = mappingPlanFrom(err);
      if (plan) {
        setMappingPlan(plan);
        return;
      }
      toast.error(clientErrorMessage(err) ?? t(($) => $.workflows.editor.error));
    }
  };

  const remove = async () => {
    if (!workflowId) return;
    try {
      await deleteWorkflow.mutateAsync(workflowId);
      toast.success(t(($) => $.workflows.settings.deleted));
      onBack();
    } catch (err) {
      toast.error(clientErrorMessage(err) ?? t(($) => $.workflows.settings.delete_error));
    }
  };

  if (workflowId && isLoaded && !saved) {
    return (
      <div className="space-y-4">
        <Breadcrumb onBack={onBack} current="" />
        <p className="text-body text-muted-foreground">{t(($) => $.workflows.editor.not_found)}</p>
      </div>
    );
  }

  const title = draft.name.trim() || t(($) => $.workflows.editor.create_title);

  return (
    <div className="space-y-6">
      <Breadcrumb onBack={onBack} current={title} />
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1 space-y-1">
          <input
            aria-label={t(($) => $.workflows.editor.name)}
            value={draft.name}
            maxLength={64}
            readOnly={!canEdit}
            placeholder={t(($) => $.workflows.editor.name_placeholder)}
            onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            className="w-full bg-transparent text-title-lg font-semibold tracking-tight outline-none placeholder:text-muted-foreground/60"
          />
          <input
            aria-label={t(($) => $.workflows.editor.description)}
            value={draft.description}
            maxLength={256}
            readOnly={!canEdit}
            placeholder={canEdit ? t(($) => $.workflows.editor.description_placeholder) : ""}
            onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
            className="w-full bg-transparent text-body text-muted-foreground outline-none placeholder:text-muted-foreground/50"
          />
          <p className="text-body text-muted-foreground">
            {usedBy.length > 0
              ? t(($) => $.workflows.editor.used_by_note, {
                  count: usedBy.length,
                  names: usedBy.map((p) => p.title).join(t(($) => $.workflows.list_separator)),
                })
              : t(($) => $.workflows.editor.unused_note)}
          </p>
        </div>
        {canEdit && (
          <div className="flex shrink-0 items-center gap-2">
            {dirty && workflowId && (
              <span className="text-caption text-muted-foreground">{t(($) => $.workflows.editor.unsaved)}</span>
            )}
            {workflowId && (
              <Button variant="outline" className="gap-1.5" onClick={() => onOpen({ duplicateOf: workflowId })}>
                <Copy className="size-3.5" />
                {t(($) => $.workflows.editor.duplicate)}
              </Button>
            )}
            <Button onClick={() => void save()} disabled={!canSave} aria-busy={pending}>
              {pending && <Spinner className="size-3.5" />}
              {workflowId ? t(($) => $.workflows.editor.save) : t(($) => $.workflows.editor.create)}
            </Button>
            {workflowId && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon" aria-label={t(($) => $.workflows.editor.more)}>
                      <MoreHorizontal className="size-4" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end" className="w-56">
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={usedBy.length > 0 || deleteWorkflow.isPending}
                    onClick={() => void remove()}
                  >
                    <Trash2 className="size-3.5" />
                    {t(($) => $.workflows.settings.delete)}
                  </DropdownMenuItem>
                  {usedBy.length > 0 && (
                    <p className="px-2 pb-1.5 text-caption text-muted-foreground">
                      {t(($) => $.workflows.settings.delete_in_use)}
                    </p>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        )}
      </div>

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
        <div className="min-w-0">
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={stepKeys} strategy={verticalListSortingStrategy}>
              <ol>
                {draft.steps.map((step, index) => (
                  <li key={step.status_key}>
                    {index > 0 && <div aria-hidden className="ml-[22px] h-3 w-px bg-border" />}
                    <StepCard
                      step={step}
                      initial={draft.initial === step.status_key}
                      selected={selected === step.status_key}
                      draggable={canEdit}
                      onSelect={() => setSelected(step.status_key)}
                    />
                  </li>
                ))}
              </ol>
            </SortableContext>
          </DndContext>
          {canEdit && (
            <DropdownMenu>
              <DropdownMenuTrigger
                disabled={addable.length === 0}
                render={
                  <Button variant="outline" className="mt-3 gap-1.5">
                    <Plus className="size-3.5" />
                    {t(($) => $.workflows.editor.add_step)}
                  </Button>
                }
              />
              <DropdownMenuContent align="start" className="max-h-80 w-56 overflow-y-auto">
                {addable.map((key) => (
                  <DropdownMenuItem
                    key={key}
                    onClick={() => {
                      update((d) => ({
                        ...d,
                        steps: [...d.steps, { status_key: key, handler: { type: "none" }, instructions: "" }],
                      }));
                      setSelected(key);
                    }}
                  >
                    <StatusIcon
                      status={key}
                      category={catalog.categoryOf(key)}
                      color={catalog.colorOf(key)}
                      icon={catalog.iconOf(key)}
                      className="h-3.5 w-3.5"
                    />
                    <span className="truncate">{labelOf(key)}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <p className="mt-2.5 text-caption text-muted-foreground">{t(($) => $.workflows.editor.steps_hint)}</p>
        </div>

        <div className="min-w-0 rounded-xl border border-surface-border bg-card p-5">
          {selectedStep ? (
            <StepConfig
              key={selectedStep.status_key}
              step={selectedStep}
              steps={draft.steps}
              initial={draft.initial === selectedStep.status_key}
              addable={addable}
              canEdit={canEdit}
              onPatch={(patch) => patchStep(selectedStep.status_key, patch)}
              onSwap={(to) => swapStatus(selectedStep.status_key, to)}
              onMakeInitial={() => setDraft((d) => ({ ...d, initial: selectedStep.status_key }))}
              onRemove={() => {
                update((d) => ({ ...d, steps: d.steps.filter((s) => s.status_key !== selectedStep.status_key) }));
                setSelected(null);
              }}
              onPreview={() => setPreviewOpen(true)}
            />
          ) : (
            <p className="py-10 text-center text-body text-muted-foreground">{t(($) => $.workflows.editor.select_step)}</p>
          )}
        </div>
      </div>

      <WorkflowMappingDialog
        open={mappingPlan !== null}
        onOpenChange={(open) => !open && setMappingPlan(null)}
        variant={{ kind: "edit", workflowName: draft.name.trim() }}
        plan={mappingPlan}
        targetKeys={stepKeys}
        pending={pending}
        onConfirm={(mapping) => void save(mapping)}
      />
      {selectedStep && (
        <BriefPreviewDialog
          open={previewOpen}
          onOpenChange={setPreviewOpen}
          draft={draft}
          statusKey={selectedStep.status_key}
        />
      )}
    </div>
  );
}

function Breadcrumb({ onBack, current }: { onBack: () => void; current: string }) {
  const { t } = useT("issues");
  const workspace = useCurrentWorkspace();
  return (
    <nav className="flex items-center gap-1.5 text-caption text-muted-foreground">
      {workspace?.name && (
        <>
          <span className="truncate">{workspace.name}</span>
          <ChevronRight aria-hidden className="size-3" />
        </>
      )}
      <button type="button" onClick={onBack} className="hover:text-foreground transition-colors">
        {t(($) => $.workflows.settings.title)}
      </button>
      {current && (
        <>
          <ChevronRight aria-hidden className="size-3" />
          <span className="truncate text-foreground">{current}</span>
        </>
      )}
    </nav>
  );
}

function StepCard({
  step,
  initial,
  selected,
  draggable,
  onSelect,
}: {
  step: IssueWorkflowStep;
  initial: boolean;
  selected: boolean;
  draggable: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const handlerLabel = useStepHandlerLabel(null);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: step.status_key,
    disabled: !draggable,
  });
  const label = labelOf(step.status_key);
  const handler = handlerLabel(step);
  const { type, id } = step.handler;

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        "flex items-start gap-2 rounded-lg border bg-card px-2.5 py-2.5 transition-colors",
        selected ? "border-foreground/20 bg-accent/60" : "border-surface-border hover:bg-accent/30",
        isDragging && "z-10 shadow-[var(--surface-shadow)]",
      )}
    >
      {draggable ? (
        <button
          type="button"
          aria-label={t(($) => $.workflows.editor.reorder, { name: label })}
          className="mt-0.5 flex size-5 shrink-0 touch-none items-center justify-center rounded cursor-grab text-muted-foreground active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-3.5" />
        </button>
      ) : (
        <span className="size-5 shrink-0" />
      )}
      <button type="button" onClick={onSelect} aria-pressed={selected} className="min-w-0 flex-1 text-left">
        <span className="flex items-center gap-2">
          <StatusIcon
            status={step.status_key}
            category={catalog.categoryOf(step.status_key)}
            color={catalog.colorOf(step.status_key)}
            icon={catalog.iconOf(step.status_key)}
            className="h-4 w-4 shrink-0"
          />
          <span className="truncate text-body font-medium">{label}</span>
          {initial && (
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
              {t(($) => $.workflows.editor.start)}
            </span>
          )}
        </span>
        <span className="mt-1 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          {handler ? (
            <>
              <CornerDownRight aria-hidden className="size-3 shrink-0" />
              <span className="shrink-0">{t(($) => $.workflows.editor.card_hand_to)}</span>
              {(type === "agent" || type === "squad" || type === "member") && id && (
                <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
              )}
              <span className="truncate">{handler}</span>
              {step.next_status_key && (
                <>
                  <span aria-hidden className="text-muted-foreground/50">·</span>
                  <span className="shrink-0">
                    {t(($) => $.workflows.editor.card_then, { status: labelOf(step.next_status_key) })}
                  </span>
                </>
              )}
            </>
          ) : (
            <>
              <User aria-hidden className="size-3 shrink-0" />
              <span>{t(($) => $.workflows.handler.none)}</span>
            </>
          )}
        </span>
      </button>
    </div>
  );
}

function StepConfig({
  step,
  steps,
  initial,
  addable,
  canEdit,
  onPatch,
  onSwap,
  onMakeInitial,
  onRemove,
  onPreview,
}: {
  step: IssueWorkflowStep;
  steps: IssueWorkflowStep[];
  initial: boolean;
  addable: string[];
  canEdit: boolean;
  onPatch: (patch: Partial<IssueWorkflowStep>) => void;
  onSwap: (to: string) => void;
  onMakeInitial: () => void;
  onRemove: () => void;
  onPreview: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const type = step.handler.type;
  const handsOff = type !== "none";

  const statusIcon = (key: string, className = "h-3.5 w-3.5") => (
    <StatusIcon
      status={key}
      category={catalog.categoryOf(key)}
      color={catalog.colorOf(key)}
      icon={catalog.iconOf(key)}
      className={cn("shrink-0", className)}
    />
  );
  const categoryLabel = (key: string) => t(($) => $.status_category[catalog.categoryOf(key)]);

  const targets =
    type === "agent"
      ? agents.filter((a) => !a.archived_at).map((a) => ({ id: a.id, name: a.name }))
      : type === "squad"
        ? squads.map((s) => ({ id: s.id, name: s.name }))
        : type === "member"
          ? members.map((m) => ({ id: m.user_id, name: m.name }))
          : [];
  const targetType = type === "agent" || type === "squad" || type === "member" ? type : null;
  const targetKind = targetType ? t(($) => $.workflows.editor[`target_${targetType}`]) : "";

  const refOptions = [
    { value: NOT_SET, label: t(($) => $.workflows.editor.none_option) },
    ...steps.filter((s) => s.status_key !== step.status_key).map((s) => ({ value: s.status_key, label: labelOf(s.status_key) })),
  ];

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-2">
        {statusIcon(step.status_key, "h-[18px] w-[18px]")}
        <span className="truncate text-title-sm font-semibold">{labelOf(step.status_key)}</span>
        <span className="truncate font-mono text-caption text-muted-foreground">{step.status_key}</span>
        <span className="flex-1" />
        {canEdit && !initial && (
          <Button variant="ghost" size="xs" className="gap-1" onClick={onMakeInitial}>
            <Flag className="size-3.5" />
            {t(($) => $.workflows.editor.set_start)}
          </Button>
        )}
        {canEdit && (
          <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.workflows.editor.remove)} onClick={onRemove}>
            <Trash2 className="size-3.5" />
          </Button>
        )}
      </div>

      <Field label={t(($) => $.workflows.editor.status)} hint={t(($) => $.workflows.editor.status_hint)}>
        <Select
          items={[step.status_key, ...addable].map((key) => ({ value: key, label: labelOf(key) }))}
          value={step.status_key}
          disabled={!canEdit}
          onValueChange={(value) => value && value !== step.status_key && onSwap(value)}
        >
          <SelectTrigger aria-label={t(($) => $.workflows.editor.status)} className="w-full">
            <span className="flex min-w-0 items-center gap-2">
              {statusIcon(step.status_key)}
              <span className="truncate">{labelOf(step.status_key)}</span>
              <span className="truncate text-caption text-muted-foreground">{categoryLabel(step.status_key)}</span>
            </span>
          </SelectTrigger>
          <SelectContent>
            {[step.status_key, ...addable].map((key) => (
              <SelectItem key={key} value={key}>
                <span className="flex items-center gap-2">
                  {statusIcon(key)}
                  <span>{labelOf(key)}</span>
                  <span className="text-caption text-muted-foreground">{categoryLabel(key)}</span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field
        label={t(($) => $.workflows.editor.on_enter)}
        hint={
          type === "project_lead"
            ? t(($) => $.workflows.editor.handler_hint_lead) + " " + t(($) => $.workflows.editor.handler_hint_on)
            : type === "creator"
              ? t(($) => $.workflows.editor.handler_hint_creator) + " " + t(($) => $.workflows.editor.handler_hint_on)
              : handsOff
                ? t(($) => $.workflows.editor.handler_hint_on)
                : t(($) => $.workflows.editor.handler_hint_off)
        }
      >
        <div
          role="radiogroup"
          aria-label={t(($) => $.workflows.editor.on_enter)}
          className="inline-flex max-w-full flex-wrap gap-0.5 rounded-lg bg-muted p-0.5"
        >
          {HANDLER_TYPES.map((option) => (
            <button
              key={option}
              type="button"
              role="radio"
              aria-checked={type === option}
              disabled={!canEdit}
              onClick={() => onPatch({ handler: { type: option } })}
              className={cn(
                "rounded-md px-2.5 py-1 text-caption transition-colors",
                type === option
                  ? "bg-background font-medium text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t(($) => $.workflows.handler[option])}
            </button>
          ))}
        </div>
        {targetType && (
          <Select
            items={targets.map((target) => ({ value: target.id, label: target.name }))}
            value={step.handler.id ?? ""}
            disabled={!canEdit}
            onValueChange={(value) => value && onPatch({ handler: { type: targetType, id: value } })}
          >
            <SelectTrigger aria-label={targetKind} className="mt-2 w-full">
              {step.handler.id ? (
                <span className="flex min-w-0 items-center gap-2">
                  <ActorAvatar actorType={targetType} actorId={step.handler.id} size="xs" profileLink={false} />
                  <span className="truncate">{targets.find((x) => x.id === step.handler.id)?.name ?? ""}</span>
                  <span className="truncate text-caption text-muted-foreground">{targetKind}</span>
                </span>
              ) : (
                <SelectValue placeholder={t(($) => $.workflows.editor.choose_target)} />
              )}
            </SelectTrigger>
            <SelectContent>
              {targets.map((target) => (
                <SelectItem key={target.id} value={target.id}>
                  <span className="flex items-center gap-2">
                    <ActorAvatar actorType={targetType} actorId={target.id} size="xs" profileLink={false} />
                    <span>{target.name}</span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </Field>

      {handsOff && (
        <Field label={t(($) => $.workflows.editor.instructions)} hint={t(($) => $.workflows.editor.instructions_hint)} htmlFor="workflow-step-instructions">
          <Textarea
            id="workflow-step-instructions"
            rows={5}
            maxLength={8000}
            readOnly={!canEdit}
            value={step.instructions}
            placeholder={t(($) => $.workflows.editor.instructions_placeholder)}
            onChange={(e) => onPatch({ instructions: e.target.value })}
          />
        </Field>
      )}

      <div className="space-y-2">
        <div className="grid gap-3 sm:grid-cols-2">
          {(["next", "back"] as const).map((kind) => {
            const field = kind === "next" ? "next_status_key" : "back_status_key";
            const value = step[field];
            return (
              <div key={kind} className="min-w-0 space-y-1.5">
                <span className="block text-caption font-medium">{t(($) => $.workflows.editor[kind])}</span>
                <Select
                  items={refOptions}
                  value={value ?? NOT_SET}
                  disabled={!canEdit}
                  onValueChange={(next) => onPatch({ [field]: !next || next === NOT_SET ? undefined : next })}
                >
                  <SelectTrigger aria-label={t(($) => $.workflows.editor[kind])} className="w-full">
                    {value ? (
                      <span className="flex min-w-0 items-center gap-2">
                        {statusIcon(value)}
                        <span className="truncate">{labelOf(value)}</span>
                      </span>
                    ) : (
                      <span className="text-muted-foreground">{t(($) => $.workflows.editor.none_option)}</span>
                    )}
                  </SelectTrigger>
                  <SelectContent>
                    {refOptions.map((option) => (
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
          {t(($) => $.workflows.editor.next_hint)}{" "}
          {handsOff && (
            <button type="button" onClick={onPreview} className="text-brand hover:underline">
              {t(($) => $.workflows.editor.preview_brief)}
            </button>
          )}
        </p>
      </div>
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

/** The brief a step's handler receives, rendered by the server from the unsaved draft. */
function BriefPreviewDialog({
  open,
  onOpenChange,
  draft,
  statusKey,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  draft: Draft;
  statusKey: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const { data: brief, isPending } = useQuery({
    queryKey: ["issue-workflows", wsId, "brief-preview", statusKey, JSON.stringify(draft)],
    queryFn: () =>
      api.previewIssueWorkflowBrief({
        name: draft.name.trim() || "Workflow",
        initial_status_key: draft.initial,
        steps: draft.steps,
        status_key: statusKey,
      }),
    enabled: open,
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.workflows.editor.preview_title, { status: labelOf(statusKey) })}</DialogTitle>
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
