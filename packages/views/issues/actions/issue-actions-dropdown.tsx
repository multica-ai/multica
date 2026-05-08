"use client";

import type { ReactElement } from "react";
import type { Issue } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIssueActions } from "./use-issue-actions";
import {
  IssueActionsMenuItems,
  dropdownPrimitives,
} from "./issue-actions-menu-items";
import { useTakeOver } from "./use-take-over";

interface IssueActionsDropdownProps {
  issue: Issue;
  /** A single React element cloned by Base UI as the trigger (via `render` prop). */
  trigger: ReactElement;
  align?: "start" | "end" | "center";
  /** If set, navigate here after the issue is deleted. */
  onDeletedNavigateTo?: string;
  /**
   * Enable the desktop-only "Take Over Locally" item. The dropdown still
   * decides per-issue visibility based on the run history; this flag only
   * gates the data-fetch (board/list rows pass false to skip the round-trip).
   */
  enableTakeOver?: boolean;
}

export function IssueActionsDropdown({
  issue,
  trigger,
  align = "end",
  onDeletedNavigateTo,
  enableTakeOver = false,
}: IssueActionsDropdownProps) {
  const actions = useIssueActions(issue);
  const takeOver = useTakeOver(issue.id, { enabled: enableTakeOver });
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align={align} className="w-auto">
        <IssueActionsMenuItems
          issue={issue}
          actions={actions}
          primitives={dropdownPrimitives}
          onDeletedNavigateTo={onDeletedNavigateTo}
          onTakeOverLocally={takeOver.visible ? takeOver.takeOver : undefined}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
