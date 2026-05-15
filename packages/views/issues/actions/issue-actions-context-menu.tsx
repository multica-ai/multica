"use client";

import { useRef, useState, type ReactElement } from "react";
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
import { AssigneePicker } from "../components/pickers";

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
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  // Right-click coordinates captured during contextmenu so the AssigneePicker
  // opens where the context menu just was, instead of jumping to the row's
  // top-left corner. Reset between opens; only consulted while the picker is
  // mounted-open.
  const clickPosRef = useRef<{ x: number; y: number }>({ x: 0, y: 0 });

  const handleContextMenu = (e: React.MouseEvent) => {
    clickPosRef.current = { x: e.clientX, y: e.clientY };
  };

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger
          render={children}
          onContextMenu={handleContextMenu}
        />
        <ContextMenuContent>
          <IssueActionsMenuItems
            issue={issue}
            actions={actions}
            primitives={contextPrimitives}
            onOpenAssignee={() => setAssigneeOpen(true)}
          />
        </ContextMenuContent>
      </ContextMenu>
      {/* Mount the picker only once the user actually opens it. Otherwise
          every row in a list/board would subscribe to members/agents/squads
          /frequency queries on mount, multiplying memory + render cost. */}
      {assigneeOpen && (
        <AssigneePicker
          assigneeType={issue.assignee_type}
          assigneeId={issue.assignee_id}
          onUpdate={actions.updateField}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={
            <span
              aria-hidden
              className="pointer-events-none fixed"
              style={{
                left: clickPosRef.current.x,
                top: clickPosRef.current.y,
                width: 0,
                height: 0,
              }}
            />
          }
          trigger={<span />}
          align="start"
        />
      )}
    </>
  );
}
