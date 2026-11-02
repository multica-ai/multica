"use client";

// Labrastro fork (OL-1030): folder-tree row and batch actions for the agents
// list. Upstream `agent-row-actions.tsx` and `agent-batch-toolbar.tsx` mount
// these with one line each and carry only the optional `treeActions` prop.

import { FolderInput } from "@multica/ui/icons";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { AgentListRow } from "./agents-page";

/** Folder-tree extras handed to the row kebab and the batch toolbar. Present
 *  when the workspace folder tree loaded; absent callers keep the pre-tree
 *  flat behavior. */
export interface AgentTreeActions {
  /** Fallback for flat rows and mixed visible batch selections. Row menus
   *  override this to "place" for a direct squad-folder parent. */
  defaultVariant: "move" | "place";
  squadFolderIds?: ReadonlySet<string>;
  /** Open the move dialog for the given agents. */
  onMove: (agents: Agent[], variant: "move" | "place") => void;
}

/** Row kebab item: move (or "also place" inside a squad folder). Moving
 *  requires the agent's owner or a workspace owner/admin — the same gate as
 *  the other management actions (`canManage`). */
export function AgentTreeRowMenuItems({
  agent,
  canManage,
  treeActions,
  parentFolderId,
}: {
  agent: Agent;
  canManage: boolean;
  treeActions: AgentTreeActions | undefined;
  parentFolderId?: string | null;
}) {
  const { t } = useT("agent-folders");
  if (!treeActions || !canManage || agent.archived_at) return null;
  const variant = parentFolderId && treeActions.squadFolderIds?.has(parentFolderId)
    ? "place"
    : treeActions.defaultVariant;
  return (
    <DropdownMenuItem onClick={() => treeActions.onMove([agent], variant)}>
      <FolderInput className="size-3.5" />
      {variant === "place"
        ? t(($) => $.move_agents.menu_place)
        : t(($) => $.move_agents.menu_move)}
    </DropdownMenuItem>
  );
}

/** Batch toolbar move button; disabled with a reason tooltip when the
 *  selection contains agents the caller may not move. Visible selections can
 *  span several folders, so batch operations always use the move wording. */
export function AgentTreeBatchMoveAction({
  rows,
  treeActions,
}: {
  rows: AgentListRow[];
  treeActions: AgentTreeActions | undefined;
}) {
  const { t } = useT("agent-folders");
  if (!treeActions) return null;

  const variant = treeActions.defaultVariant;
  const allMovable = rows.every((row) => row.canManage);
  const moveButton = (
    <Button
      variant="ghost"
      size="sm"
      disabled={!allMovable}
      onClick={() =>
        treeActions.onMove(
          rows.map((row) => row.agent),
          variant,
        )
      }
      className={cn(!allMovable && "pointer-events-none")}
    >
      <FolderInput className="mr-1 size-3.5" />
      {variant === "place"
        ? t(($) => $.move_agents.batch_place)
        : t(($) => $.move_agents.batch_move)}
    </Button>
  );

  return allMovable ? (
    moveButton
  ) : (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex">{moveButton}</span>} />
      <TooltipContent side="top">
        {t(($) => $.move_agents.no_permission)}
      </TooltipContent>
    </Tooltip>
  );
}
