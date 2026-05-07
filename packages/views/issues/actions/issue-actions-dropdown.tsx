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

interface IssueActionsDropdownProps {
  issue: Issue;
  /** A single React element cloned by Base UI as the trigger (via `render` prop). */
  trigger: ReactElement;
  align?: "start" | "end" | "center";
  /** If set, navigate here after the issue is deleted. */
  onDeletedNavigateTo?: string;
}

export function IssueActionsDropdown({
  issue,
  trigger,
  align = "end",
  onDeletedNavigateTo,
}: IssueActionsDropdownProps) {
  const actions = useIssueActions(issue);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align={align} className="w-auto">
        <IssueActionsMenuItems
          issue={issue}
          actions={actions}
          primitives={dropdownPrimitives}
          onDeletedNavigateTo={onDeletedNavigateTo}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
