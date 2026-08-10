"use client";

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { FilterX, ListTodo, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useViewStore,
  ViewStoreProvider,
} from "@multica/core/issues/stores/view-store-context";
import {
  getIssueSurfaceViewStore,
  seedIssueSurfaceViewState,
} from "@multica/core/issues/stores/surface-view-store";
import { useActiveIssueView } from "@multica/core/issue-views/use-active-view";
import { baselineFromQuery } from "@multica/core/issue-views/baseline";
import { ViewBaselineProvider, useViewBaseline } from "./view-baseline-context";
import type { IssueViewScope } from "@multica/core/issue-views/queries";
import {
  actorKindForViewVariant,
  issueScopeKey,
  myRelationForViewVariant,
  type IssueScope,
} from "@multica/core/issues/surface/scope";
import type { Issue } from "@multica/core/types";
import { BoardView } from "../components/board-view";
import { BatchActionToolbar } from "../components/batch-action-toolbar";
import { GanttView } from "../components/gantt-view";
import { IssuesHeader } from "../components/issues-header";
import { ListView } from "../components/list-view";
import { SwimLaneView } from "../components/swimlane-view";
import { TableView } from "../components/table-view";
import { useT } from "../../i18n";
import { IssueContextMenuProvider } from "../actions";
import { IssueSurfaceActionsProvider } from "./actions-context";
import { IssueSurfaceSelectionProvider } from "./selection-context";
import type { IssueCreateDefaults, IssueSurfaceProps } from "./types";
import {
  useIssueSurfaceController,
  type IssueSurfaceController,
} from "./use-issue-surface-controller";

export interface IssueSurfaceRenderContext {
  controller: IssueSurfaceController;
  issues: Issue[];
}

interface IssueSurfaceComponentProps extends IssueSurfaceProps {
  renderHeader?: (context: IssueSurfaceRenderContext) => ReactNode;
  renderEmpty?: (context: IssueSurfaceRenderContext) => ReactNode;
  renderLoading?: (context: IssueSurfaceRenderContext) => ReactNode;
  clientFilter?: (issue: Issue) => boolean;
  showClientEmpty?: (context: IssueSurfaceRenderContext) => boolean;
  batchToolbar?: "always" | "list" | "never";
  contentClassName?: string;
}

export function IssueSurface({
  scope,
  modes,
  surfaceKey,
  createDefaults,
  search,
  renderHeader,
  renderEmpty,
  renderLoading,
  clientFilter,
  showClientEmpty,
  batchToolbar = "always",
  contentClassName,
}: IssueSurfaceComponentProps) {
  const wsId = useWorkspaceId();
  // Saved views exist on workspace / my / project surfaces only.
  const viewScope: IssueViewScope | null =
    scope.type === "workspace"
      ? { scope_type: "workspace" }
      : scope.type === "my"
        ? { scope_type: "my" }
        : scope.type === "project"
          ? { scope_type: "project", scope_id: scope.projectId }
          : null;
  const { activeView } = useActiveIssueView(wsId, viewScope);

  // An open saved view swaps the surface onto its own view-preference key:
  // display/extra-filter adjustments persist per user per view, and the
  // underlying built-in surface state is never touched (no draft machinery).
  const resolvedSurfaceKey = activeView
    ? `view:${activeView.id}`
    : (surfaceKey ?? issueScopeKey(scope));
  const seedDefinition = activeView
    ? { ...activeView.query, ...activeView.display }
    : null;
  const viewBaseline = useMemo(
    () => (activeView ? baselineFromQuery(activeView.query) : null),
    [activeView],
  );
  // While a view is open, the scope axis the view captured belongs to the
  // VIEW, not to whichever tab the user stood on: workspace/project views
  // carry an assignee-type variant, my views a relation variant. The user's
  // own tab state is never touched — it is exactly where they left it when
  // the view closes (or vanishes). A variant-free view resolves to the
  // unrestricted axis value.
  const effectiveScope: IssueScope =
    activeView && (scope.type === "workspace" || scope.type === "project")
      ? { ...scope, actorKind: actorKindForViewVariant(activeView.scope_variant) }
      : activeView && scope.type === "my"
        ? { ...scope, relation: myRelationForViewVariant(activeView.scope_variant) }
        : scope;
  const store = useMemo(() => {
    // First-open seeding happens HERE, at store-creation time for this key:
    // no React component has subscribed to the new key's store yet, so the
    // setState inside cannot cascade into an in-render update loop (it did
    // when this ran in the component body).
    if (seedDefinition) {
      seedIssueSurfaceViewState(resolvedSurfaceKey, seedDefinition);
    }
    return getIssueSurfaceViewStore(resolvedSurfaceKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- seed identity follows the key
  }, [resolvedSurfaceKey]);

  // Every change of this key tears down and remounts the ENTIRE surface
  // (providers, DnD, all columns/cards) — by design for data-window changes,
  // but expensive enough that unexpected flips are performance bugs. Dev-only
  // breadcrumb so a Performance trace showing double mounts can be tied to
  // the exact key transition.
  const contentKey = `${wsId}:${issueScopeKey(effectiveScope)}:${activeView ? `view:${activeView.id}` : "default"}`;
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") {
      console.warn(`[issue-surface] mount ${contentKey}`);
    }
  }, [contentKey]);

  return (
    <ViewStoreProvider store={store}>
      <ViewBaselineProvider baseline={viewBaseline}>
      {/* Remount on data-window change: the list queries keep the previous
          key's data as a placeholder (keepPreviousData) so sort/filter
          changes within ONE surface never flash a skeleton — but reusing the
          mounted observer across windows made project A's cards impersonate
          project B (with isLoading=false, so no skeleton either) until B's
          fetch landed. A window-keyed remount gives the new window a fresh
          observer: cold window → skeleton, warm window → instant cache hit.
          The window identity is wsId + scope — wsId is required because the
          workspace layout does not remount on workspace switch and two
          workspaces share the same scope key (e.g. "workspace:all"). Keyed
          by data identity, not surfaceKey (view-preference identity). */}
      <IssueSurfaceContent
        key={contentKey}
        scope={effectiveScope}
        modes={modes}
        createDefaults={createDefaults}
        search={search}
        renderHeader={renderHeader}
        renderEmpty={renderEmpty}
        renderLoading={renderLoading}
        clientFilter={clientFilter}
        showClientEmpty={showClientEmpty}
        batchToolbar={batchToolbar}
        contentClassName={contentClassName}
      />
      </ViewBaselineProvider>
    </ViewStoreProvider>
  );
}

function IssueSurfaceContent({
  scope,
  modes,
  createDefaults,
  search,
  renderHeader,
  renderEmpty,
  renderLoading,
  clientFilter,
  showClientEmpty,
  batchToolbar,
  contentClassName,
}: Omit<IssueSurfaceComponentProps, "surfaceKey">) {
  const { t } = useT("projects");
  const controller = useIssueSurfaceController({
    scope,
    modes,
    createDefaults,
    search,
  });
  const [tableLoadedIssues, setTableLoadedIssues] = useState<Issue[]>([]);
  const handleTableLoadedIssuesChange = useCallback((next: Issue[]) => {
    setTableLoadedIssues((current) =>
      current.length === next.length &&
      current.every((issue, index) => issue === next[index])
        ? current
        : next,
    );
  }, []);
  useEffect(() => {
    if (controller.viewMode !== "table") setTableLoadedIssues([]);
  }, [controller.viewMode]);
  const issues = useMemo(
    () =>
      clientFilter
        ? controller.issues.filter((issue) => clientFilter(issue))
        : controller.issues,
    [clientFilter, controller.issues],
  );
  const swimlaneIssues = useMemo(
    () =>
      clientFilter
        ? controller.swimlaneIssues.filter((issue) => clientFilter(issue))
        : controller.swimlaneIssues,
    [clientFilter, controller.swimlaneIssues],
  );
  const renderContext = useMemo(
    () => ({ controller, issues }),
    [controller, issues],
  );
  const openCreateIssue = useCallback(
    (defaults?: IssueCreateDefaults) => {
      controller.openCreateIssue(defaults);
    },
    [controller],
  );
  const shouldShowClientEmpty =
    !!clientFilter &&
    issues.length === 0 &&
    (showClientEmpty ? showClientEmpty(renderContext) : true);
  const shouldShowBatchToolbar =
    batchToolbar !== "never" &&
    (batchToolbar === "always" ||
      controller.viewMode === "list" ||
      controller.viewMode === "table");

  return (
    <IssueSurfaceActionsProvider actions={controller.actions}>
      {/* One shared right-click menu for every card/row this surface renders
          — see IssueContextMenuProvider. Inside the actions provider so the
          singleton's useIssueActions routes updates through surface
          actions. */}
      <IssueContextMenuProvider>
      <IssueSurfaceSelectionProvider selection={controller.selection}>
        {renderHeader ? (
          renderHeader(renderContext)
        ) : (
          <IssuesHeader
            scopedIssues={controller.surfaceIssues}
            workingAgents={controller.workingAgents}
            allowGantt={controller.allowGantt}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={
              controller.facetCountsExact
            }
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
            saveViewScope={
              scope.type === "project"
                ? { kind: "project", projectId: scope.projectId }
                : scope.type === "workspace"
                  ? { kind: "workspace" }
                  : null
            }
          />
        )}
        {controller.isLoading ? (
          renderLoading ? (
            renderLoading(renderContext)
          ) : (
            <IssueSurfaceSkeleton mode={controller.viewMode} />
          )
        ) : controller.isEmpty || shouldShowClientEmpty ? (
          // A filtered-empty surface is NOT an empty surface. Claiming "no
          // issues here yet" and offering to create one is wrong when the rows
          // exist and a filter is hiding them — and it is the state the
          // agents-working chip drops you into most often (MUL-5525). This
          // branch precedes `renderEmpty` on purpose: every surface's own empty
          // copy describes the unfiltered case.
          controller.hasActiveFilters ? (
            <FilteredEmptyState />
          ) : renderEmpty ? (
            renderEmpty(renderContext)
          ) : (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-faint-foreground" />
              <p className="text-body">{t(($) => $.detail.empty_issues_title)}</p>
              <p className="text-caption">{t(($) => $.detail.empty_issues_hint)}</p>
              <Button
                variant="outline"
                size="sm"
                className="mt-1"
                onClick={() => controller.openCreateIssue()}
              >
                <Plus className="size-3.5 mr-1.5" />
                {t(($) => $.detail.empty_issues_new_button)}
              </Button>
            </div>
          )
        ) : (
          <div className={cn("flex flex-col flex-1 min-h-0", contentClassName)}>
            {controller.viewMode === "board" && (
              <BoardView
                issues={issues}
                visibleStatuses={controller.visibleStatuses}
                hiddenStatuses={controller.hiddenStatuses}
                onMoveIssue={controller.moveIssue}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                projectId={controller.projectId}
                onCreateIssue={openCreateIssue}
                statusPagination={controller.statusPagination}
                groupBranches={controller.groupBranches}
              />
            )}
            {controller.viewMode === "list" && (
              <ListView
                issues={issues}
                visibleStatuses={controller.visibleStatuses}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                projectId={controller.projectId}
                onMoveIssue={controller.moveIssue}
                onCreateIssue={openCreateIssue}
                statusPagination={controller.statusPagination!}
              />
            )}
            {controller.viewMode === "table" && (
              <TableView
                serverQuery={controller.tableQuerySpec}
                childProgressMap={controller.childProgressMap}
                search={controller.tableSearch}
                onSearchChange={controller.setTableSearch}
                onLoadedIssuesChange={handleTableLoadedIssuesChange}
                onCreateIssue={openCreateIssue}
                exportIssues={controller.exportTableIssues}
                resolveExportLookups={controller.resolveTableExportLookups}
              />
            )}
            {controller.viewMode === "gantt" && (
              <GanttView issues={controller.filteredGanttIssues} />
            )}
            {controller.viewMode === "swimlane" && (
              <SwimLaneView
                issues={issues}
                unfilteredIssues={swimlaneIssues}
                activeFilters={controller.activeFilters}
                visibleStatuses={controller.visibleStatuses}
                hiddenStatuses={controller.hiddenStatuses}
                onMoveIssue={controller.moveIssue}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                projectId={controller.projectId}
                onCreateIssue={openCreateIssue}
                groupBranches={controller.groupBranches}
              />
            )}
          </div>
        )}
        {shouldShowBatchToolbar && (
          <BatchActionToolbar
            issues={
              controller.viewMode === "table" ? tableLoadedIssues : issues
            }
          />
        )}
      </IssueSurfaceSelectionProvider>
      </IssueContextMenuProvider>
    </IssueSurfaceActionsProvider>
  );
}

/**
 * Shown when the surface has rows but the active filters match none of them.
 * Shared by every surface, so no caller has to remember that its own empty
 * copy only describes the unfiltered case. The action clears exactly the
 * filters this state tests for, so it always restores content.
 */
function FilteredEmptyState() {
  const { t } = useT("issues");
  const clearFilters = useViewStore((s) => s.clearFilters);
  const resetFiltersTo = useViewStore((s) => s.resetFiltersTo);
  // Inside a saved view "clear" returns to the view's own conditions — the
  // baseline is not clearable from here (only the edit dialog changes it).
  const baseline = useViewBaseline();
  const handleClear = baseline
    ? () => resetFiltersTo(baseline.raw)
    : clearFilters;

  return (
    <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground">
      <FilterX className="h-10 w-10 text-faint-foreground" />
      <p className="text-body">{t(($) => $.filtered_empty.title)}</p>
      <p className="text-caption">{t(($) => $.filtered_empty.hint)}</p>
      <Button variant="outline" size="sm" className="mt-1" onClick={handleClear}>
        {t(($) => $.filtered_empty.clear_button)}
      </Button>
    </div>
  );
}

// Table is deliberately absent. It owns its own placeholders, drawn as rows
// inside its real grid so the header, column widths and toolbar are up before
// any data is — a surface-level stand-in would replace all of that with bars
// of a different shape and then jump when the rows arrived.
function IssueSurfaceSkeleton({ mode }: { mode: string }) {
  if (mode === "list") {
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex min-w-52 flex-1 flex-col gap-2">
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-24 w-full rounded-lg" />
          <Skeleton className="h-24 w-full rounded-lg" />
        </div>
      ))}
    </div>
  );
}
