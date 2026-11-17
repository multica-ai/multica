"use client";

import { Archive, Check, User } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Project, ProjectPriority, ProjectStatus, UpdateProjectRequest } from "@multica/core/types";
import { PROJECT_PRIORITY_ORDER, PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import { ActorAvatar } from "../../common/actor-avatar";
import { PillButton } from "../../common/pill-button";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectWorkflowControl } from "../../workflows/project-workflow-row";
import { useT } from "../../i18n";
import { useActorName } from "@multica/core/workspace/hooks";
import { useFormatRelativeDate, useProjectPriorityLabels, useProjectStatusLabels } from "./labels";
import { ProjectAccessControl } from "./project-access";
import { ProjectDueDatePicker } from "./project-due-date-picker";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProjectStartDatePicker } from "./project-start-date-picker";

/**
 * The project's properties as one row of editable pills under the title —
 * the same pills the create-project modal uses, so a project reads the same
 * while it is drafted and after.
 */
export function ProjectPropertyPills({
  project,
  onUpdate,
  onStatusChange,
  onOpenWorkflow,
}: {
  project: Project;
  onUpdate: (data: UpdateProjectRequest) => void;
  onStatusChange: (status: ProjectStatus) => void;
  onOpenWorkflow: () => void;
}) {
  const { t } = useT("projects");
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();

  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="project-properties">
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <PillButton>
              <span className={cn("size-2 shrink-0 rounded-full", PROJECT_STATUS_CONFIG[project.status].dotColor)} />
              <span className="truncate">{statusLabels[project.status]}</span>
            </PillButton>
          }
        />
        <DropdownMenuContent align="start" className="w-44">
          {PROJECT_STATUS_ORDER.map((s) => (
            <DropdownMenuItem key={s} onClick={() => onStatusChange(s as ProjectStatus)}>
              <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
              <span>{statusLabels[s]}</span>
              {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <PillButton>
              <PriorityIcon priority={project.priority} />
              <span className="truncate">{priorityLabels[project.priority]}</span>
            </PillButton>
          }
        />
        <DropdownMenuContent align="start" className="w-44">
          {PROJECT_PRIORITY_ORDER.map((p) => (
            <DropdownMenuItem key={p} onClick={() => onUpdate({ priority: p as ProjectPriority })}>
              <PriorityIcon priority={p} />
              <span>{priorityLabels[p]}</span>
              {p === project.priority && <Check className="ml-auto h-3.5 w-3.5" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      <ProjectLeadPicker
        project={project}
        handleUpdate={onUpdate}
        renderTrigger={(leadName) => (
          <PillButton>
            {project.lead_type && project.lead_id ? (
              <>
                <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" showStatusDot />
                <span className="truncate">{leadName}</span>
              </>
            ) : (
              <>
                <User className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
              </>
            )}
          </PillButton>
        )}
      />

      <ProjectAccessControl project={project} variant="pill" />

      <ProjectStartDatePicker startDate={project.start_date} onUpdate={onUpdate} triggerRender={<PillButton />} />
      <ProjectDueDatePicker dueDate={project.due_date} onUpdate={onUpdate} triggerRender={<PillButton />} />

      <ProjectWorkflowControl project={project} onOpen={onOpenWorkflow} variant="pill" />

      {project.archived_at && <ArchivedPill project={project} />}
    </div>
  );
}

function ArchivedPill({ project }: { project: Project }) {
  const { t } = useT("projects");
  const { getActorName } = useActorName();
  const formatRelativeDate = useFormatRelativeDate();
  const by = project.archived_by_type && project.archived_by_id
    ? { type: project.archived_by_type, id: project.archived_by_id }
    : null;
  return (
    <span
      className="inline-flex h-[var(--button-height-xs)] min-w-0 max-w-56 items-center gap-1.5 overflow-hidden rounded-md border border-dashed px-2 text-caption text-muted-foreground [--avatar-slot-size:1rem]"
      title={by ? `${t(($) => $.archive.prop_label)} · ${getActorName(by.type, by.id)}` : t(($) => $.archive.prop_label)}
    >
      <Archive className="size-3.5 shrink-0" />
      <span className="shrink-0 tabular-nums">{formatRelativeDate(project.archived_at!)}</span>
      {by && <ActorAvatar actorType={by.type} actorId={by.id} size="sm" />}
    </span>
  );
}
