"use client";

import { Eye, MoreHorizontal } from "lucide-react";
import type { IssueStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";

/**
 * Single source of truth for the "Hidden columns" side panel rendered by
 * the kanban-style views (board and swimlane).
 *
 * Each consumer renders its own per-row count via the {@link renderRow} slot —
 * the board uses `useLoadMoreByStatus` to fetch the workspace-wide aggregate,
 * while the swimlane uses an in-memory total derived from already-loaded
 * issues. Centralising the chrome here keeps a future view (calendar /
 * timeline / etc.) from forking yet another copy.
 */
export function HiddenColumnsPanel({
  hiddenStatuses,
  renderRow,
}: {
  hiddenStatuses: IssueStatus[];
  renderRow: (status: IssueStatus) => React.ReactNode;
}) {
  const { t } = useT("issues");
  return (
    <div className="flex w-[240px] shrink-0 flex-col">
      <div className="mb-2 flex items-center gap-2 px-1">
        <span className="text-sm font-medium text-muted-foreground">
          {t(($) => $.board.hidden_columns_label)}
        </span>
      </div>
      <div className="flex-1 space-y-0.5">
        {hiddenStatuses.map((status) => renderRow(status))}
      </div>
    </div>
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
  total: number;
}) {
  const { t } = useT("issues");
  const viewStoreApi = useViewStoreApi();
  return (
    <div className="flex items-center justify-between rounded-lg px-2.5 py-2 hover:bg-muted/50">
      <div className="flex items-center gap-2">
        <StatusIcon status={status} className="h-3.5 w-3.5" />
        <span className="text-sm">{t(($) => $.status[status])}</span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="text-xs text-muted-foreground">{total}</span>
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
            <DropdownMenuItem
              onClick={() => viewStoreApi.getState().showStatus(status)}
            >
              <Eye className="size-3.5" />
              {t(($) => $.board.show_column)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
