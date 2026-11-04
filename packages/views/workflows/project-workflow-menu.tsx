"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, ListTree, Settings, Workflow } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  resolveProjectWorkflow,
  statusAcceptsHandoff,
  useIssueWorkflows,
} from "@multica/core/issue-workflows";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { IssueWorkflow, Project } from "@multica/core/types";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { WorkflowHandoffForm } from "./handoff-form";
import { useWorkflowPermissions } from "./permissions";
import { useProjectWorkflowSwitch, WorkflowChoiceItems } from "./project-workflow-row";
import { stepHandlerActor, useStepHandlerLabel } from "./step-handler";

/**
 * The project's "Workflow" submenu (MUL-7420), shown in the project title's
 * menu: open the full flow or switch workflows; workflows themselves are made
 * in settings.
 *
 * The flow sheet and the switch dialog must outlive the menu that opens them,
 * so this host owns both and hands the submenu to `children` to place.
 */
export function ProjectWorkflowMenuHost({
  project,
  children,
}: {
  project: Project;
  children: (submenu: ReactNode) => ReactNode;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { workflows } = useIssueWorkflows(wsId);
  const { canManageProject, canCreateWorkflow } = useWorkflowPermissions(project);
  const { choose, busy, dialog } = useProjectWorkflowSwitch(project);
  const [flowOpen, setFlowOpen] = useState(false);
  const workflow = resolveProjectWorkflow(workflows, project);

  const submenu = workflow ? (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger disabled={busy}>
        <Workflow className="size-3.5" />
        <span className="flex-1">{t(($) => $.workflows.project.label)}</span>
        <span className="ml-3 max-w-32 truncate text-caption font-medium text-muted-foreground">
          {workflow.name}
        </span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-64">
        <DropdownMenuItem onClick={() => setFlowOpen(true)}>
          <ListTree className="size-3.5" />
          {t(($) => $.workflows.menu.view_flow)}
        </DropdownMenuItem>
        {canManageProject && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Workflow className="size-3.5" />
              {t(($) => $.workflows.menu.switch)}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-80">
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.workflows.project.picker_title)}</DropdownMenuLabel>
                <WorkflowChoiceItems project={project} onChoose={(w) => void choose(w)} />
              </DropdownMenuGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
        {canCreateWorkflow && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=workflows`)}>
              <Settings className="size-3.5" />
              {t(($) => $.workflows.project.manage)}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  ) : null;

  return (
    <>
      {children(submenu)}
      {dialog}
      {workflow && (
        <WorkflowFlowSheet open={flowOpen} onOpenChange={setFlowOpen} project={project} workflow={workflow} />
      )}
    </>
  );
}

/**
 * The project's whole flow in one panel (MUL-7420): every column, who it
 * hands to, and the same handoff form the column header opens. Handoffs
 * belong to the workflow, so only its owner and admins edit them here.
 */
export function WorkflowFlowSheet({
  open,
  onOpenChange,
  project,
  workflow,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project: Project;
  workflow: IssueWorkflow;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { canEditWorkflow, canManageProject } = useWorkflowPermissions(project, workflow);
  const [expanded, setExpanded] = useState<string | null>(null);
  const usedBy = workflow.is_default
    ? projects.filter((p) => !p.workflow_id).length
    : workflow.project_ids.length;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[440px] overflow-y-auto p-5 sm:max-w-[440px]">
        <SheetHeader className="p-0">
          <SheetTitle>{t(($) => $.workflows.flow.title, { workflow: workflow.name })}</SheetTitle>
          <SheetDescription>
            {usedBy > 1
              ? t(($) => $.workflows.flow.scope_shared, { count: usedBy })
              : t(($) => $.workflows.flow.scope_own)}
          </SheetDescription>
        </SheetHeader>
        <ol className="space-y-1.5">
          {workflow.steps.map((step) => (
            <FlowRow
              key={step.status_key}
              project={project}
              workflow={workflow}
              statusKey={step.status_key}
              canEdit={canEditWorkflow}
              canCopyForProject={canManageProject}
              expanded={expanded === step.status_key}
              onToggle={() => setExpanded((current) => (current === step.status_key ? null : step.status_key))}
            />
          ))}
        </ol>
      </SheetContent>
    </Sheet>
  );
}

function FlowRow({
  project,
  workflow,
  statusKey,
  canEdit,
  canCopyForProject,
  expanded,
  onToggle,
}: {
  project: Project;
  workflow: IssueWorkflow;
  statusKey: string;
  canEdit: boolean;
  canCopyForProject: boolean;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const handlerLabel = useStepHandlerLabel(project);
  const step = workflow.steps.find((s) => s.status_key === statusKey);
  const label = handlerLabel(step);
  const actor = stepHandlerActor(step, project);
  const accepts = statusAcceptsHandoff(statusKey);

  return (
    <li className={cn("rounded-lg border border-surface-border", expanded && "bg-card")}>
      <button
        type="button"
        disabled={!accepts}
        onClick={onToggle}
        aria-expanded={expanded}
        className="flex w-full items-center gap-2 px-3 py-2 text-left disabled:cursor-default"
      >
        <StatusIcon
          status={statusKey}
          category={catalog.categoryOf(statusKey)}
          color={catalog.colorOf(statusKey)}
          icon={catalog.iconOf(statusKey)}
          className="h-3.5 w-3.5 shrink-0"
        />
        <span className="min-w-0 flex-1 truncate text-body font-medium">{labelOf(statusKey)}</span>
        {label ? (
          <span className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
            {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" profileLink={false} />}
            <span className="truncate">{label}</span>
          </span>
        ) : (
          <span className="text-caption text-muted-foreground">
            {accepts ? t(($) => $.workflows.handler.none) : t(($) => $.workflows.flow.no_handoff_here)}
          </span>
        )}
        {accepts && (
          <ChevronRight
            aria-hidden
            className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-90")}
          />
        )}
      </button>
      {expanded && accepts && (
        <div className="border-t border-surface-border px-3 py-3">
          <WorkflowHandoffForm
            workflow={workflow}
            statusKey={statusKey}
            canEdit={canEdit}
            canCopyForProject={canCopyForProject}
            project={project}
          />
        </div>
      )}
    </li>
  );
}
