"use client";

import { useMemo } from "react";
import { useStore } from "zustand";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { Issue } from "@multica/core/types";
import { myIssuesViewStore, type MyIssuesScope } from "@multica/core/issues/stores/my-issues-view-store";
import { useT } from "../../i18n";
import { WorkspaceAgentWorkingChip } from "../../issues/components/workspace-agent-working-chip";
import { IssueDisplayControls } from "../../issues/components/issues-header";

export function MyIssuesHeader({ allIssues }: { allIssues: Issue[] }) {
  const { t } = useT("my-issues");
  const { t: tIssues } = useT("issues");
  const SCOPES: { value: MyIssuesScope; label: string; description: string }[] = [
    { value: "all", label: t(($) => $.header.scope.all_label), description: t(($) => $.header.scope.all_description) },
    { value: "assigned", label: t(($) => $.header.scope.assigned_label), description: t(($) => $.header.scope.assigned_description) },
    { value: "created", label: t(($) => $.header.scope.created_label), description: t(($) => $.header.scope.created_description) },
    { value: "agents", label: t(($) => $.header.scope.agents_label), description: t(($) => $.header.scope.agents_description) },
  ];
  const scope = useStore(myIssuesViewStore, (s) => s.scope);
  const agentRunningFilter = useStore(myIssuesViewStore, (s) => s.agentRunningFilter);
  const act = myIssuesViewStore.getState();
  const scopedIssueIds = useMemo(
    () => new Set(allIssues.map((i) => i.id)),
    [allIssues],
  );

  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-4">
      <div className="flex items-center gap-1">
        {SCOPES.map((s) => (
          <Tooltip key={s.value}>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={
                    scope === s.value
                      ? "bg-accent text-accent-foreground hover:bg-accent/80"
                      : "text-muted-foreground"
                  }
                  onClick={() => act.setScope(s.value)}
                >
                  {s.label}
                </Button>
              }
            />
            <TooltipContent side="bottom">{s.description}</TooltipContent>
          </Tooltip>
        ))}
      </div>

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
