"use client";

import { useState, useCallback } from "react";
import { Plus, FolderKanban, UserMinus, Check, Rows3, LayoutGrid } from "lucide-react";
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
  DropdownMenuGroup,
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
import { useProjectViewStore } from "@multica/core/projects";

function ProjectCard({ project }: { project: Project }) {
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

  const progressPercent = project.issue_count > 0 ? Math.round((project.done_count / project.issue_count) * 100) : 0;

  return (
    <div className="group/card flex flex-col rounded-md border bg-card hover:border-primary/50 transition-colors">
      <div className="p-3 pb-2">
        <div className="flex items-center gap-2">
          <AppLink
            href={wsPaths.projectDetail(project.id)}
            className="flex items-center gap-2 min-w-0 flex-1"
          >
            <ProjectIcon project={project} size="sm" />
            <h3 className="font-medium text-sm truncate">{project.title}</h3>
          </AppLink>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button type="button" className={cn(
                  "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium cursor-pointer hover:opacity-80 transition-opacity shrink-0",
                  statusCfg.badgeBg, statusCfg.badgeText,
                )}>
                  {statusLabels[project.status]}
                </button>
              }
            />
            <DropdownMenuContent align="end" className="w-44">
              {PROJECT_STATUS_ORDER.map((s) => (
                <DropdownMenuItem key={s} onClick={() => handleUpdate({ status: s as ProjectStatus })}>
                  <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
                  <span>{statusLabels[s]}</span>
                  {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {project.issue_count > 0 ? (
          <div className="flex justify-end items-center gap-1.5 pt-2">
            <div className="relative h-4 w-4">
              <svg className="h-4 w-4 -rotate-90" viewBox="0 0 16 16">
                <circle
                  className="text-muted"
                  strokeWidth="2"
                  stroke="currentColor"
                  fill="none"
                  r="6"
                  cx="8"
                  cy="8"
                />
                <circle
                  className="text-emerald-500"
                  strokeWidth="2"
                  stroke="currentColor"
                  fill="none"
                  r="6"
                  cx="8"
                  cy="8"
                  strokeDasharray={`${progressPercent * 0.377} 37.7`}
                  strokeLinecap="round"
                />
              </svg>
            </div>
            <span className="text-[10px] text-muted-foreground tabular-nums">
              {project.done_count}/{project.issue_count}
            </span>
          </div>
        ) : (
          <span className="text-[10px] text-muted-foreground pt-2 flex justify-end">{t(($) => $.detail.no_issues_yet)}</span>
        )}
      </div>

      <div className="flex items-center justify-between px-3 pb-3 border-t mt-0 pt-2">
        <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
          <PopoverTrigger
            render={
              <button type="button" className="flex items-center gap-1.5 rounded px-1.5 py-0.5 -mx-1.5 hover:bg-accent/60 transition-colors cursor-pointer">
                {project.lead_type && project.lead_id ? (
                  <Tooltip>
                    <TooltipTrigger render={<span><ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={20} enableHoverCard /></span>} />
                    <TooltipContent side="bottom">{getActorName(project.lead_type, project.lead_id)}</TooltipContent>
                  </Tooltip>
                ) : (
                  <span className="inline-flex h-5 w-5 rounded-full border border-dashed border-muted-foreground/30" />
                )}
                <span className="text-[10px] text-muted-foreground truncate max-w-[60px]">
                  {project.lead_type && project.lead_id ? getActorName(project.lead_type, project.lead_id) : t(($) => $.lead.no_lead)}
                </span>
              </button>
            }
          />
          <PopoverContent align="start" className="w-52 p-0">
            <div className="px-2 py-1.5 border-b">
              <input
                type="text"
                value={leadFilter}
                onChange={(e) => setLeadFilter(e.target.value)}
                placeholder={t(($) => $.lead.assign_placeholder)}
                className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
              />
            </div>
            <div className="p-1 max-h-48 overflow-y-auto">
              <button
                type="button"
                onClick={() => { handleUpdate({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
              >
                <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
              </button>
              {filteredMembers.length > 0 && (
                <>
                  <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.members_group)}</div>
                  {filteredMembers.map((m) => (
                    <button
                      type="button"
                      key={m.user_id}
                      onClick={() => { handleUpdate({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                    >
                      <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                      <span>{m.name}</span>
                    </button>
                  ))}
                </>
              )}
              {filteredAgents.length > 0 && (
                <>
                  <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.agents_group)}</div>
                  {filteredAgents.map((a) => (
                    <button
                      type="button"
                      key={a.id}
                      onClick={() => { handleUpdate({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
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

        <div className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button type="button" className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium hover:bg-accent/60 transition-colors cursor-pointer">
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
          <span className="text-[10px] text-muted-foreground">
            {formatRelativeDate(project.created_at)}
          </span>
        </div>
      </div>
    </div>
  );
}


function ProjectCardCompact({ project }: { project: Project }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const formatRelativeDate = useFormatRelativeDate();
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];
  const updateProject = useUpdateProject();
  const { getActorName } = useActorName();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

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

  const leadId = project.lead_id;
  const leadType = project.lead_type;
  const leadName = leadId && leadType ? getActorName(leadType, leadId) : null;

  return (
    <div className="grid w-full min-w-[740px] grid-cols-[24px_minmax(200px,1fr)_96px_96px_80px_80px_80px] h-10 items-center gap-2 px-4 text-sm transition-colors hover:bg-accent/40 border-b">
      <ProjectIcon project={project} size="sm" />
      <AppLink
        href={wsPaths.projectDetail(project.id)}
        className="flex items-center justify-start gap-2 min-w-0 overflow-hidden"
      >
        <span className="font-medium truncate text-left">{project.title}</span>
      </AppLink>

      <div className="flex items-center justify-start">
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button type="button" className="flex items-center justify-start gap-1 rounded px-1 py-0.5 hover:bg-accent/60 transition-colors cursor-pointer">
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
      </div>

      <div className="flex items-center justify-start">
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button type="button" className={cn(
                "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium cursor-pointer hover:opacity-80 transition-opacity",
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
      </div>

      <span className="flex items-center justify-start gap-1.5 text-xs text-muted-foreground tabular-nums">
        {project.issue_count > 0 ? `${project.done_count}/${project.issue_count}` : "--"}
      </span>

      <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
        <PopoverTrigger
          render={
            <button type="button" className="flex items-center justify-start gap-1.5 rounded px-1 py-0.5 hover:bg-accent/60 transition-colors cursor-pointer">
              <span className="shrink-0">
                {project.lead_type && project.lead_id ? (
                  <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={20} enableHoverCard />
                ) : (
                  <span className="inline-flex h-5 w-5 rounded-full border border-dashed border-muted-foreground/30" />
                )}
              </span>
              <span className="text-xs text-muted-foreground truncate max-w-[50px]">
                {leadName ?? t(($) => $.lead.no_lead)}
              </span>
            </button>
          }
        />
        <PopoverContent align="start" className="w-52 p-0">
          <div className="px-2 py-1.5 border-b">
            <input
              type="text"
              value={leadFilter}
              onChange={(e) => setLeadFilter(e.target.value)}
              placeholder={t(($) => $.lead.assign_placeholder)}
              className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
            />
          </div>
          <div className="p-1 max-h-48 overflow-y-auto">
            <button
              type="button"
              onClick={() => { handleUpdate({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
            >
              <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">{t(($) => $.lead.no_lead)}</span>
            </button>
            {filteredMembers.length > 0 && (
              <>
                <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.members_group)}</div>
                {filteredMembers.map((m) => (
                  <button
                    type="button"
                    key={m.user_id}
                    onClick={() => { handleUpdate({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                  >
                    <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                    <span>{m.name}</span>
                  </button>
                ))}
              </>
            )}
            {filteredAgents.length > 0 && (
              <>
                <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">{t(($) => $.lead.agents_group)}</div>
                {filteredAgents.map((a) => (
                  <button
                    type="button"
                    key={a.id}
                    onClick={() => { handleUpdate({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
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

      <span className="text-left text-xs text-muted-foreground tabular-nums">
        {formatRelativeDate(project.created_at)}
      </span>
    </div>
  );
}


export function ProjectsPage() {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const viewMode = useProjectViewStore((s) => s.viewMode);
  const setViewMode = useProjectViewStore((s) => s.setViewMode);
  const isCompact = viewMode === "compact";
  const gridClass = isCompact ? "grid-cols-1" : "grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3";
  const { data: projects = [], isLoading } = useQuery(projectListOptions(wsId));
  const openCreateProject = () => useModalStore.getState().open("create-project");

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FolderKanban className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && projects.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{projects.length}</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={openCreateProject}>
            <Plus className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.page.new_project)}
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto flex flex-col">
        {(projects.length > 0 || isLoading) && (
          <div className="flex justify-end px-5 pt-4 -mb-2">
            <DropdownMenu>
              <Tooltip>
                <DropdownMenuTrigger
                  render={
                    <TooltipTrigger
                      render={
                        <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                          {isCompact ? <Rows3 className="size-4" /> : <LayoutGrid className="size-4" />}
                        </Button>
                      }
                    />
                  }
                />
              <TooltipContent side="bottom">
                {isCompact ? t(($) => $.page.view_compact) : t(($) => $.page.view_comfortable)}
              </TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end" className="w-auto">
                <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => setViewMode("compact")}>
                  <Rows3 className="mr-2 h-4 w-4" />
                  {t(($) => $.page.view_compact)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setViewMode("comfortable")}>
                  <LayoutGrid className="mr-2 h-4 w-4" />
                  {t(($) => $.page.view_comfortable)}
                </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
        {isLoading ? (
          isCompact ? (
            <div className="pt-4 overflow-x-auto">
              <div className="min-w-[600px]">
                <div className="flex h-10 items-center gap-2 px-4 border-b">
                  <Skeleton className="h-6 w-6 rounded" />
                  <Skeleton className="h-4 w-48" />
                </div>
                {Array.from({ length: 6 }).map((_, i) => (
                  <div key={i} className="flex h-10 items-center gap-2 px-4 border-b">
                    <Skeleton className="h-6 w-6 rounded" />
                    <Skeleton className="h-4 w-48" />
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className={cn("pt-4 grid px-5", gridClass)}>
              {Array.from({ length: 8 }).map((_, i) => (
                <div key={i} className={cn("flex flex-col rounded-md border p-3 gap-2")}>
                  <div className="flex items-center gap-2">
                    <Skeleton className="h-8 w-8 rounded" />
                    <Skeleton className="h-4 w-3/4" />
                  </div>
                  <div className="flex gap-1.5">
                    <Skeleton className="h-5 w-16 rounded" />
                    <Skeleton className="h-5 w-20 rounded" />
                  </div>
                  <div className="flex items-center justify-between">
                    <Skeleton className="h-5 w-5 rounded-full" />
                    <Skeleton className="h-3 w-12" />
                  </div>
                </div>
              ))}
            </div>
          )
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
            <FolderKanban className="h-10 w-10 mb-3 opacity-30" />
            <p className="text-sm">{t(($) => $.page.empty)}</p>
            <Button size="sm" variant="outline" className="mt-3" onClick={openCreateProject}>
              {t(($) => $.page.create_first)}
            </Button>
          </div>
        ) : isCompact ? (
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border mt-4 mx-5">
            <div className="flex min-h-0 flex-1 flex-col overflow-auto min-w-0 pb-4">
              <div className="grid w-full min-w-[740px] grid-cols-[24px_minmax(200px,1fr)_96px_96px_80px_80px_80px] h-8 shrink-0 items-center gap-2 px-4 text-xs font-medium text-muted-foreground border-b bg-muted/30 sticky top-0 z-10">
                <span />
                <span className="text-left">{t(($) => $.table.name)}</span>
                <span className="text-left">{t(($) => $.table.priority)}</span>
                <span className="text-left">{t(($) => $.table.status)}</span>
                <span className="text-left">{t(($) => $.table.progress)}</span>
                <span className="text-left">{t(($) => $.table.lead)}</span>
                <span className="text-left">{t(($) => $.table.created)}</span>
              </div>
              {projects.map((project) => (
                <ProjectCardCompact key={project.id} project={project} />
              ))}
            </div>
          </div>
        ) : (
          <div className={cn("pt-4 pb-5 px-5 grid", gridClass)}>
            {projects.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}