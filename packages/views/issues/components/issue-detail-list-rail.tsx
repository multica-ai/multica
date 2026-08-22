"use client";

import { useQuery } from "@tanstack/react-query";
import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { useIssueDetailSplitStore } from "@multica/core/issues/stores";
import { issueListOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";

/**
 * Left list rail for the `/{ws}/issues/{id}` detail route's two-column layout.
 * Lists every workspace issue as a compact single column; the current issue is
 * highlighted and clicking a row navigates in place. The header toggle
 * collapses the rail to a narrow icon + count strip, persisted via the split
 * store.
 */
export function IssueDetailListRail({ activeIssueId }: { activeIssueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const collapsed = useIssueDetailSplitStore((s) => s.collapsed);
  const toggleCollapsed = useIssueDetailSplitStore((s) => s.toggleCollapsed);
  const { data: issues = [] } = useQuery(issueListOptions(wsId));

  if (collapsed) {
    return (
      <div
        className="flex h-full w-full flex-col items-center gap-2 py-2"
        data-testid="issue-detail-rail-collapsed"
      >
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          aria-label={t(($) => $.detail.list_rail.toggle_expand_aria)}
          onClick={toggleCollapsed}
          data-testid="issue-detail-rail-toggle"
        >
          <PanelLeftOpen />
        </Button>
        <span className="text-micro text-muted-foreground tabular-nums">
          {issues.length}
        </span>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-1.5">
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          aria-label={t(($) => $.detail.list_rail.toggle_collapse_aria)}
          onClick={toggleCollapsed}
          data-testid="issue-detail-rail-toggle"
        >
          <PanelLeftClose />
        </Button>
        <span className="pr-1 text-caption text-muted-foreground tabular-nums">
          {t(($) => $.detail.list_rail.count_badge, { count: issues.length })}
        </span>
      </div>
      <div
        className="flex-1 min-h-0 overflow-y-auto py-1"
        data-testid="issue-detail-rail-list"
      >
        {issues.map((issue) => {
          const active = issue.id === activeIssueId;
          return (
            <button
              key={issue.id}
              type="button"
              data-active={active || undefined}
              data-testid={`issue-detail-rail-row-${issue.id}`}
              onClick={() => navigation.replace(paths.issueDetail(issue.id))}
              className={cn(
                "flex w-full items-center gap-2 px-3 py-1.5 text-left text-body",
                active
                  ? "bg-surface-selected hover:bg-surface-selected"
                  : "hover:bg-surface-hover",
              )}
            >
              <StatusIcon status={issue.status} className="h-3.5 w-3.5 shrink-0" />
              <span className="w-14 shrink-0 truncate text-caption text-muted-foreground">
                {issue.identifier}
              </span>
              <span className="min-w-0 flex-1 truncate">{issue.title}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
