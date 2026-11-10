"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Settings, Workflow } from "@multica/ui/icons";
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
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
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
 * The project title menu's one Workflow item (MUL-7420, MUL-8941): it opens
 * the project's flow panel, where everything else about the workflow lives.
 */
export function ProjectWorkflowMenuItem({ project, onOpen }: { project: Project; onOpen: () => void }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { workflows } = useIssueWorkflows(wsId);
  const workflow = resolveProjectWorkflow(workflows, project);
  if (!workflow) return null;

  return (
    <>
      <DropdownMenuSeparator />
      <DropdownMenuItem onClick={onOpen}>
        <Workflow className="size-3.5" />
        <span className="flex-1">{t(($) => $.workflows.project.label)}</span>
        <span className="ml-3 max-w-32 truncate text-caption font-medium text-muted-foreground">{workflow.name}</span>
      </DropdownMenuItem>
    </>
  );
}

/**
 * The project's whole flow in one panel (MUL-7420): every column, who it
 * hands to, and the same handoff form the column header opens. Switching
 * workflows and the way to settings live in its header, so the project page
 * offers each exactly once (MUL-8941). Handoffs belong to the workflow, so
 * only its owner and admins edit them here.
 */
export function ProjectWorkflowSheet({
  open,
  onOpenChange,
  project,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project: Project;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const workflow = resolveProjectWorkflow(workflows, project);
  const { canCreateWorkflow, canEditWorkflow, canManageProject } = useWorkflowPermissions(project, workflow);
  const { choose, busy, dialog } = useProjectWorkflowSwitch(project);
  const [expanded, setExpanded] = useState<string | null>(null);
  if (!workflow) return null;

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
        {(canManageProject || canCreateWorkflow) && (
          <div className="flex flex-wrap items-center gap-2">
            {canManageProject && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  disabled={busy}
                  render={
                    <Button variant="outline" size="xs">
                      <Workflow aria-hidden />
                      {t(($) => $.workflows.flow.switch)}
                      <ChevronDown aria-hidden className="size-3 text-muted-foreground" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="start" className="w-80">
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>{t(($) => $.workflows.project.picker_title)}</DropdownMenuLabel>
                    <WorkflowChoiceItems project={project} onChoose={(w) => void choose(w)} />
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {canCreateWorkflow && (
              <Button
                variant="ghost"
                size="xs"
                className="text-muted-foreground"
                onClick={() => {
                  onOpenChange(false);
                  navigation.push(`${paths.settings()}?tab=workflows`);
                }}
              >
                <Settings aria-hidden />
                {t(($) => $.workflows.project.manage)}
              </Button>
            )}
          </div>
        )}
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
        {dialog}
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
