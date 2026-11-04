"use client";

import { Building2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { PAGE_GUTTER, PageHeader } from "../../layout/page-header";
import { RefreshablePageIcon } from "../../layout/refreshable-page-icon";
import { useT } from "../../i18n";
import { FilterChipsBar } from "../../issues/components/filter-chips-bar";
import { IssueDisplayControls } from "../../issues/components/issues-header";
import { WorkspaceAgentWorkingChip } from "../../issues/components/workspace-agent-working-chip";
import { OfficeCompanyState } from "./office-company-state";
import { OfficePulseStrip } from "./office-pulse-strip";
import type { IssueSurfaceController } from "../../issues/surface/use-issue-surface-controller";

/** Filters and display options of the board, without its view switcher. */
export function OfficeHeader({ controller }: { controller: IssueSurfaceController }) {
  const { t } = useT("issues");
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const toggleAgentRunningFilter = useViewStore((s) => s.toggleAgentRunningFilter);

  return (
    <>
      <PageHeader>
        <RefreshablePageIcon refreshing={controller.isRefreshing}>
          <Building2 className="size-4" />
        </RefreshablePageIcon>
        <h1 className="text-body font-medium">{t(($) => $.office.title)}</h1>
      </PageHeader>
      <div className={cn("flex min-h-12 shrink-0 items-center justify-end gap-1 py-2", PAGE_GUTTER)}>
        <OfficePulseStrip />
        <OfficeCompanyState />
        <WorkspaceAgentWorkingChip
          value={agentRunningFilter}
          onToggle={toggleAgentRunningFilter}
          agents={controller.workingAgents}
        />
        <IssueDisplayControls
          scopedIssues={controller.surfaceIssues}
          hideViewToggle
          dateFilter={dateFilter}
          onDateFilterChange={setDateFilter}
          facetCountsExact={controller.facetCountsExact}
          tableFacetCounts={controller.tableFacetCounts}
          onTableFacetChange={controller.setActiveTableFacet}
        />
      </div>
      <FilterChipsBar dateFilter={dateFilter} onDateFilterChange={setDateFilter} />
    </>
  );
}
