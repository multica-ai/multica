"use client";

import { useState, type ReactElement } from "react";
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
import { AssigneePicker } from "../components/pickers";

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
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [assigneeMounted, setAssigneeMounted] = useState(false);

  // The outer `relative inline-flex` is the picker's anchor box: the
  // absolute, pointer-events-none span inside `triggerRender` fills it, so
  // the popover positions itself relative to the dropdown's 3-dot button
  // without us having to thread a ref through Base UI's anchor API.
  return (
    <span className="relative inline-flex">
      <DropdownMenu>
        <DropdownMenuTrigger render={trigger} />
        <DropdownMenuContent align={align} className="w-auto">
          <IssueActionsMenuItems
            issue={issue}
            actions={actions}
            primitives={dropdownPrimitives}
            onOpenAssignee={() => {
              setAssigneeMounted(true);
              setAssigneeOpen(true);
            }}
            onDeletedNavigateTo={onDeletedNavigateTo}
          />
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Lazily mount the picker on first use so untouched list/board rows do
          not subscribe to its queries. Keep it mounted after the popover
          closes: RuntimeSelectDialog is portalled outside the popover, so a
          click on a runtime closes the popover as an outside interaction.
          Unmounting here would also destroy the dialog before confirmation. */}
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
              className="pointer-events-none absolute inset-0"
            />
          }
          trigger={<span />}
          align={align}
        />
      )}
    </span>
  );
}
