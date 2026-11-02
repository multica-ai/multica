"use client";

import { ListTodo } from "@multica/ui/icons";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  WorkingAgentSummary,
} from "@multica/core/types";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { PageHeader } from "../../layout/page-header";
import { RefreshablePageIcon } from "../../layout/refreshable-page-icon";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";

function IssuesSurfaceHeader({
  issues,
  workingAgents,
  tipAnchorScopeIssues,
  allowDag,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  workingAgents: WorkingAgentSummary[] | undefined;
  tipAnchorScopeIssues: readonly Pick<Issue, "id" | "identifier">[];
  allowDag?: boolean;
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const { t } = useT("issues");
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);

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
        tipAnchorScopeIssues={tipAnchorScopeIssues}
        allowDag={allowDag}
        dateFilter={dateFilter}
        onDateFilterChange={setDateFilter}
        facetCountsExact={facetCountsExact}
        tableFacetCounts={tableFacetCounts}
        onTableFacetChange={onTableFacetChange}
      />
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
        modes={["board", "list", "table", "swimlane", "dag"]}
        batchToolbar="list"
        renderHeader={({ controller, summaryScopeIssues }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            workingAgents={controller.workingAgents}
            tipAnchorScopeIssues={summaryScopeIssues}
            allowDag={controller.allowDag}
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
