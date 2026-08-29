"use client";

import { useCallback } from "react";
import { ListTodo } from "lucide-react";
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
import {
  LifeOSFocusStrip,
  lifeOSFocusForIssue,
  useLifeOSFocus,
} from "./lifeos-focus-strip";

function IssuesSurfaceHeader({
  issues,
  workingAgents,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
  focus,
  onFocusChange,
}: {
  issues: Issue[];
  workingAgents: WorkingAgentSummary[] | undefined;
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
  focus: ReturnType<typeof useLifeOSFocus>[0];
  onFocusChange: ReturnType<typeof useLifeOSFocus>[1];
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
        dateFilter={dateFilter}
        onDateFilterChange={setDateFilter}
        facetCountsExact={facetCountsExact}
        tableFacetCounts={tableFacetCounts}
        onTableFacetChange={onTableFacetChange}
      />
      <LifeOSFocusStrip
        issues={issues}
        value={focus}
        onChange={onFocusChange}
      />
    </>
  );
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScope("issues");
  const [focus, setFocus] = useLifeOSFocus();
  const focusFilter = useCallback(
    (issue: Issue) =>
      focus === "all" || lifeOSFocusForIssue(issue) === focus,
    [focus],
  );

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        clientFilter={focusFilter}
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            workingAgents={controller.workingAgents}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
            focus={focus}
            onFocusChange={setFocus}
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
