"use client";

// CEREBRO-PATCH(issue-detail-status-submenu-component): FIR-1550 v2b — net-new
// cerebro component. It lives in the upstream views zone (not a cerebro-*
// package) because it reuses the local StatusIcon; cerebro-status-models cannot
// import @multica/views without a dependency cycle. Marked + documented per the
// Cerebro Extension Discipline so the upstream-zone-guard stays green.
//
// Cerebro workflow v2b (FIR-1550) — model-aware Status submenu for the issue
// detail 3-dot ("more") menu.
//
// The upstream issue-detail 3-dot menu hardcoded ALL_STATUSES, so it kept
// showing the 7 base statuses even when the issue's project had a status model
// assigned — the one status entry point that still ignored the model (the
// board action menu and the shared StatusPicker were already model-aware).
//
// This component renders inside CerebroStatusModelProvider (mounted by
// issue-detail around both detail content and sidebar), so useStatusMenuRows
// resolves the project's custom statuses. Picking a custom row sends
// custom_status_key alongside the base status so the backend pins the sidecar
// — identical to issue-actions-menu-items.tsx, keeping every status surface in
// agreement.

import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useStatusMenuRows } from "@multica/cerebro-status-models/views";
import { StatusIcon } from "./status-icon";

export function CerebroIssueDetailStatusSubmenu({
  issue,
  onUpdate,
}: {
  issue: Issue;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
}) {
  const statusRows = useStatusMenuRows(issue.status, issue.custom_status?.custom_status_key);

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
        Status
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent>
        {statusRows.map((row) => (
          <DropdownMenuItem
            key={row.id}
            onClick={() =>
              onUpdate(
                row.customStatusKey
                  ? { status: row.baseStatus, custom_status_key: row.customStatusKey }
                  : { status: row.baseStatus },
              )
            }
          >
            <span style={row.color ? { color: row.color } : undefined}>
              <StatusIcon
                status={row.baseStatus}
                className="h-3.5 w-3.5"
                inheritColor={!!row.color}
              />
            </span>
            {row.label ?? STATUS_CONFIG[row.baseStatus].label}
            {row.selected && (
              <span className="ml-auto text-xs text-muted-foreground">✓</span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
