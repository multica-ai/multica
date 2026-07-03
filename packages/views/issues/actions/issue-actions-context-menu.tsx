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
  const [assigneeMounted, setAssigneeMounted] = useState(false);
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
            onOpenAssignee={() => {
              setAssigneeMounted(true);
              setAssigneeOpen(true);
            }}
          />
        </ContextMenuContent>
      </ContextMenu>
      {/* Lazily mount on first use, then retain the picker so its portalled
          RuntimeSelectDialog survives the context-menu popover closing when
          the user clicks a runtime option. Untouched rows still pay no query
          subscription cost. */}
      {assigneeMounted && (
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
