"use client";

import { useState } from "react";
import { Plus, Workflow } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { statusAcceptsHandoff, workflowStep } from "@multica/core/issue-workflows";
import { useActorName } from "@multica/core/workspace/hooks";
import type { IssueWorkflow, Project } from "@multica/core/types";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "../common/actor-avatar";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";
import { useWorkflowSharing, WorkflowHandoffForm } from "./handoff-form";
import { useWorkflowPermissions } from "./permissions";
import { stepHandlerActor, useStepHandlerLabel } from "./step-handler";

/**
 * A project board column's handoff (MUL-7420): the handler badge, or a
 * "Hand to…" affordance for admins. Either opens the column's handoff form;
 * it edits the workflow, so it says how many projects the change reaches.
 */
export function ColumnHandoff({
  status,
  workflow,
  project,
}: {
  status: string;
  workflow: IssueWorkflow;
  project: Project;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const handlerLabel = useStepHandlerLabel(project);
  const { getActorName } = useActorName();
  const { isAdmin } = useWorkflowPermissions(project);
  const [open, setOpen] = useState(false);
  const sharing = useWorkflowSharing(workflow, project.id);
  const step = workflowStep(workflow, status);
  if (!step || !statusAcceptsHandoff(status)) return null;
  const label = handlerLabel(step);
  if (!label && !isAdmin) return null;
  const actor = stepHandlerActor(step, project);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          label ? (
            <button
              type="button"
              className="inline-flex min-w-0 max-w-32 items-center gap-1 rounded-full border border-surface-border bg-background/70 py-0.5 pl-0.5 pr-2 text-caption transition-colors hover:bg-accent"
              aria-label={t(($) => $.workflows.hands_off_to, { name: label })}
              title={t(($) => $.workflows.hands_off_to, { name: label })}
            >
              {actor && <ActorAvatar actorType={actor.type} actorId={actor.id} size="xs" profileLink={false} />}
              <span className="truncate">{actor ? getActorName(actor.type, actor.id) : label}</span>
            </button>
          ) : (
            <button
              type="button"
              className="inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.5 text-caption text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover/column:opacity-100 data-popup-open:opacity-100"
            >
              <Plus aria-hidden className="size-3" />
              {t(($) => $.workflows.handoff.add)}
            </button>
          )
        }
      />
      <PopoverContent align="end" className="w-[26rem] max-w-[calc(100vw-2rem)] p-4">
        <div className="mb-3 space-y-0.5">
          <p className="text-body font-medium">{t(($) => $.workflows.handoff.title, { status: labelOf(status) })}</p>
          <p className="flex items-start gap-1.5 text-caption text-muted-foreground">
            <Workflow aria-hidden className="mt-0.5 size-3 shrink-0" />
            <span>
              {sharing.others.length > 0
                ? t(($) => $.workflows.handoff.scope_line_shared, { workflow: workflow.name, projects: sharing.label })
                : t(($) => $.workflows.handoff.scope_line, { workflow: workflow.name })}
            </span>
          </p>
        </div>
        <WorkflowHandoffForm
          workflow={workflow}
          statusKey={status}
          canEdit={isAdmin}
          project={project}
          onSaved={() => setOpen(false)}
        />
      </PopoverContent>
    </Popover>
  );
}
