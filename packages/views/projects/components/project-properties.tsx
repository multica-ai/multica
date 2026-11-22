"use client";

import { Check } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Project, ProjectPriority, ProjectStatus, UpdateProjectRequest } from "@multica/core/types";
import { PROJECT_PRIORITY_ORDER, PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectWorkflowControl } from "../../workflows/project-workflow-row";
import { ObjectPanelSection, ObjectProperty } from "../../layout/object-page";
import { useT } from "../../i18n";
import { useFormatRelativeDate, useProjectPriorityLabels, useProjectStatusLabels } from "./labels";
import { ProjectAccessControl } from "./project-access";
import { ProjectDueDatePicker } from "./project-due-date-picker";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProjectStartDatePicker } from "./project-start-date-picker";

const PROPERTY_TRIGGER =
  "flex min-w-0 items-center gap-1.5 rounded-sm text-caption outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50";

/** The project's editable properties, as the panel section every object page opens with. */
export function ProjectPropertiesPanel({
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
  const { t: tIssues } = useT("issues");
  const { getActorName } = useActorName();
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const formatRelativeDate = useFormatRelativeDate();

  return (
    <div data-testid="project-properties">
      <ObjectPanelSection title={t(($) => $.detail.section_properties)}>
        <ObjectProperty label={t(($) => $.table.status)}>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button type="button" className={PROPERTY_TRIGGER}>
                  <span className={cn("size-2 shrink-0 rounded-full", PROJECT_STATUS_CONFIG[project.status].dotColor)} />
                  <span className="truncate">{statusLabels[project.status]}</span>
                </button>
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
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.priority)}>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button type="button" className={PROPERTY_TRIGGER}>
                  <PriorityIcon priority={project.priority} />
                  <span className="truncate">{priorityLabels[project.priority]}</span>
                </button>
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
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.lead)}>
          <ProjectLeadPicker
            project={project}
            handleUpdate={onUpdate}
            renderTrigger={(leadName) => (
              <button type="button" className={PROPERTY_TRIGGER}>
                {project.lead_type && project.lead_id ? (
                  <>
                    <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" enableHoverCard showStatusDot />
                    <span className="truncate">{leadName}</span>
                  </>
                ) : (
                  <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
                )}
              </button>
            )}
          />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.access.label)}>
          <ProjectAccessControl project={project} />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.prop_start_date)}>
          <ProjectStartDatePicker startDate={project.start_date} onUpdate={onUpdate} />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.prop_due_date)}>
          <ProjectDueDatePicker dueDate={project.due_date} onUpdate={onUpdate} />
        </ObjectProperty>
        <ObjectProperty label={tIssues(($) => $.workflows.project.label)}>
          <ProjectWorkflowControl project={project} onOpen={onOpenWorkflow} />
        </ObjectProperty>
        {project.archived_at && (
          <ObjectProperty label={t(($) => $.archive.prop_label)}>
            <span className="tabular-nums">{formatRelativeDate(project.archived_at)}</span>
            {project.archived_by_type && project.archived_by_id && (
              <>
                <span className="text-muted-foreground">·</span>
                <ActorAvatar actorType={project.archived_by_type} actorId={project.archived_by_id} size="sm" enableHoverCard />
                <span className="truncate">{getActorName(project.archived_by_type, project.archived_by_id)}</span>
              </>
            )}
          </ObjectProperty>
        )}
      </ObjectPanelSection>
    </div>
  );
}
