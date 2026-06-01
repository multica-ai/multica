"use client";

import { useMemo } from "react";
import { useStore } from "zustand";
import type { Issue, SavedView, ViewFilters } from "@multica/core/types";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { useT } from "../../i18n";
import { WorkspaceAgentWorkingChip } from "../../issues/components/workspace-agent-working-chip";
import { IssueDisplayControls } from "../../issues/components/issues-header";
import { ViewTabs } from "../../views/view-tabs";

export function MyIssuesHeader({
  allIssues,
  currentViewId,
  onSelectView,
  currentFilters,
}: {
  allIssues: Issue[];
  currentViewId: string | null;
  onSelectView: (view: SavedView | null) => void;
  currentFilters?: ViewFilters;
}) {
  const { t } = useT("my-issues");
  const { t: tIssues } = useT("issues");
  const agentRunningFilter = useStore(myIssuesViewStore, (s) => s.agentRunningFilter);
  const act = myIssuesViewStore.getState();
  const scopedIssueIds = useMemo(
    () => new Set(allIssues.map((i) => i.id)),
    [allIssues],
  );

  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-4">
      <ViewTabs
        page="my_issues"
        currentViewId={currentViewId}
        onSelectView={onSelectView}
        currentFilters={currentFilters}
        resolveDefaultName={(nameKey) =>
          t(($) => $.views.defaults[nameKey as keyof typeof $.views.defaults] ?? nameKey)
        }
      />

      <div className="flex items-center gap-1">
        {agentRunningFilter && (
          <span className="mr-1 text-xs text-muted-foreground">
            {tIssues(($) => $.agent_activity.filter_active_label)}
          </span>
        )}
        <WorkspaceAgentWorkingChip
          value={agentRunningFilter}
          onToggle={act.toggleAgentRunningFilter}
          scopedIssueIds={scopedIssueIds}
        />
        <IssueDisplayControls scopedIssues={allIssues} />
      </div>
    </div>
  );
}
