"use client";

import { useCallback, useEffect, useMemo } from "react";
import type { StoreApi } from "zustand";
import { toast } from "sonner";
import { ChevronRight, ListTodo } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useIssueMutations } from "@/features/issues/mutations";
import { useIssueStore } from "@/features/issues/store";
import {
  registerViewStoreForWorkspaceSync,
  type IssueViewState,
} from "@/features/issues/stores/view-store";
import {
  ViewStoreProvider,
  useViewStore,
  useViewStoreApi,
} from "@/features/issues/stores/view-store-context";
import { useIssuesScopeStore } from "@/features/issues/stores/issues-scope-store";
import { filterIssues } from "@/features/issues/utils/filter";
import { BOARD_STATUSES } from "@/features/issues/config";
import { useWorkspaceStore, WorkspaceAvatar } from "@/features/workspace";
import type { Issue, IssueStatus } from "@/shared/types";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { IssuesHeader } from "./issues-header";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { BatchActionToolbar } from "./batch-action-toolbar";
import { IssueTaskStatusSync } from "./issue-task-status-sync";

interface WorkbenchIssuesPageProps {
  breadcrumbLabel: string;
  emptyTitle: string;
  emptyDescription: string;
  store: StoreApi<IssueViewState>;
  deriveIssues: (issues: Issue[]) => Issue[];
  forcedViewMode?: "board" | "list";
  hideViewToggle?: boolean;
  createIssueData?: Record<string, unknown> | null;
}

interface WorkbenchIssuesPageContentProps extends WorkbenchIssuesPageProps {}

function WorkbenchIssuesPageContent({
  breadcrumbLabel,
  emptyTitle,
  emptyDescription,
  deriveIssues,
  forcedViewMode,
  hideViewToggle,
  createIssueData,
}: WorkbenchIssuesPageContentProps) {
  const allIssues = useIssueStore((state) => state.issues);
  const loading = useIssueStore((state) => state.loading);
  const workspace = useWorkspaceStore((state) => state.workspace);
  const scope = useIssuesScopeStore((state) => state.scope);
  const viewStoreApi = useViewStoreApi();
  const { updateIssue } = useIssueMutations();

  const storeViewMode = useViewStore((state) => state.viewMode);
  const setViewMode = useViewStore((state) => state.setViewMode);
  const statusFilters = useViewStore((state) => state.statusFilters);
  const priorityFilters = useViewStore((state) => state.priorityFilters);
  const assigneeFilters = useViewStore((state) => state.assigneeFilters);
  const includeNoAssignee = useViewStore((state) => state.includeNoAssignee);
  const creatorFilters = useViewStore((state) => state.creatorFilters);
  const viewMode = forcedViewMode ?? storeViewMode;

  useEffect(() => {
    if (forcedViewMode && storeViewMode !== forcedViewMode) {
      setViewMode(forcedViewMode);
    }
  }, [forcedViewMode, setViewMode, storeViewMode]);

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [viewMode, scope]);

  const derivedIssues = useMemo(() => deriveIssues(allIssues), [allIssues, deriveIssues]);

  const scopedIssues = useMemo(() => {
    if (scope === "members") {
      return derivedIssues.filter((issue) => issue.assignee_type === "member");
    }

    if (scope === "agents") {
      return derivedIssues.filter((issue) => issue.assignee_type === "agent");
    }

    return derivedIssues;
  }, [derivedIssues, scope]);

  const issues = useMemo(
    () =>
      filterIssues(scopedIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
      }),
    [
      scopedIssues,
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
    ],
  );

  const statusesWithIssues = useMemo(
    () => BOARD_STATUSES.filter((status) => scopedIssues.some((issue) => issue.status === status)),
    [scopedIssues],
  );

  const visibleStatuses = useMemo<IssueStatus[]>(() => {
    const fallbackStatuses: IssueStatus[] =
      statusesWithIssues.length > 0 ? statusesWithIssues : ["backlog"];

    if (statusFilters.length > 0) {
      const filteredStatuses = BOARD_STATUSES.filter((status) => statusFilters.includes(status));
      return filteredStatuses.length > 0 ? filteredStatuses : fallbackStatuses;
    }

    return fallbackStatuses;
  }, [statusFilters, statusesWithIssues]);

  const hiddenStatuses = useMemo(
    () => statusesWithIssues.filter((status) => !visibleStatuses.includes(status)),
    [statusesWithIssues, visibleStatuses],
  );

  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const viewState = viewStoreApi.getState();
      if (viewState.sortBy !== "position") {
        viewState.setSortBy("position");
        viewState.setSortDirection("asc");
      }

      const updates: Partial<{ status: IssueStatus; position: number }> = {
        status: newStatus,
      };
      if (newPosition !== undefined) updates.position = newPosition;

      void updateIssue(issueId, updates).catch(() => {
        toast.error("Failed to move issue");
      });
    },
    [updateIssue, viewStoreApi],
  );

  if (loading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-8 w-24" />
        </div>
        <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
          {Array.from({ length: 3 }).map((_, index) => (
            <div key={index} className="flex min-w-52 flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-24 w-full rounded-lg" />
              <Skeleton className="h-24 w-full rounded-lg" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueTaskStatusSync />

      <div className="border-b px-4 py-4 md:hidden">
        <h1 className="text-base font-semibold">{breadcrumbLabel}</h1>
        <p className="mt-1 text-xs text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </p>
      </div>

      <div className="hidden h-12 shrink-0 items-center gap-1.5 border-b px-4 md:flex">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">{breadcrumbLabel}</span>
      </div>

      <IssuesHeader
        scopedIssues={scopedIssues}
        hideViewToggle={hideViewToggle || !!forcedViewMode}
      />

      {scopedIssues.length === 0 ? (
        <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
          <ListTodo className="h-10 w-10 text-muted-foreground/40" />
          <p className="text-sm">{emptyTitle}</p>
          <p className="text-xs">{emptyDescription}</p>
        </div>
      ) : (
        <div className="flex flex-col flex-1 min-h-0">
          {viewMode === "board" ? (
            <BoardView
              issues={issues}
              allIssues={scopedIssues}
              visibleStatuses={visibleStatuses}
              hiddenStatuses={hiddenStatuses}
              onMoveIssue={handleMoveIssue}
              createIssueData={createIssueData}
            />
          ) : (
            <ListView issues={issues} visibleStatuses={visibleStatuses} />
          )}
        </div>
      )}

      {viewMode === "list" && !forcedViewMode && <BatchActionToolbar />}
    </div>
  );
}

export function WorkbenchIssuesPage({ store, ...props }: WorkbenchIssuesPageProps) {
  useEffect(() => {
    registerViewStoreForWorkspaceSync(store);
  }, [store]);

  return (
    <ViewStoreProvider store={store}>
      <WorkbenchIssuesPageContent store={store} {...props} />
    </ViewStoreProvider>
  );
}