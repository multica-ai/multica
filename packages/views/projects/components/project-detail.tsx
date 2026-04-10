"use client";

import { useMemo, useState, useCallback, useRef } from "react";
import { Check, ChevronRight, Link2, ListTodo, MoreHorizontal, Trash2, UserMinus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Issue, IssueStatus, ProjectStatus, ProjectPriority } from "@multica/core/types";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { useUpdateProject, useDeleteProject } from "@multica/core/projects/mutations";
import { issueListOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useActorName } from "@multica/core/workspace/hooks";
import { PROJECT_STATUS_ORDER, PROJECT_STATUS_CONFIG, PROJECT_PRIORITY_ORDER, PROJECT_PRIORITY_CONFIG } from "@multica/core/projects/config";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider, useViewStore } from "@multica/core/issues/stores/view-store-context";
import { filterIssues } from "../../issues/utils/filter";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { TitleEditor, ContentEditor, type ContentEditorRef } from "../../editor";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { IssuesHeader } from "../../issues/components/issues-header";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
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
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
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
// Property pill — inline clickable pill for status/lead
// ---------------------------------------------------------------------------

function PropertyPill({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
        "hover:bg-accent/60 transition-colors cursor-pointer",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Project Issues Tab — reuses the existing issues list/board components
// ---------------------------------------------------------------------------

const projectViewStore = createIssueViewStore("project_issues_view");

function ProjectIssuesTab({ projectIssues }: { projectIssues: Issue[] }) {
  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);

  const issues = useMemo(
    () => filterIssues(projectIssues, { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters }),
    [projectIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters],
  );

  const childProgressMap = useMemo(() => {
    const map = new Map<string, { done: number; total: number }>();
    for (const issue of projectIssues) {
      if (!issue.parent_issue_id) continue;
      const entry = map.get(issue.parent_issue_id);
      const isDone = issue.status === "done" || issue.status === "cancelled";
      if (entry) {
        entry.total++;
        if (isDone) entry.done++;
      } else {
        map.set(issue.parent_issue_id, { done: isDone ? 1 : 0, total: 1 });
      }
    }
    return map;
  }, [projectIssues]);

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
      const viewState = projectViewStore.getState();
      if (viewState.sortBy !== "position") {
        viewState.setSortBy("position");
        viewState.setSortDirection("asc");
      }
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
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
        <ListTodo className="h-10 w-10 text-muted-foreground/40" />
        <p className="text-sm">No issues linked</p>
        <p className="text-xs">Assign issues to this project from the issue detail page.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {viewMode === "board" ? (
        <BoardView
          issues={issues}
          allIssues={projectIssues}
          visibleStatuses={visibleStatuses}
          hiddenStatuses={hiddenStatuses}
          onMoveIssue={handleMoveIssue}
          childProgressMap={childProgressMap}
        />
      ) : (
        <ListView issues={issues} visibleStatuses={visibleStatuses} childProgressMap={childProgressMap} />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ProjectDetail
// ---------------------------------------------------------------------------

export function ProjectDetail({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);
  const { data: project, isLoading } = useQuery(projectDetailOptions(wsId, projectId));
  const { data: allIssues = [] } = useQuery(issueListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();
  const descEditorRef = useRef<ContentEditorRef>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [activeTab, setActiveTab] = useState<"overview" | "issues">("overview");

  // Lead popover
  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(leadQuery));

  const projectIssues = useMemo(
    () => allIssues.filter((i) => i.project_id === projectId),
    [allIssues, projectId],
  );

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
        router.push("/projects");
      },
    });
  }, [project, deleteProject, router]);

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

  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];

  return (
    <div className="flex h-full flex-col">
      {/* Header bar — breadcrumb */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b bg-background px-4 text-sm">
        <div className="flex items-center gap-1.5 min-w-0">
          <AppLink href="/projects" className="text-muted-foreground hover:text-foreground transition-colors shrink-0">
            {workspaceName ?? "Projects"}
          </AppLink>
          <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
          <span className="truncate">{project.title}</span>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-xs" className="text-muted-foreground shrink-0">
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              }
            />
            <DropdownMenuContent align="start" className="w-48">
              <DropdownMenuItem onClick={() => {
                navigator.clipboard.writeText(window.location.href);
                toast.success("Link copied");
              }}>
                <Link2 className="h-3.5 w-3.5" />
                Copy link
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setDeleteDialogOpen(true)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete project
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            onClick={() => {
              navigator.clipboard.writeText(window.location.href);
              toast.success("Link copied");
            }}
          >
            <Link2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex h-10 shrink-0 items-center gap-1 border-b px-1">
        <button
          type="button"
          onClick={() => setActiveTab("overview")}
          className={cn(
            "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
            activeTab === "overview"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
          )}
        >
          Overview
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("issues")}
          className={cn(
            "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
            activeTab === "issues"
              ? "bg-accent text-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
          )}
        >
          Issues
        </button>
      </div>

      {/* Tab content */}
      {activeTab === "overview" ? (
        <div className="flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-4xl px-8 py-8">
            {/* Icon — clickable to change */}
            <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="text-3xl cursor-pointer rounded-lg p-1 -ml-1 hover:bg-accent/60 transition-colors"
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

            {/* Editable title */}
            <TitleEditor
              key={`title-${projectId}`}
              defaultValue={project.title}
              placeholder="Project title"
              className="mt-3 w-full text-2xl font-bold leading-snug tracking-tight"
              onBlur={(value) => {
                const trimmed = value.trim();
                if (trimmed && trimmed !== project.title) handleUpdateField({ title: trimmed });
              }}
            />

            {/* Properties row — inline pills */}
            <div className="mt-5 flex items-center gap-4">
              <span className="text-xs font-medium text-muted-foreground shrink-0 w-20">Properties</span>
              <div className="flex items-center gap-1.5 flex-wrap">
                {/* Status */}
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <PropertyPill>
                        <span className={cn("size-2 rounded-full", statusCfg.dotColor)} />
                        <span>{statusCfg.label}</span>
                      </PropertyPill>
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

                {/* Priority */}
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <PropertyPill>
                        <PriorityIcon priority={project.priority} />
                        <span>{priorityCfg.label}</span>
                      </PropertyPill>
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

                {/* Lead */}
                <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
                  <PopoverTrigger
                    render={
                      <PropertyPill>
                        {project.lead_type && project.lead_id ? (
                          <>
                            <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={16} />
                            <span>{getActorName(project.lead_type, project.lead_id)}</span>
                          </>
                        ) : (
                          <span className="text-muted-foreground">Lead</span>
                        )}
                      </PropertyPill>
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
                              <ActorAvatar actorType="agent" actorId={a.id} size={16} />
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
              </div>
            </div>

            {/* Description */}
            <div className="mt-8">
              <h3 className="text-xs font-medium text-muted-foreground mb-2">Description</h3>
              <ContentEditor
                ref={descEditorRef}
                key={projectId}
                defaultValue={project.description || ""}
                placeholder="Add description..."
                onUpdate={(md) => handleUpdateField({ description: md || null })}
                debounceMs={1500}
              />
            </div>

          </div>
        </div>
      ) : (
        /* Issues tab — reuse existing issue list/board components */
        <ViewStoreProvider store={projectViewStore}>
          <IssuesHeader scopedIssues={projectIssues} />
          <ProjectIssuesTab projectIssues={projectIssues} />
          <BatchActionToolbar />
        </ViewStoreProvider>
      )}

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
    </div>
  );
}
