"use client";

import { Archive, ListTodo } from "lucide-react";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
} from "@multica/core/types";
import { useIssuesScopeStore } from "@multica/core/issues/stores/issues-scope-store";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";
import { Button } from "@multica/ui/components/ui/button";

function IssuesSurfaceHeader({
  issues,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);

  return (
    <IssuesHeader
      scopedIssues={issues}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      facetCountsExact={facetCountsExact}
      tableFacetCounts={tableFacetCounts}
      onTableFacetChange={onTableFacetChange}
    />
  );
}

function archivedCountFromFacets(
  tableFacetCounts: IssueTableFacetsResponse | undefined,
): number | undefined {
  const statusFacet = tableFacetCounts?.facets.find((f) => f.kind === "status");
  return statusFacet?.values.find((v) => v.key === "archived")?.count;
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScopeStore((s) => s.scope);
  const act = useViewStoreApi().getState();

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={({ controller }) => {
          const archivedCount = archivedCountFromFacets(controller.tableFacetCounts) ?? 0;
          const showArchivedHint = archivedCount > 0;
          return (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-body">{t(($) => $.page.empty_title)}</p>
              <p className="text-caption">{t(($) => $.page.empty_hint)}</p>
              {showArchivedHint && (
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-1 gap-1.5 text-caption"
                  onClick={() => act.setStatusFilters(["archived"])}
                >
                  <Archive className="size-3.5" />
                  {t(($) => $.archived.empty_state_hint, { count: archivedCount })}
                </Button>
              )}
            </div>
          );
        }}
      />
    </div>
  );
}
