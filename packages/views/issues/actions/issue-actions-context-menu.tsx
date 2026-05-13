"use client";

import type { ReactElement } from "react";
import type { Issue } from "@multica/core/types";
import {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
} from "@multica/ui/components/ui/context-menu";
import { useIssueActions } from "./use-issue-actions";
import {
  IssueActionsMenuItems,
  contextPrimitives,
} from "./issue-actions-menu-items";

interface IssueActionsContextMenuProps {
  issue: Issue;
  /** A single React element cloned by Base UI as the trigger (via `render` prop). */
  children: ReactElement;
}

export function IssueActionsContextMenu({
  issue,
  children,
}: IssueActionsContextMenuProps) {
  const actions = useIssueActions(issue);
  return (
    <ContextMenu>
      <ContextMenuTrigger render={children} />
      <ContextMenuContent>
        <IssueActionsMenuItems
          issue={issue}
          actions={actions}
          primitives={contextPrimitives}
        />
      </ContextMenuContent>
    </ContextMenu>
  );
}
