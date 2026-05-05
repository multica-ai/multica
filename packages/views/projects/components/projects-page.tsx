"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import { Plus, FolderKanban, UserMinus, Check, Pencil } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions, archivedProjectListOptions } from "@multica/core/projects/queries";
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

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

function ProjectRow({ project }: { project: Project }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];
  const updateProject = useUpdateProject();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();

  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(leadQuery));

  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => {
      updateProject.mutate({ id: project.id, ...data });
    },
    [project.id, updateProject],
  );

  // Inline-rename state. Single-click on the title still navigates via the
  // AppLink (default browser behavior); double-click flips into edit mode.
  // Enter / blur commits; Escape cancels and reverts.
  const [isEditingTitle, setIsEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState(project.title);
  const titleInputRef = useRef<HTMLInputElement | null>(null);

  // Keep the draft in sync if the project is renamed elsewhere (e.g. detail
  // page or a real-time event from another client) while we're not actively
  // editing.
  useEffect(() => {
    if (!isEditingTitle) setTitleDraft(project.title);
  }, [project.title, isEditingTitle]);

  // Auto-focus + select-all when entering edit mode so the user can type
  // immediately or extend the existing name without an extra click.
  useEffect(() => {
    if (isEditingTitle) {
      const el = titleInputRef.current;
      el?.focus();
      el?.select();
    }
  }, [isEditingTitle]);

  const commitTitle = useCallback(() => {
    const trimmed = titleDraft.trim();
    setIsEditingTitle(false);
    if (!trimmed) {
      // Empty input — revert silently rather than wiping the project name.
      setTitleDraft(project.title);
      return;
    }
    if (trimmed !== project.title) {
      handleUpdate({ title: trimmed });
    }
  }, [titleDraft, project.title, handleUpdate]);

  const cancelTitleEdit = useCallback(() => {
    setTitleDraft(project.title);
    setIsEditingTitle(false);
  }, [project.title]);

  return (
    <div className="group/row flex h-11 items-center gap-2 px-5 text-sm transition-colors hover:bg-accent/40">
      {/* Icon + Name. Single-click navigates (AppLink). Double-click flips
          into inline rename mode. While editing, the AppLink is replaced
          with an <input> so clicks don't navigate. */}
      {isEditingTitle ? (
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <ProjectIcon project={project} size="md" />
          <input
            ref={titleInputRef}
            type="text"
            value={titleDraft}
            onChange={(e) => setTitleDraft(e.target.value)}
            onBlur={commitTitle}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitTitle();
              } else if (e.key === "Escape") {
                e.preventDefault();
                cancelTitleEdit();
              }
            }}
            aria-label="Project name"
            className="min-w-0 flex-1 bg-transparent border-b border-primary/40 font-medium outline-none focus:border-primary"
          />
        </div>
      ) : (
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <AppLink
            href={wsPaths.projectDetail(project.id)}
            className="flex min-w-0 flex-1 items-center gap-2"
          >
            <ProjectIcon project={project} size="md" />
            <span
              className="min-w-0 flex-1 truncate font-medium"
              // Double-click also flips into edit mode for power users —
              // the visible pencil button is the discoverable affordance,
              // but dblclick is the muscle-memory shortcut.
              onDoubleClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setIsEditingTitle(true);
              }}
            >
              {project.title}
            </span>
          </AppLink>
          {/* Hover-revealed rename button. Sibling of AppLink so a click
              doesn't trigger navigation. Visible on row hover (and always
              visible to keyboard focus). */}
          <button
            type="button"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              setIsEditingTitle(true);
            }}
            className="shrink-0 inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover/row:opacity-60 hover:opacity-100"
            aria-label={`Rename ${project.title}`}
            title="Rename project"
          >
            <Pencil className="size-3" />
          </button>
        </div>
      )}

      {/* Priority — dropdown */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button type="button" className="flex w-24 items-center justify-center gap-1 shrink-0 rounded px-1 py-0.5 hover:bg-accent/60 transition-colors cursor-pointer">
              <PriorityIcon priority={project.priority} />
              <span className={cn("text-xs", priorityCfg.color)}>{priorityCfg.label}</span>
            </button>
          }
        />
        <DropdownMenuContent align="start" className="w-44">
          {PROJECT_PRIORITY_ORDER.map((p) => (
            <DropdownMenuItem key={p} onClick={() => handleUpdate({ priority: p as ProjectPriority })}>
              <PriorityIcon priority={p} />
              <span>{PROJECT_PRIORITY_CONFIG[p].label}</span>
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
              "inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs font-medium shrink-0 w-28 justify-center cursor-pointer hover:opacity-80 transition-opacity",
              statusCfg.badgeBg, statusCfg.badgeText,
            )}>
              {statusCfg.label}
            </button>
          }
        />
        <DropdownMenuContent align="start" className="w-44">
          {PROJECT_STATUS_ORDER.map((s) => (
            <DropdownMenuItem key={s} onClick={() => handleUpdate({ status: s as ProjectStatus })}>
              <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
              <span>{PROJECT_STATUS_CONFIG[s].label}</span>
              {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Progress (read-only) */}
      <span className="flex w-24 items-center justify-center gap-1.5 shrink-0">
        {project.issue_count > 0 ? (
          <>
            <span className="relative h-1.5 w-12 rounded-full bg-muted overflow-hidden">
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
            <button type="button" className="flex w-10 items-center justify-center shrink-0 rounded-full hover:ring-2 hover:ring-accent transition-all cursor-pointer">
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
          <div className="px-2 py-1.5 border-b">
            <input
              type="text"
              value={leadFilter}
              onChange={(e) => setLeadFilter(e.target.value)}
              placeholder="Assign lead..."
              className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
            />
          </div>
          <div className="p-1 max-h-60 overflow-y-auto">
            <button
              type="button"
              onClick={() => { handleUpdate({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
            >
              <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">No lead</span>
            </button>
            {filteredMembers.length > 0 && (
              <>
                <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Members</div>
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
                <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Agents</div>
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
              <div className="px-2 py-3 text-center text-sm text-muted-foreground">No results</div>
            )}
          </div>
        </PopoverContent>
      </Popover>

      {/* Created */}
      <span className="w-20 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatRelativeDate(project.created_at)}
      </span>
    </div>
  );
}


export function ProjectsPage() {
  const wsId = useWorkspaceId();
  // Two parallel cached lists — active (default) and archived. We always
  // load the active list and conditionally swap to the archived list when
  // the user toggles "Show archived." Cached separately so toggling
  // doesn't refetch the active set.
  const [showArchived, setShowArchived] = useState(false);
  const activeQuery = useQuery(projectListOptions(wsId));
  const archivedQuery = useQuery({
    ...archivedProjectListOptions(wsId),
    enabled: showArchived,
  });
  const projects = showArchived
    ? (archivedQuery.data ?? []).filter((p) => !!p.archived_at)
    : activeQuery.data ?? [];
  const isLoading = showArchived ? archivedQuery.isLoading : activeQuery.isLoading;
  const openCreateProject = () => useModalStore.getState().open("create-project");

  return (
    <div className="flex h-full flex-col">
      {/* Header bar */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FolderKanban className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Projects</h1>
          {!isLoading && projects.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{projects.length}</span>
          )}
          {showArchived && (
            <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-600 dark:text-amber-400">
              Archived
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowArchived((v) => !v)}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
            title={showArchived ? "Show active projects" : "Show archived projects"}
          >
            {showArchived ? "Show active" : "Show archived"}
          </button>
          <Button size="sm" variant="outline" onClick={openCreateProject}>
            <Plus className="h-3.5 w-3.5 mr-1" />
            New project
          </Button>
        </div>
      </PageHeader>

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5">
              <span className="shrink-0 w-[24px]" />
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-8 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
            </div>
            <div className="p-5 pt-1 space-y-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-11 w-full" />
              ))}
            </div>
          </>
        ) : projects.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
            <FolderKanban className="h-10 w-10 mb-3 opacity-30" />
            <p className="text-sm">
              {showArchived ? "No archived projects" : "No projects yet"}
            </p>
            {!showArchived && (
              <Button size="sm" variant="outline" className="mt-3" onClick={openCreateProject}>
                Create your first project
              </Button>
            )}
          </div>
        ) : (
          <>
            {/* Column headers */}
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground">
              {/* Icon spacer + Name */}
              <span className="shrink-0 w-[24px]" />
              <span className="min-w-0 flex-1">Name</span>
              <span className="w-24 text-center shrink-0">Priority</span>
              <span className="w-28 text-center shrink-0">Status</span>
              <span className="w-24 text-center shrink-0">Progress</span>
              <span className="w-10 text-center shrink-0">Lead</span>
              <span className="w-20 text-right shrink-0">Created</span>
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
