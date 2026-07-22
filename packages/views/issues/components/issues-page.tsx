"use client";

import { ListTodo } from "lucide-react";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
} from "@multica/core/types";
import { useIssuesScopeStore } from "@multica/core/issues/stores/issues-scope-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";

function IssuesSurfaceHeader({
  issues,
  workingIssues,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  workingIssues: Issue[] | undefined;
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
      workingIssues={workingIssues}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      facetCountsExact={facetCountsExact}
      tableFacetCounts={tableFacetCounts}
      onTableFacetChange={onTableFacetChange}
    />
  );
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScopeStore((s) => s.scope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller, workingIssues }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            workingIssues={workingIssues}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.viewMode !== "table"}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_hint)}</p>
          </div>
        )}
      />
    </div>
  );
}
