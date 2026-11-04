"use client";

import { cn } from "@multica/ui/lib/utils";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { PAGE_GUTTER } from "../../layout/page-header";
import { FilterChipsBar } from "../../issues/components/filter-chips-bar";
import { IssueDisplayControls } from "../../issues/components/issues-header";
import { WorkspaceAgentWorkingChip } from "../../issues/components/workspace-agent-working-chip";
import { CompanyState } from "./company-state";
import { CompanyPulseStrip } from "./company-pulse-strip";
import type { IssueSurfaceController } from "../../issues/surface/use-issue-surface-controller";

/**
 * The floor plan's toolbar: filters and display options of the board. The page
 * header above it, shared with the overview, carries the title and the tabs.
 */
export function CompanyHeader({ controller }: { controller: IssueSurfaceController }) {
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const toggleAgentRunningFilter = useViewStore((s) => s.toggleAgentRunningFilter);

  return (
    <>
      <div className={cn("flex min-h-12 shrink-0 items-center justify-end gap-1 py-2", PAGE_GUTTER)}>
        <CompanyPulseStrip />
        <CompanyState />
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
