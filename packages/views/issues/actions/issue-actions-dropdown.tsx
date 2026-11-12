"use client";

import { useState, type ReactElement } from "react";
import type { Issue } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from "@multica/ui/components/ui/dropdown-menu";
import type { useRowMenu } from "../../common/point-menu";
import { useIssueActions } from "./use-issue-actions";
import {
  IssueActionsMenuItems,
  dropdownPrimitives,
} from "./issue-actions-menu-items";

interface IssueActionsDropdownProps {
  issue: Issue;
  /** A single React element cloned by Base UI as the trigger (via `render` prop). */
  trigger: ReactElement;
  align?: "start" | "end" | "center";
  /** If set, leave the page after the issue is deleted — back to wherever the
   *  user came from, or to this path when there is no in-app history. */
  onDeletedFallbackPath?: string;
  /** The reader's archived inbox notification for this issue; adds "Restore to inbox". */
  archivedInboxItemId?: string;
  /** Adds "Show in sidebar". */
  onShowInSidebar?: () => void;
  /** Open state shared with a right-click surface, which opens this same
   *  menu at the cursor via `openAtPointer`. */
  rowMenu?: ReturnType<typeof useRowMenu>;
}

export function IssueActionsDropdown({
  issue,
  trigger,
  align = "end",
  onDeletedFallbackPath,
  archivedInboxItemId,
  onShowInSidebar,
  rowMenu,
}: IssueActionsDropdownProps) {
  const actions = useIssueActions(issue);
  const [ownOpen, setOwnOpen] = useState(false);
  const open = rowMenu ? rowMenu.menu.open : ownOpen;
  const atPoint = !!rowMenu?.menu.point;
  const close = () => {
    if (rowMenu) rowMenu.setMenu((menu) => ({ ...menu, open: false }));
    else setOwnOpen(false);
  };

  return (
    <DropdownMenu open={open} onOpenChange={rowMenu ? rowMenu.onOpenChange : setOwnOpen}>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent
        align={atPoint ? "start" : align}
        anchor={rowMenu?.anchor}
        className="w-auto"
      >
        <IssueActionsMenuItems
          issue={issue}
          actions={actions}
          primitives={dropdownPrimitives}
          onClose={close}
          onDeletedFallbackPath={onDeletedFallbackPath}
          archivedInboxItemId={archivedInboxItemId}
          onShowInSidebar={onShowInSidebar}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
