"use client";

import { useState, useCallback } from "react";
import { Plus, FolderKanban, UserMinus, Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
import { useUpdateProject } from "@multica/core/projects/mutations";
import {
  PROJECT_STATUS_CONFIG,
  PROJECT_STATUS_ORDER,
  PROJECT_PRIORITY_CONFIG,
  PROJECT_PRIORITY_ORDER,
} from "@multica/core/projects/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useModalStore } from "@multica/core/modals";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { Project, ProjectStatus, ProjectPriority, UpdateProjectRequest } from "@multica/core/types";
import { PageHeader } from "../../layout/page-header";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectIcon } from "./project-icon";
import { useT } from "../../i18n";
import {
  useProjectStatusLabels,
  useProjectPriorityLabels,
  useFormatRelativeDate,
} from "./labels";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

function ProjectRow({ project }: { project: Project }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const formatRelativeDate = useFormatRelativeDate();
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];
  const updateProject = useUpdateProject();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();

  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery) || matchesPinyin(m.name, leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && (a.name.toLowerCase().includes(leadQuery) || matchesPinyin(a.name, leadQuery)));

  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => {
      updateProject.mutate({ id: project.id, ...data });
    },
    [project.id, updateProject],
  );

  return (
    <div className="group/row flex flex-col gap-2 border-b px-4 py-3 text-sm transition-colors hover:bg-accent/40 sm:h-11 sm:flex-row sm:items-center sm:gap-2 sm:border-b-0 sm:px-5 sm:py-0">
      {/* Icon + Name (navigates to detail) */}
      <AppLink
        href={wsPaths.projectDetail(project.id)}
        className="flex min-w-0 items-center gap-2 sm:flex-1"
      >
        <ProjectIcon project={project} size="md" />
        <span className="min-w-0 flex-1 truncate font-medium">{project.title}</span>
      </AppLink>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 pl-6 text-xs sm:contents sm:pl-0">
        {/* Priority — dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button type="button" className="flex shrink-0 items-center gap-1 rounded px-1 py-0.5 cursor-pointer transition-colors hover:bg-accent/60 sm:w-24 sm:justify-center">
                <PriorityIcon priority={project.priority} />
                <span className={cn("text-xs", priorityCfg.color)}>{priorityLabels[project.priority]}</span>
              </button>
            }
          />
          <DropdownMenuContent align="start" className="w-44">
            {PROJECT_PRIORITY_ORDER.map((p) => (
              <DropdownMenuItem key={p} onClick={() => handleUpdate({ priority: p as ProjectPriority })}>
                <PriorityIcon priority={p} />
                <span>{priorityLabels[p]}</span>
                {p === project.priority && <Check className="ml-auto h-3.5 w-3.5" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Status — dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button type="button" className={cn(
                "inline-flex shrink-0 items-center gap-1 rounded px-2 py-0.5 text-xs font-medium cursor-pointer transition-opacity hover:opacity-80 sm:w-28 sm:justify-center",
                statusCfg.badgeBg, statusCfg.badgeText,
              )}>
                {statusLabels[project.status]}
              </button>
            }
          />
          <DropdownMenuContent align="start" className="w-44">
            {PROJECT_STATUS_ORDER.map((s) => (
              <DropdownMenuItem key={s} onClick={() => handleUpdate({ status: s as ProjectStatus })}>
                <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
                <span>{statusLabels[s]}</span>
                {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Progress (read-only) */}
        <span className="flex shrink-0 items-center gap-1.5 sm:w-24 sm:justify-center">
          {project.issue_count > 0 ? (
            <>
              <span className="relative h-1.5 w-12 overflow-hidden rounded-full bg-muted">
                <span
                  className="absolute inset-y-0 left-0 rounded-full bg-emerald-500 transition-all"
                  style={{ width: `${Math.round((project.done_count / project.issue_count) * 100)}%` }}
                />
              </span>
              <span className="text-xs text-muted-foreground tabular-nums">
                {project.done_count}/{project.issue_count}
              </span>
            </>
          ) : (
            <span className="text-xs text-muted-foreground">--</span>
          )}
        </span>

        {/* Lead — popover */}
        <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
          <PopoverTrigger
            render={
              <button type="button" className="flex shrink-0 items-center justify-center rounded-full transition-all hover:ring-2 hover:ring-accent sm:w-10">
                {project.lead_type && project.lead_id ? (
                  <Tooltip>
                    <TooltipTrigger render={<span><ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={22} enableHoverCard /></span>} />
                    <TooltipContent side="bottom">{getActorName(project.lead_type, project.lead_id)}</TooltipContent>
                  </Tooltip>
                ) : (
                  <span className="h-[22px] w-[22px] rounded-full border border-dashed border-muted-foreground/30" />
                )}
              </button>
            }
          />
          <PopoverContent align="start" className="w-52 p-0">
            <div className="border-b px-2 py-1.5">
              <input
                type="text"
                value={leadFilter}
                onChange={(e) => setLeadFilter(e.target.value)}
                placeholder={t(($) => $.lead.assign_placeholder)}
                className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
              />
            </div>
            <div className="max-h-60 overflow-y-auto p-1">
              <button
                type="button"
                onClick={() => { handleUpdate({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
              >
                <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
              </button>
              {filteredMembers.length > 0 && (
                <>
                  <div className="px-2 pb-1 pt-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">{t(($) => $.lead.members_group)}</div>
                  {filteredMembers.map((m) => (
                    <button
                      type="button"
                      key={m.user_id}
                      onClick={() => { handleUpdate({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
                    >
                      <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                      <span>{m.name}</span>
                    </button>
                  ))}
                </>
              )}
              {filteredAgents.length > 0 && (
                <>
                  <div className="px-2 pb-1 pt-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">{t(($) => $.lead.agents_group)}</div>
                  {filteredAgents.map((a) => (
                    <button
                      type="button"
                      key={a.id}
                      onClick={() => { handleUpdate({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
                    >
                      <ActorAvatar actorType="agent" actorId={a.id} size={16} showStatusDot />
                      <span>{a.name}</span>
                    </button>
                  ))}
                </>
              )}
              {filteredMembers.length === 0 && filteredAgents.length === 0 && leadFilter && (
                <div className="px-2 py-3 text-center text-sm text-muted-foreground">{t(($) => $.lead.no_results)}</div>
              )}
            </div>
          </PopoverContent>
        </Popover>

        {/* Created */}
        <span className="text-xs tabular-nums text-muted-foreground sm:w-20 sm:shrink-0 sm:text-right">
          {formatRelativeDate(project.created_at)}
        </span>
      </div>
    </div>
  );
}


export function ProjectsPage() {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: projects = [], isLoading } = useQuery(projectListOptions(wsId));
  const openCreateProject = () => useModalStore.getState().open("create-project");

  return (
    <div className="flex h-full flex-col">
      {/* Header bar */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FolderKanban className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && projects.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{projects.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={openCreateProject}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.page.new_project)}
        </Button>
      </PageHeader>

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 sm:flex">
              <span className="shrink-0 w-[24px]" />
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-8 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
            </div>
            <div className="space-y-2 p-4 pt-0 sm:space-y-1 sm:p-5 sm:pt-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[72px] w-full sm:h-11" />
              ))}
            </div>
          </>
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
            <FolderKanban className="h-10 w-10 mb-3 opacity-30" />
            <p className="text-sm">{t(($) => $.page.empty)}</p>
            <Button size="sm" variant="outline" className="mt-3" onClick={openCreateProject}>
              {t(($) => $.page.create_first)}
            </Button>
          </div>
        ) : (
          <>
            {/* Column headers */}
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground sm:flex">
              {/* Icon spacer + Name */}
              <span className="shrink-0 w-[24px]" />
              <span className="min-w-0 flex-1">{t(($) => $.table.name)}</span>
              <span className="w-24 text-center shrink-0">{t(($) => $.table.priority)}</span>
              <span className="w-28 text-center shrink-0">{t(($) => $.table.status)}</span>
              <span className="w-24 text-center shrink-0">{t(($) => $.table.progress)}</span>
              <span className="w-10 text-center shrink-0">{t(($) => $.table.lead)}</span>
              <span className="w-20 text-right shrink-0">{t(($) => $.table.created)}</span>
            </div>
            {/* Rows */}
            {projects.map((project) => (
              <ProjectRow key={project.id} project={project} />
            ))}
          </>
        )}
      </div>
    </div>
  );
}
