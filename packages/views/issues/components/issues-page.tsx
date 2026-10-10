"use client";

import { ListTodo } from "lucide-react";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  WorkingAgentSummary,
} from "@multica/core/types";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { STATUS_ORDER } from "@multica/core/issues/config";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { PageHeader } from "../../layout/page-header";
import { RefreshablePageIcon } from "../../layout/refreshable-page-icon";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";

function IssuesSurfaceHeader({
  issues,
  workingAgents,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  workingAgents: WorkingAgentSummary[] | undefined;
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const statusCatalog = useIssueStatuses(wsId);
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);
  const statusFacet = tableFacetCounts?.facets.find((value) => value.kind === "status");

  return (
    <>
      <PageHeader>
        <RefreshablePageIcon refreshing={isRefreshing}>
          <ListTodo className="size-4" />
        </RefreshablePageIcon>
        <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>
      <IssuesHeader
        scopedIssues={issues}
        workingAgents={workingAgents}
        dateFilter={dateFilter}
        onDateFilterChange={setDateFilter}
        facetCountsExact={facetCountsExact}
        tableFacetCounts={tableFacetCounts}
        onTableFacetChange={onTableFacetChange}
      />
      {statusFacet && (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-1 px-4 pb-2 text-caption text-muted-foreground md:px-6">
          <span>
            {t(($) => $.status_category.total)}{" "}
            <span className="font-medium text-foreground">
              {statusFacet.values.reduce((total, value) => total + value.count, 0)}
            </span>
          </span>
          {STATUS_ORDER.map((category) => {
            const count = statusFacet.values.reduce((total, value) => (
              statusCatalog.categoryOf(value.key) === category ? total + value.count : total
            ), 0);
            return (
              <span key={category}>
                {t(($) => $.status_category[category])} <span className="font-medium text-foreground">{count}</span>
              </span>
            );
          })}
          <span>
            {t(($) => $.status_category.cancelled)}{" "}
            <span className="font-medium text-foreground">
              {statusFacet.values.reduce((total, value) => (
                value.key === "cancelled" ? total + value.count : total
              ), 0)}
            </span>
          </span>
        </div>
      )}
    </>
  );
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScope("issues");

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            workingAgents={controller.workingAgents}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-faint-foreground" />
            <p className="text-body">{t(($) => $.page.empty_title)}</p>
          </div>
        )}
      />
    </div>
  );
}
