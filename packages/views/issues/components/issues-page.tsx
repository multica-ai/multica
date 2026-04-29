"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, ListTodo } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  childIssueProgressOptions,
  flattenIssueBuckets,
  issueDetailOptions,
  issueExecutionSummaryOptions,
  issueKeys,
  issueListOptions,
} from "@multica/core/issues/queries";
import { useUpdateIssue, useLoadMoreByStatus } from "@multica/core/issues/mutations";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import {
  useIssueViewStore,
  useClearFiltersOnWorkspaceChange,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useIssuesScopeStore } from "@multica/core/issues/stores/issues-scope-store";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type {
  Issue,
  IssueStatus,
  ListIssuesCache,
} from "@multica/core/types";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { PageHeader } from "../../layout/page-header";
import { useNavigation } from "../../navigation";
import { IssuesHeader } from "./issues-header";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { BatchActionToolbar } from "./batch-action-toolbar";
import { IssuePreviewPanel } from "./issue-preview-panel";
import { filterIssues } from "../utils/filter";
import { sortIssues } from "../utils/sort";

function isTextInputTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (target.isContentEditable) return true;
  if (target.closest("[contenteditable='true'], [contenteditable='plaintext-only']")) return true;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

function shouldIgnorePreviewShortcut(event: KeyboardEvent) {
  if (event.defaultPrevented) return true;
  if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return true;
  if (!(event.target instanceof HTMLElement)) return false;
  if (isTextInputTarget(event.target)) return true;
  return !!event.target.closest(
    [
      "[role='dialog']",
      "[role='menu']",
      "[role='listbox']",
      "[data-radix-popper-content-wrapper]",
      "[data-floating-ui-portal]",
    ].join(", "),
  );
}

export function IssuesPage() {
  const [isDesktopPreviewViewport, setIsDesktopPreviewViewport] = useState<boolean | null>(
    null,
  );
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();

  const scope = useIssuesScopeStore((s) => s.scope);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);
  const assigneeFilters = useIssueViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useIssueViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useIssueViewStore((s) => s.creatorFilters);
  const projectFilters = useIssueViewStore((s) => s.projectFilters);
  const includeNoProject = useIssueViewStore((s) => s.includeNoProject);
  const labelFilters = useIssueViewStore((s) => s.labelFilters);
  const sortBy = useIssueViewStore((s) => s.sortBy);
  const sortDirection = useIssueViewStore((s) => s.sortDirection);

  const { data: allIssues = [], isLoading: loading } = useQuery(issueListOptions(wsId));
  const { data: childProgressMap = new Map() } = useQuery(childIssueProgressOptions(wsId));
  const { data: executionSummaryMap = new Map() } = useQuery(
    issueExecutionSummaryOptions(wsId),
  );

  const peekIssueId = navigation.searchParams.get("peek") ?? "";
  const { data: peekIssue, error: peekIssueError } = useQuery({
    ...issueDetailOptions(wsId, peekIssueId),
    enabled: !!peekIssueId,
    retry: false,
  });

  // Clear filter state when switching between workspaces (URL-driven).
  useClearFiltersOnWorkspaceChange(useIssueViewStore, wsId);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope]);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(min-width: 1280px)");
    const updateViewport = () => setIsDesktopPreviewViewport(mediaQuery.matches);
    updateViewport();
    mediaQuery.addEventListener("change", updateViewport);
    return () => mediaQuery.removeEventListener("change", updateViewport);
  }, []);

  const buildIssuesPath = useCallback(
    (nextPeekId?: string) => {
      const params = new URLSearchParams(navigation.searchParams.toString());
      if (nextPeekId) params.set("peek", nextPeekId);
      else params.delete("peek");
      const query = params.toString();
      return query ? `${paths.issues()}?${query}` : paths.issues();
    },
    [navigation.searchParams, paths],
  );

  const openPreview = useCallback(
    (issue: Issue | string) => {
      const id = typeof issue === "string" ? issue : issue.id;
      navigation.replace(buildIssuesPath(id));
    },
    [buildIssuesPath, navigation],
  );

  const closePreview = useCallback(() => {
    navigation.replace(buildIssuesPath());
  }, [buildIssuesPath, navigation]);

  const openFullDetail = useCallback(
    (issueId: string) => {
      const fullDetailPath = paths.issueDetail(issueId);
      if (navigation.openInNewTab) {
        navigation.openInNewTab(fullDetailPath);
        return;
      }
      navigation.push(fullDetailPath);
    },
    [navigation, paths],
  );

  useEffect(() => {
    if (!(peekIssueError instanceof ApiError) || peekIssueError.status !== 404) {
      return;
    }
    closePreview();
  }, [closePreview, peekIssueError]);

  // Scope pre-filter: narrow by assignee type.
  const scopedIssues = useMemo(() => {
    if (scope === "members") {
      return allIssues.filter((issue) => issue.assignee_type === "member");
    }
    if (scope === "agents") {
      return allIssues.filter((issue) => issue.assignee_type === "agent");
    }
    return allIssues;
  }, [allIssues, scope]);

  const issues = useMemo(
    () =>
      filterIssues(scopedIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        labelFilters,
      }),
    [
      scopedIssues,
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
    ],
  );

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0) {
      return BOARD_STATUSES.filter((status) => statusFilters.includes(status));
    }
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(() => {
    return BOARD_STATUSES.filter((status) => !visibleStatuses.includes(status));
  }, [visibleStatuses]);

  const issuesByStatus = useMemo(() => {
    const map = new Map<IssueStatus, Issue[]>();
    for (const status of visibleStatuses) {
      map.set(
        status,
        sortIssues(
          issues.filter((issue) => issue.status === status),
          sortBy,
          sortDirection,
        ),
      );
    }
    return map;
  }, [issues, sortBy, sortDirection, visibleStatuses]);

  const previewStatus = peekIssue?.status ?? null;
  const currentLaneIssues = previewStatus
    ? issuesByStatus.get(previewStatus) ?? []
    : [];
  const currentLaneIds = currentLaneIssues.map((issue) => issue.id);
  const currentLaneIndex = peekIssueId
    ? currentLaneIds.indexOf(peekIssueId)
    : -1;
  const notInCurrentView =
    !!peekIssueId && (!!peekIssue || peekIssueError == null) && currentLaneIndex === -1;

  const previewLoadStatus =
    previewStatus ?? visibleStatuses[0] ?? "todo";
  const {
    loadMore: loadMorePreviewLane,
    hasMore: previewLaneHasMore,
    isLoading: previewLaneLoadingMore,
  } = useLoadMoreByStatus(previewLoadStatus);

  const applyScopeAndFilters = useCallback(
    (sourceIssues: Issue[]) => {
      const scoped = (() => {
        if (scope === "members") {
          return sourceIssues.filter((issue) => issue.assignee_type === "member");
        }
        if (scope === "agents") {
          return sourceIssues.filter((issue) => issue.assignee_type === "agent");
        }
        return sourceIssues;
      })();

      return filterIssues(scoped, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        labelFilters,
      });
    },
    [
      scope,
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
    ],
  );

  const getNavigableLaneIssues = useCallback(
    (status: IssueStatus) => {
      const cache = queryClient.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
      const sourceIssues = cache ? flattenIssueBuckets(cache) : allIssues;
      return sortIssues(
        applyScopeAndFilters(sourceIssues).filter((issue) => issue.status === status),
        sortBy,
        sortDirection,
      );
    },
    [allIssues, applyScopeAndFilters, queryClient, sortBy, sortDirection, wsId],
  );

  const navigatePreview = useCallback(
    async (direction: -1 | 1) => {
      if (!peekIssueId || !previewStatus || notInCurrentView) return;

      const laneIssues = getNavigableLaneIssues(previewStatus);
      const currentIndex = laneIssues.findIndex((issue) => issue.id === peekIssueId);
      if (currentIndex === -1) return;

      const target = laneIssues[currentIndex + direction];
      if (target) {
        openPreview(target.id);
        return;
      }

      if (
        direction > 0 &&
        !previewLaneLoadingMore &&
        previewLaneHasMore
      ) {
        await loadMorePreviewLane();
        const nextLaneIssues = getNavigableLaneIssues(previewStatus);
        const nextIndex = nextLaneIssues.findIndex((issue) => issue.id === peekIssueId);
        const nextTarget = nextLaneIssues[nextIndex + 1];
        if (nextTarget) {
          openPreview(nextTarget.id);
        }
      }
    },
    [
      getNavigableLaneIssues,
      loadMorePreviewLane,
      notInCurrentView,
      openPreview,
      peekIssueId,
      previewLaneHasMore,
      previewLaneLoadingMore,
      previewStatus,
    ],
  );

  const closePreviewRef = useRef(closePreview);
  const navigatePreviewRef = useRef(navigatePreview);

  useEffect(() => {
    closePreviewRef.current = closePreview;
    navigatePreviewRef.current = navigatePreview;
  }, [closePreview, navigatePreview]);

  useEffect(() => {
    if (!peekIssueId) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (shouldIgnorePreviewShortcut(event)) return;
      if (event.key === "Escape") {
        event.preventDefault();
        closePreviewRef.current();
        return;
      }
      if (event.key === "j" || event.key === "ArrowDown") {
        event.preventDefault();
        void navigatePreviewRef.current(1);
        return;
      }
      if (event.key === "k" || event.key === "ArrowUp") {
        event.preventDefault();
        void navigatePreviewRef.current(-1);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [peekIssueId]);

  const updateIssueMutation = useUpdateIssue();
  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const viewState = useIssueViewStore.getState();
      if (viewState.sortBy !== "position") {
        viewState.setSortBy("position");
        viewState.setSortDirection("asc");
      }

      const updates: Partial<{ status: IssueStatus; position: number }> = {
        status: newStatus,
      };
      if (newPosition !== undefined) updates.position = newPosition;

      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error("Failed to move issue") },
      );
    },
    [updateIssueMutation],
  );

  const previewSummary = peekIssueId
    ? executionSummaryMap.get(peekIssueId)
    : undefined;

  if (loading) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex h-12 shrink-0 items-center justify-between px-4">
          <div className="flex items-center gap-1">
            <Skeleton className="h-8 w-14 rounded-md" />
            <Skeleton className="h-8 w-20 rounded-md" />
            <Skeleton className="h-8 w-16 rounded-md" />
          </div>
          <div className="flex items-center gap-1">
            <Skeleton className="h-8 w-8 rounded-md" />
            <Skeleton className="h-8 w-8 rounded-md" />
            <Skeleton className="h-8 w-8 rounded-md" />
          </div>
        </div>
        {viewMode === "list" ? (
          <div className="space-y-1 overflow-y-auto p-2">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full rounded-lg" />
            ))}
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
            {Array.from({ length: 5 }).map((_, index) => (
              <div key={index} className="flex min-w-52 flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-20" />
                <Skeleton className="h-24 w-full rounded-lg" />
                <Skeleton className="h-24 w-full rounded-lg" />
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  const content = scopedIssues.length === 0 ? (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
      <ListTodo className="h-10 w-10 text-muted-foreground/40" />
      <p className="text-sm">No issues yet</p>
      <p className="text-xs">Create an issue to get started.</p>
    </div>
  ) : (
    <div className="flex min-h-0 flex-1 flex-col">
      {viewMode === "board" ? (
        <BoardView
          issues={issues}
          visibleStatuses={visibleStatuses}
          hiddenStatuses={hiddenStatuses}
          onMoveIssue={handleMoveIssue}
          childProgressMap={childProgressMap}
          executionSummaryMap={executionSummaryMap}
          selectedIssueId={peekIssueId || undefined}
          onOpenIssue={openPreview}
        />
      ) : (
        <ListView
          issues={issues}
          visibleStatuses={visibleStatuses}
          childProgressMap={childProgressMap}
          executionSummaryMap={executionSummaryMap}
          onOpenIssue={openPreview}
        />
      )}
    </div>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-1.5">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">Issues</span>
      </PageHeader>

      <ViewStoreProvider store={useIssueViewStore}>
        <IssuesHeader scopedIssues={scopedIssues} />

        <div className="flex min-h-0 flex-1">
          <div className={cn("min-w-0 flex-1 flex flex-col", peekIssueId && "xl:border-r")}>
            {content}
          </div>

          {peekIssueId && (
            <div className="hidden min-h-0 shrink-0 xl:flex xl:w-[520px] 2xl:w-[560px]">
              <IssuePreviewPanel
                issueId={peekIssueId}
                summary={previewSummary}
                notInCurrentView={notInCurrentView}
                canGoPrev={currentLaneIndex > 0}
                canGoNext={
                  currentLaneIndex !== -1 &&
                  (currentLaneIndex < currentLaneIds.length - 1 || previewLaneHasMore)
                }
                onGoPrev={() => void navigatePreview(-1)}
                onGoNext={() => void navigatePreview(1)}
                onClose={closePreview}
                onExpand={() => openFullDetail(peekIssueId)}
              />
            </div>
          )}
        </div>

        {viewMode === "list" && <BatchActionToolbar issues={issues} />}

        <Sheet
          open={!!peekIssueId && isDesktopPreviewViewport === false}
          onOpenChange={(open) => !open && closePreview()}
        >
          <SheetContent
            side="right"
            showCloseButton={false}
            className="flex w-full max-w-[560px] flex-col overflow-hidden p-0 xl:hidden"
          >
            {peekIssueId && (
              <IssuePreviewPanel
                issueId={peekIssueId}
                summary={previewSummary}
                notInCurrentView={notInCurrentView}
                canGoPrev={currentLaneIndex > 0}
                canGoNext={
                  currentLaneIndex !== -1 &&
                  (currentLaneIndex < currentLaneIds.length - 1 || previewLaneHasMore)
                }
                onGoPrev={() => void navigatePreview(-1)}
                onGoNext={() => void navigatePreview(1)}
                onClose={closePreview}
                onExpand={() => openFullDetail(peekIssueId)}
              />
            )}
          </SheetContent>
        </Sheet>
      </ViewStoreProvider>
    </div>
  );
}
