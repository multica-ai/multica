"use client";

import { useState } from "react";
import { Settings, Workflow } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useProjectWithWorkflow } from "@multica/core/issue-workflows";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useWorkflowPermissions } from "./permissions";
import { stepHandlerActor, useStepHandlerLabel } from "./step-handler";

/**
 * The workflow an issue follows through its project (MUL-7420), opening a
 * read-only card of its columns and who each hands to. It changes on the
 * project, so nothing here edits it.
 */
export function IssueFlowCard({ projectId, currentStatus }: { projectId: string | null; currentStatus: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const { project, workflow } = useProjectWithWorkflow(wsId, projectId);
  const { canCreateWorkflow } = useWorkflowPermissions(project);
  const handlerLabel = useStepHandlerLabel(project);
  const [open, setOpen] = useState(false);
  if (!workflow || !project) return null;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type="button"
            className="-ml-1 inline-flex min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-caption transition-colors hover:bg-accent data-popup-open:bg-accent"
          >
            <Workflow aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate text-foreground">{workflow.name}</span>
            <span className="shrink-0 text-muted-foreground">{t(($) => $.workflows.detail.follows_project)}</span>
          </button>
        }
      />
      <PopoverContent align="start" className="w-72 p-3">
        <p className="mb-2 text-caption text-muted-foreground">
          {t(($) => $.workflows.detail.card_title, { workflow: workflow.name, project: project.title })}
        </p>
        <ol className="space-y-0.5">
          {workflow.steps.map((step) => {
            const label = handlerLabel(step);
            const actor = stepHandlerActor(step, project);
            const current = step.status_key === currentStatus;
            return (
              <li
                key={step.status_key}
                className={cn("flex items-center gap-2 rounded-md px-2 py-1.5", current && "bg-accent")}
                aria-current={current ? "step" : undefined}
              >
                <StatusIcon
                  status={step.status_key}
                  category={catalog.categoryOf(step.status_key)}
                  color={catalog.colorOf(step.status_key)}
                  icon={catalog.iconOf(step.status_key)}
                  className="h-3.5 w-3.5 shrink-0"
                />
                <span className={cn("min-w-0 flex-1 truncate text-body", current && "font-medium")}>
                  {labelOf(step.status_key)}
                </span>
                {label && (
                  <span className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
                    {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" profileLink={false} />}
                    <span className="truncate">{label}</span>
                  </span>
                )}
              </li>
            );
          })}
        </ol>
        {canCreateWorkflow && (
          <div className="-mx-3 -mb-3 mt-2 border-t border-surface-border p-1">
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start"
              onClick={() => {
                setOpen(false);
                navigation.push(`${paths.settings()}?tab=workflows`);
              }}
            >
              <Settings aria-hidden className="size-3.5" />
              {t(($) => $.workflows.project.manage)}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
