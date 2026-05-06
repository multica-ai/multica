"use client";

import { useMemo, useState, useCallback, useRef, useEffect } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { Archive, Check, ChevronRight, Link2, ListTodo, MoreHorizontal, PanelRight, Pin, PinOff, Plus, Trash2, UserMinus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Issue, IssueStatus, ProjectStatus, ProjectPriority } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { projectDetailOptions } from "@multica/core/projects/queries";
import {
  useUpdateProject,
  useDeleteProject,
  useArchiveProject,
  useRestoreProject,
} from "@multica/core/projects/mutations";
import { pinListOptions } from "@multica/core/pins";
import { useCreatePin, useDeletePin } from "@multica/core/pins";
import { myIssueListOptions, childIssueProgressOptions, type MyIssuesFilter } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { PROJECT_STATUS_ORDER, PROJECT_STATUS_CONFIG, PROJECT_PRIORITY_ORDER, PROJECT_PRIORITY_CONFIG } from "@multica/core/projects/config";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider, useViewStore } from "@multica/core/issues/stores/view-store-context";
import { filterIssues } from "../../issues/utils/filter";
import { getProjectIssueMetrics } from "./project-issue-metrics";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { TitleEditor, ContentEditor, type ContentEditorRef } from "../../editor";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectResourcesSection } from "./project-resources-section";
import { IssuesHeader } from "../../issues/components/issues-header";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { PageHeader } from "../../layout/page-header";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";

// ---------------------------------------------------------------------------
// Property row — sidebar property display
// ---------------------------------------------------------------------------

function PropRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-8 items-center gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors">
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs truncate">
        {children}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Project Issues — reuses the existing issues list/board components
// ---------------------------------------------------------------------------

const projectViewStore = createIssueViewStore("project_issues_view");

function ProjectIssuesContent({
  projectId,
  projectIssues,
  scope,
  filter,
}: {
  projectId: string;
  projectIssues: Issue[];
  scope: string;
  filter: MyIssuesFilter;
}) {
  const wsId = useWorkspaceId();
  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const labelFilters = useViewStore((s) => s.labelFilters);

  const issues = useMemo(
    () => filterIssues(projectIssues, { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters: [], includeNoProject: false, labelFilters }),
    [projectIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, labelFilters],
  );

  const { data: childProgressMap = new Map() } = useQuery(childIssueProgressOptions(wsId));

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0)
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const updateIssueMutation = useUpdateIssue();
  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const updates: Partial<{ status: IssueStatus; position: number }> = { status: newStatus };
      if (newPosition !== undefined) updates.position = newPosition;
      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error("Failed to move issue") },
      );
    },
    [updateIssueMutation],
  );

  if (projectIssues.length === 0) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground">
        <ListTodo className="h-10 w-10 text-muted-foreground/40" />
        <p className="text-sm">No issues linked</p>
        <p className="text-xs">Create a new issue or assign existing ones to this project.</p>
        <Button
          variant="outline"
          size="sm"
          className="mt-1"
          onClick={() =>
            useModalStore.getState().open("create-issue", { project_id: projectId })
          }
        >
          <Plus className="size-3.5 mr-1.5" />
          New Issue
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {viewMode === "board" ? (
        <BoardView
          issues={issues}
          visibleStatuses={visibleStatuses}
          hiddenStatuses={hiddenStatuses}
          onMoveIssue={handleMoveIssue}
          childProgressMap={childProgressMap}
          myIssuesScope={scope}
          myIssuesFilter={filter}
        />
      ) : (
        <ListView
          issues={issues}
          visibleStatuses={visibleStatuses}
          // Drag-to-move parity with BoardView. Without this, the ListView
          // renders rows in read-only mode (the prop being undefined is the
          // signal to ListView to disable its sortable wiring), so users on
          // the project detail's list tab couldn't drag issues between
          // statuses even though the same UX works on /<slug>/issues. The
          // handler itself is the same one BoardView uses two lines up —
          // updateIssueMutation with status (and optional position).
          onMoveIssue={handleMoveIssue}
          childProgressMap={childProgressMap}
          myIssuesScope={scope}
          myIssuesFilter={filter}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ProjectDetail
// ---------------------------------------------------------------------------

export function ProjectDetail({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const workspace = useCurrentWorkspace();
  const workspaceName = workspace?.name;
  const { data: project, isLoading } = useQuery(projectDetailOptions(wsId, projectId));
  const projectScope = `project:${projectId}`;
  const projectFilter = useMemo<MyIssuesFilter>(
    () => ({ project_id: projectId }),
    [projectId],
  );
  const { data: projectIssues = [] } = useQuery(
    myIssueListOptions(wsId, projectScope, projectFilter),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();
  const archiveProject = useArchiveProject();
  const restoreProject = useRestoreProject();
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some((p) => p.item_type === "project" && p.item_id === projectId);
  const createPin = useCreatePin();
  const deletePinMut = useDeletePin();
  const descEditorRef = useRef<ContentEditorRef>(null);
  const isMobile = useIsMobile();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [progressOpen, setProgressOpen] = useState(true);
  const [descriptionOpen, setDescriptionOpen] = useState(true);

  // Sidebar panel
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_project_detail_layout",
  });
  const sidebarRef = usePanelRef();
  // Desktop and mobile sidebar state must be separate. A single state defaulting
  // to `true` made the mobile <Sheet> mount in the open position on first render
  // (after `useIsMobile()` flipped from false→true), briefly covering the page
  // with its modal backdrop and locking scroll — leaving the page unresponsive.
  const [desktopSidebarOpen, setDesktopSidebarOpen] = useState(true);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const sidebarOpen = isMobile ? mobileSidebarOpen : desktopSidebarOpen;

  useEffect(() => {
    if (isMobile) {
      setMobileSidebarOpen(false);
    }
  }, [isMobile]);

  // Lead popover
  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(leadQuery));

  const handleUpdateField = useCallback(
    (data: Parameters<typeof updateProject.mutate>[0] extends { id: string } & infer R ? R : never) => {
      if (!project) return;
      updateProject.mutate({ id: project.id, ...data });
    },
    [project, updateProject],
  );

  const handleDelete = useCallback(() => {
    if (!project) return;
    deleteProject.mutate(project.id, {
      onSuccess: () => {
        toast.success("Project deleted");
        router.push(wsPaths.projects());
      },
    });
  }, [project, deleteProject, router, wsPaths]);

  const handleArchive = useCallback(() => {
    if (!project) return;
    archiveProject.mutate(project.id, {
      onSuccess: () => {
        toast.success("Project archived");
        // Don't navigate away — leave the user on the detail page so the
        // banner appears and they can hit Restore if they didn't mean it.
        // The active projects list will refetch in the background.
      },
      onError: (e) => {
        toast.error(e instanceof Error ? e.message : "Failed to archive project");
      },
    });
  }, [project, archiveProject]);

  const handleRestore = useCallback(() => {
    if (!project) return;
    restoreProject.mutate(project.id, {
      onSuccess: () => toast.success("Project restored"),
      onError: (e) => {
        toast.error(e instanceof Error ? e.message : "Failed to restore project");
      },
    });
  }, [project, restoreProject]);

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-4xl px-8 py-10 space-y-4">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-40 w-full mt-8" />
      </div>
    );
  }

  if (!project) {
    return <div className="flex items-center justify-center h-full text-muted-foreground">Project not found</div>;
  }

  const issueMetrics = getProjectIssueMetrics(project);
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];

  const sidebarContent = (
    <div className="space-y-5">
      {/* Icon + Title */}
      <div>
        <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="text-2xl cursor-pointer rounded-lg p-1 -ml-1 hover:bg-accent/60 transition-colors"
                title="Change icon"
              >
                {project.icon || "📁"}
              </button>
            }
          />
          <PopoverContent align="start" className="w-auto p-0">
            <EmojiPicker
              onSelect={(emoji) => {
                handleUpdateField({ icon: emoji });
                setIconPickerOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
        <TitleEditor
          key={`title-${projectId}`}
          defaultValue={project.title}
          placeholder="Project title"
          className="mt-2 w-full text-base font-semibold leading-snug tracking-tight"
          onBlur={(value) => {
            const trimmed = value.trim();
            if (trimmed && trimmed !== project.title) handleUpdateField({ title: trimmed });
          }}
        />
        {project.archived_at && (
          <div className="mt-3 flex items-center justify-between gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs">
            <div className="flex items-center gap-2">
              <Archive className="h-3.5 w-3.5 text-amber-500" />
              <span className="text-amber-700 dark:text-amber-400">
                Archived. Hidden from the projects list.
              </span>
            </div>
            <button
              type="button"
              className="font-medium text-amber-700 hover:underline dark:text-amber-400 disabled:opacity-50"
              onClick={handleRestore}
              disabled={restoreProject.isPending}
            >
              {restoreProject.isPending ? "Restoring..." : "Restore"}
            </button>
          </div>
        )}
      </div>

      {/* Properties */}
      <div>
        <button
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPropertiesOpen(!propertiesOpen)}
        >
          Properties
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="space-y-0.5 pl-2">
          <PropRow label="Status">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    <span className={cn("size-2 rounded-full", statusCfg.dotColor)} />
                    <span>{statusCfg.label}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_STATUS_ORDER.map((s) => (
                  <DropdownMenuItem key={s} onClick={() => handleUpdateField({ status: s as ProjectStatus })}>
                    <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
                    <span>{PROJECT_STATUS_CONFIG[s].label}</span>
                    {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label="Priority">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    <PriorityIcon priority={project.priority} />
                    <span>{priorityCfg.label}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_PRIORITY_ORDER.map((p) => (
                  <DropdownMenuItem key={p} onClick={() => handleUpdateField({ priority: p as ProjectPriority })}>
                    <PriorityIcon priority={p} />
                    <span>{PROJECT_PRIORITY_CONFIG[p].label}</span>
                    {p === project.priority && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label="Lead">
            <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
              <PopoverTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    {project.lead_type && project.lead_id ? (
                      <>
                        <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={16} enableHoverCard showStatusDot />
                        <span className="cursor-pointer">{getActorName(project.lead_type, project.lead_id)}</span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">No lead</span>
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
                    onClick={() => { handleUpdateField({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
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
                          onClick={() => { handleUpdateField({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
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
                          onClick={() => { handleUpdateField({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
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
          </PropRow>
        </div>}
      </div>

      {/* Progress */}
      {issueMetrics.totalCount > 0 && (() => {
        const pct = Math.round((issueMetrics.completedCount / issueMetrics.totalCount) * 100);
        return (
          <div>
            <button
              className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${progressOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
              onClick={() => setProgressOpen(!progressOpen)}
            >
              Progress
              <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${progressOpen ? "rotate-90" : ""}`} />
            </button>
            {progressOpen && <div className="pl-2 flex items-center gap-3">
              <div className="relative h-2 flex-1 rounded-full bg-muted overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 rounded-full bg-emerald-500 transition-all"
                  style={{ width: `${pct}%` }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums shrink-0">
                {issueMetrics.completedCount}/{issueMetrics.totalCount}
              </span>
            </div>}
          </div>
        );
      })()}

      {/* Description */}
      <div>
        <button
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${descriptionOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setDescriptionOpen(!descriptionOpen)}
        >
          Description
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${descriptionOpen ? "rotate-90" : ""}`} />
        </button>
        {descriptionOpen && <div className="pl-2">
          <ContentEditor
            ref={descEditorRef}
            key={projectId}
            defaultValue={project.description || ""}
            placeholder="Add description..."
            onUpdate={(md) => handleUpdateField({ description: md || null })}
            debounceMs={1500}
          />
        </div>}
      </div>

      {/* Resources */}
      <ProjectResourcesSection projectId={projectId} />
    </div>
  );

  return (
    <>
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
        <div className="flex h-full flex-col">
          <PageHeader className="gap-2 bg-background text-sm">
            <div className="flex flex-1 items-center gap-1.5 min-w-0">
              <AppLink href={wsPaths.projects()} className="text-muted-foreground hover:text-foreground transition-colors shrink-0">
                {workspaceName ?? "Projects"}
              </AppLink>
              <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              <span className="truncate">{project.title}</span>
            </div>
            <div className="flex items-center gap-1 shrink-0">
              <Button
                variant="ghost"
                size="icon-sm"
                className={cn("text-muted-foreground", isPinned && "text-foreground")}
                title={isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
                onClick={() => {
                  if (isPinned) {
                    deletePinMut.mutate({ itemType: "project", itemId: projectId });
                  } else {
                    createPin.mutate({ item_type: "project", item_id: projectId });
                  }
                }}
              >
                {isPinned ? <PinOff /> : <Pin />}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end" className="w-auto">
                  <DropdownMenuItem onClick={() => {
                    navigator.clipboard.writeText(window.location.href);
                    toast.success("Link copied");
                  }}>
                    <Link2 className="h-3.5 w-3.5" />
                    Copy link
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  {/*
                   * Archive vs Delete:
                   * - Archive (soft-delete): keeps the row and all issue
                   *   references; reversible. The default for "I don't
                   *   want to see this anymore."
                   * - Delete: hard delete; cascades through resources;
                   *   issue.project_id gets nulled out; irreversible.
                   *
                   * The menu item shown depends on current state — an
                   * archived project shows Restore + Delete; an active
                   * one shows Archive + Delete.
                   */}
                  {project.archived_at ? (
                    <DropdownMenuItem
                      onClick={handleRestore}
                      disabled={restoreProject.isPending}
                    >
                      <Archive className="h-3.5 w-3.5" />
                      Restore project
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem
                      onClick={handleArchive}
                      disabled={archiveProject.isPending}
                    >
                      <Archive className="h-3.5 w-3.5" />
                      Archive project
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleteDialogOpen(true)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Delete project
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant={sidebarOpen ? "secondary" : "ghost"}
                      size="icon-sm"
                      className={sidebarOpen ? "" : "text-muted-foreground"}
                      onClick={() => {
                        if (isMobile) {
                          setMobileSidebarOpen((open) => !open);
                        } else {
                          const panel = sidebarRef.current;
                          if (!panel) return;
                          if (panel.isCollapsed()) panel.expand();
                          else panel.collapse();
                        }
                      }}
                    >
                      <PanelRight />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
              </Tooltip>
            </div>
          </PageHeader>

          <ViewStoreProvider store={projectViewStore}>
              <IssuesHeader scopedIssues={projectIssues} />
              <ProjectIssuesContent
                projectId={projectId}
                projectIssues={projectIssues}
                scope={projectScope}
                filter={projectFilter}
              />
              <BatchActionToolbar />
            </ViewStoreProvider>
          </div>
        </ResizablePanel>
        {!isMobile && <ResizableHandle />}
        {!isMobile && (
        <ResizablePanel
          id="sidebar"
          defaultSize={desktopSidebarOpen ? 320 : 0}
          minSize={260}
          maxSize={420}
          collapsible
          groupResizeBehavior="preserve-pixel-size"
          panelRef={sidebarRef}
          onResize={(size) => setDesktopSidebarOpen(size.inPixels > 0)}
        >
          <div className="overflow-y-auto border-l h-full">
            <div className="p-4">
              {sidebarContent}
            </div>
          </div>
        </ResizablePanel>
        )}
        {isMobile && (
          <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
            <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
              {sidebarContent}
            </SheetContent>
          </Sheet>
        )}
      </ResizablePanelGroup>

      {/* Delete confirmation */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete project</AlertDialogTitle>
            <AlertDialogDescription>
              This will delete the project. Issues will not be deleted but will be unlinked.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-white hover:bg-destructive/90">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
