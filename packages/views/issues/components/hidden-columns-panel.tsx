"use client";

import { Eye, Filter, MoreHorizontal } from "lucide-react";
import type { IssueStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useStatusLabel } from "../utils/status-label";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useWorkflowPermissions } from "../../workflows/permissions";

/**
 * Single source of truth for the "Hidden columns" side panel rendered by
 * the kanban-style views (board and swimlane).
 *
 * Each consumer renders its own per-row count via the {@link renderRow} slot —
 * the board reads the server status facet's exact total, while the swimlane
 * uses an in-memory total derived from already-loaded issues. Centralising
 * the chrome here keeps a future view (calendar / timeline / etc.) from
 * forking yet another copy.
 */
export function HiddenColumnsPanel({
  hiddenStatuses,
  renderRow,
}: {
  hiddenStatuses: IssueStatus[];
  renderRow: (status: IssueStatus) => React.ReactNode;
}) {
  const { t } = useT("issues");
  const viewerHidden = useViewStore((s) => s.hiddenStatuses);
  const outsideWorkflow = hiddenStatuses.some((status) => !viewerHidden.includes(status));
  return (
    <div className="flex w-[240px] shrink-0 flex-col">
      <div className="mb-2 flex items-center gap-2 px-1">
        <span className="text-body font-medium text-muted-foreground">
          {t(($) => $.board.hidden_columns_label)}
        </span>
      </div>
      <div className="flex-1 space-y-0.5">
        {hiddenStatuses.map((status) => renderRow(status))}
      </div>
      {outsideWorkflow && <WorkflowColumnsNote />}
    </div>
  );
}

/** Where a board's columns come from, under the rows its workflow leaves out. */
function WorkflowColumnsNote() {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { isAdmin } = useWorkflowPermissions();
  return (
    <p className="mt-3 px-2.5 text-caption leading-5 text-muted-foreground">
      {t(($) => $.board.workflow_columns_note)}
      {isAdmin && (
        <>
          {" "}
          <AppLink href={`${paths.settings()}?tab=workflows`} className="text-brand hover:underline">
            {t(($) => $.workflows.project.manage)}
          </AppLink>
        </>
      )}
    </p>
  );
}

/**
 * One row inside {@link HiddenColumnsPanel}. Pure presentational — the caller
 * computes `total` however it likes (cached query vs. in-memory aggregate).
 */
export function HiddenColumnRow({
  status,
  total,
}: {
  status: IssueStatus;
  total?: number;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const viewStoreApi = useViewStoreApi();
  // Not hidden by the viewer: the column is outside the workflow this view
  // follows (MUL-7420), so filtering by the status is how to see it.
  const outsideWorkflow = !useViewStore((s) => s.hiddenStatuses.includes(status));
  return (
    <div className="flex items-center justify-between rounded-lg px-2.5 py-2 hover:bg-muted/50">
      <div className="flex min-w-0 items-center gap-2">
        <StatusIcon category={categoryOf(status)} color={colorOf(status)} icon={iconOf(status)} status={status} className="h-3.5 w-3.5 shrink-0" />
        <span className="min-w-0">
          <span className="block truncate text-body">{labelOf(status)}</span>
          {outsideWorkflow && (
            <span className="block truncate text-caption text-muted-foreground">{t(($) => $.board.outside_workflow)}</span>
          )}
        </span>
      </div>
      <div className="flex items-center gap-1.5">
        {total !== undefined && (
          <span className="text-caption text-muted-foreground">{total}</span>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.board.show_column)}
                className="rounded-full text-muted-foreground"
              >
                <MoreHorizontal className="size-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            {outsideWorkflow ? (
              <DropdownMenuItem onClick={() => viewStoreApi.getState().toggleStatusFilter(status)}>
                <Filter className="size-3.5" />
                {t(($) => $.board.filter_to_status)}
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem
                onClick={() => viewStoreApi.getState().showStatus(status)}
              >
                <Eye className="size-3.5" />
                {t(($) => $.board.show_column)}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
