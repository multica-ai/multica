"use client";

import { useMemo, useState } from "react";
import { CircleEqual, CornerDownRight } from "lucide-react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { stepHandsOff, workflowStep } from "@multica/core/issue-workflows";
import type { IssueWorkflowStep, Project } from "@multica/core/types";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { useT } from "../../../i18n";
import { useStatusLabel } from "../../utils/status-label";
import { useIssueStatusOptions } from "../../utils/status-options";
import { stepHandlerActor, useStepHandlerLabel } from "../../../workflows/step-handler";
import { WorkflowHandoffConfirmDialog } from "../../../workflows/handoff-confirm-dialog";
import { ActorAvatar } from "../../../common/actor-avatar";

/** Above this many options the flat list stops being scannable. */
const SEARCH_THRESHOLD = 9;

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  onMarkDuplicate,
  isDuplicate,
  projectId,
  issue,
}: {
  /**
   * The currently-selected status, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * statuses) — no row is checked. Single-issue callers always pass a concrete
   * status.
   */
  status: IssueStatus | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /**
   * Adds the "Mark as duplicate" action. It is an action, not a status: it
   * opens a picker for the original and only writes once one is chosen. Pass
   * it only for an existing single issue — never on create or batch surfaces.
   */
  onMarkDuplicate?: () => void;
  /** The issue already carries a mark, so the action re-points it. */
  isDuplicate?: boolean;
  /**
   * The issue's project. When it uses a workflow, only that workflow's steps
   * are offered, in workflow order, each naming who entering it hands the
   * issue to. Omit when the surface spans projects. (MUL-7420)
   */
  projectId?: string | null;
  /**
   * The single issue being edited. With a workflow, choosing a step that
   * hands the issue off first shows the handoff confirmation for it; the
   * write carries the choice to stop the previous agent's run. (MUL-7420)
   */
  issue?: { id: string; identifier: string };
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [query, setQuery] = useState("");
  const { t } = useT("issues");
  // Every StatusPicker call site lives inside the workspace shell (issue
  // detail, table, board batch toolbar, create-issue modal), so the provider
  // is guaranteed here.
  const wsId = useWorkspaceId();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  // The project workflow's steps, or the catalog in canonical category order
  // without archived statuses (MUL-6243).
  const { options: allOptions, workflow, project } = useIssueStatusOptions(wsId, projectId);

  const options = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return allOptions;
    return allOptions.filter((o) => o.label.toLowerCase().includes(q));
  }, [allOptions, query]);

  const searchable = !!workflow || allOptions.length > SEARCH_THRESHOLD;
  const [pendingStatus, setPendingStatus] = useState<IssueStatus | null>(null);

  const markDuplicate = onMarkDuplicate ? (
    // Rendered outside the arrow-key listbox so keyboard nav and search
    // never treat the action as another status option.
    <button
      type="button"
      onClick={() => {
        setOpen(false);
        setQuery("");
        onMarkDuplicate();
      }}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-body hover:bg-accent transition-colors"
    >
      <CircleEqual className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span>
        {t(($) =>
          isDuplicate ? $.pickers.status.change_original : $.pickers.status.mark_duplicate,
        )}
      </span>
    </button>
  ) : null;

  return (
    <>
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        if (!v) setQuery("");
        setOpen(v);
      }}
      // Handoff hints need room beside the status name. (MUL-7420)
      width={workflow ? "w-72" : "w-52"}
      align={align}
      triggerRender={triggerRender}
      searchable={searchable}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setQuery}
      header={
        workflow ? (
          <div className="truncate px-2 pb-1 pt-1.5 text-caption text-muted-foreground">
            {project
              ? t(($) => $.workflows.picker.header, { workflow: workflow.name, project: project.title })
              : workflow.name}
          </div>
        ) : undefined
      }
      footer={
        workflow ? (
          <>
            {markDuplicate}
            <p className="px-2 pb-1 pt-1.5 text-caption leading-5 text-muted-foreground">
              {t(($) => $.workflows.picker.footer)}
            </p>
          </>
        ) : (
          markDuplicate ?? undefined
        )
      }
      trigger={
        customTrigger ??
        (status != null ? (
          <>
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              color={colorOf(status)}
              icon={iconOf(status)}
              className="h-3.5 w-3.5 shrink-0"
            />
            <span className="truncate">{labelOf(status)}</span>
          </>
        ) : null)
      }
    >
      {options.map((option) => (
        <PickerItem
          key={option.key}
          selected={option.key === status}
          hoverClassName={STATUS_CONFIG[option.category].hoverBg}
          onClick={() => {
            setOpen(false);
            setQuery("");
            if (workflow && issue && option.key !== status && stepHandsOff(workflowStep(workflow, option.key))) {
              setPendingStatus(option.key);
              return;
            }
            onUpdate({ status: option.key });
          }}
        >
          <StatusIcon
            status={option.key}
            category={option.category}
            color={option.color}
            icon={option.icon}
            className="h-3.5 w-3.5"
          />
          <span className="min-w-12 truncate">{option.label}</span>
          {workflow && option.key !== status && (
            <StepHandoffHint step={workflowStep(workflow, option.key)} project={project} />
          )}
        </PickerItem>
      ))}
    </PropertyPicker>
    {workflow && issue && (
      <WorkflowHandoffConfirmDialog
        issue={pendingStatus ? issue : null}
        toStatus={pendingStatus ?? ""}
        onCancel={() => setPendingStatus(null)}
        onConfirm={({ stopPreviousRuns }) => {
          const next = pendingStatus;
          setPendingStatus(null);
          if (next) onUpdate({ status: next, ...(stopPreviousRuns ? { stop_previous_assignee_runs: true } : {}) });
        }}
      />
    )}
    </>
  );
}

/**
 * Who entering a workflow step hands the issue to, or that the assignee
 * stays — the right-hand column of a workflow status picker. (MUL-7420)
 */
function StepHandoffHint({ step, project }: { step: IssueWorkflowStep | undefined; project: Project | null }) {
  const { t } = useT("issues");
  const handlerLabel = useStepHandlerLabel(project);
  const { getActorName } = useActorName();
  const handler = handlerLabel(step);
  if (!handler) {
    return (
      <span className="ml-auto shrink-0 pl-2 text-caption text-muted-foreground/70">
        {t(($) => $.workflows.handler.none)}
      </span>
    );
  }
  const actor = stepHandlerActor(step, project);
  const name = actor ? getActorName(actor.type, actor.id) : handler;
  return (
    <span className="ml-auto flex min-w-0 max-w-[55%] items-center gap-1 pl-2 text-caption text-muted-foreground">
      <span className="sr-only">{t(($) => $.workflows.hands_off_to, { name: handler })}</span>
      <CornerDownRight aria-hidden className="size-3 shrink-0" />
      {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" profileLink={false} />}
      <span aria-hidden className="truncate">{name}</span>
    </span>
  );
}
