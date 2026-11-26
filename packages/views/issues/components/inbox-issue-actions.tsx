"use client";

import { UserRound } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { noteInboxBarAction, takeInboxBarAction } from "@multica/core/inbox/auto-archive";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Issue, QuickAction, UpdateIssueRequest } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { useStatusLabel } from "../utils/status-label";
import { AssigneePicker } from "./pickers/assignee-picker";
import { StatusPicker } from "./pickers/status-picker";
import { QuickActionsMenu } from "./quick-actions-section";
import { StatusIcon } from "./status-icon";
import { ISSUE_BAR_HIDE_LABEL, type IssueBarContainer } from "./issue-bar-container";

type IssueBarActionsProps = {
  issue: Issue;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onMarkDuplicate?: () => void;
  isDuplicate?: boolean;
};

/**
 * The issue's own actions as a row of chips: its status and assignee, set as
 * the Properties panel sets them, and its quick actions. Each shows its current
 * value. Used by the Inbox's floating group above the composer, whose menus
 * open upward, and by the issue page header while the properties sidebar is
 * closed, whose menus open downward.
 */
export function IssueBarActions({
  issue,
  onUpdate,
  onMarkDuplicate,
  isDuplicate,
  container,
  side,
  onQuickActionRun,
}: IssueBarActionsProps & {
  container: IssueBarContainer;
  side: "top" | "bottom";
  onQuickActionRun?: (action: QuickAction, ran: Promise<boolean>) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const statusLabelOf = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const hideLabel = ISSUE_BAR_HIDE_LABEL[container];
  const chip = <Button variant="ghost" size="sm" className="min-w-0 max-w-48 shrink-0" />;
  const assigned = issue.assignee_type && issue.assignee_id ? { type: issue.assignee_type, id: issue.assignee_id } : null;

  return (
    <>
      <StatusPicker
        status={issue.status}
        onUpdate={onUpdate}
        projectId={issue.project_id}
        issue={issue}
        onMarkDuplicate={onMarkDuplicate}
        isDuplicate={isDuplicate}
        align="start"
        side={side}
        tooltip={
          <>
            {t(($) => $.detail.prop_status)}
            <ActionShortcutKeycaps action="setIssueStatus" />
          </>
        }
        triggerRender={chip}
        trigger={
          <>
            <StatusIcon
              status={issue.status}
              category={categoryOf(issue.status)}
              color={colorOf(issue.status)}
              icon={iconOf(issue.status)}
              className="size-3.5 shrink-0"
            />
            <span className={cn("truncate", hideLabel)}>{statusLabelOf(issue.status)}</span>
          </>
        }
      />
      <AssigneePicker
        assigneeType={issue.assignee_type}
        assigneeId={issue.assignee_id}
        onUpdate={onUpdate}
        align="start"
        side={side}
        tooltip={
          <>
            {t(($) => $.detail.prop_assignee)}
            <ActionShortcutKeycaps action="assignIssue" />
          </>
        }
        triggerRender={chip}
        trigger={
          assigned ? (
            <>
              <ActorAvatar actorType={assigned.type} actorId={assigned.id} size="xs" showStatusDot />
              <span className={cn("truncate", hideLabel)}>{getActorName(assigned.type, assigned.id)}</span>
            </>
          ) : (
            <>
              <UserRound className="size-3.5 shrink-0 text-muted-foreground" />
              <span className={cn("truncate text-muted-foreground", hideLabel)}>
                {t(($) => $.pickers.assignee.trigger_unassigned)}
              </span>
            </>
          )
        }
      />
      <QuickActionsMenu issueId={issue.id} side={side} onRun={onQuickActionRun} />
    </>
  );
}

/**
 * The issue's actions in the Inbox's floating group, the ones that resolve most
 * notifications. Each of them archives the notification, so each is recorded
 * for the settled bar to name.
 */
export function InboxIssueActions({ issue, onUpdate, ...props }: IssueBarActionsProps) {
  const update = (updates: Partial<UpdateIssueRequest>) => {
    if (updates.status) {
      noteInboxBarAction(issue.id, { type: "status", status: updates.status });
    } else if ("assignee_id" in updates) {
      const assignee = updates.assignee_type && updates.assignee_id ? { type: updates.assignee_type, id: updates.assignee_id } : null;
      noteInboxBarAction(issue.id, { type: "assignee", assignee });
    }
    onUpdate(updates);
  };

  return (
    <IssueBarActions
      {...props}
      issue={issue}
      onUpdate={update}
      container="inbox-bar"
      side="top"
      onQuickActionRun={(action, ran) => {
        noteInboxBarAction(issue.id, { type: "quick_action", name: action.name });
        void ran.then((posted) => posted || takeInboxBarAction(issue.id));
      }}
    />
  );
}
