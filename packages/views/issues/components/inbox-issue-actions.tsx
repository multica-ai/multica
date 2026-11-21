"use client";

import { ChevronDown, UserRound } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { noteInboxBarAction, takeInboxBarAction } from "@multica/core/inbox/auto-archive";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { useStatusLabel } from "../utils/status-label";
import { AssigneePicker } from "./pickers/assignee-picker";
import { StatusPicker } from "./pickers/status-picker";
import { QuickActionsMenu } from "./quick-actions-section";
import { StatusIcon } from "./status-icon";

/**
 * The issue's own actions in the Inbox bar, the ones that resolve most
 * notifications: its status and assignee, set as the Properties panel sets
 * them, and its quick actions. Each shows its current value, unlike the
 * notification's actions beside it.
 */
export function InboxIssueActions({
  issue,
  onUpdate,
  onMarkDuplicate,
  isDuplicate,
}: {
  issue: Issue;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onMarkDuplicate?: () => void;
  isDuplicate?: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const statusLabelOf = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const chip = <Button variant="ghost" size="sm" className="min-w-0 max-w-48 shrink-0" />;
  const chevron = <ChevronDown className="size-3 shrink-0 text-muted-foreground @max-[30rem]/inbox-bar:hidden" />;
  const assigned = issue.assignee_type && issue.assignee_id ? { type: issue.assignee_type, id: issue.assignee_id } : null;
  // Each of these archives the notification, and the settled bar names it.
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
    <>
      <StatusPicker
        status={issue.status}
        onUpdate={update}
        projectId={issue.project_id}
        issue={issue}
        onMarkDuplicate={onMarkDuplicate}
        isDuplicate={isDuplicate}
        align="start"
        tooltip={t(($) => $.detail.prop_status)}
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
            <span className="truncate @max-[36rem]/inbox-bar:hidden">{statusLabelOf(issue.status)}</span>
            {chevron}
          </>
        }
      />
      <AssigneePicker
        assigneeType={issue.assignee_type}
        assigneeId={issue.assignee_id}
        onUpdate={update}
        align="start"
        tooltip={t(($) => $.detail.prop_assignee)}
        triggerRender={chip}
        trigger={
          assigned ? (
            <>
              <ActorAvatar actorType={assigned.type} actorId={assigned.id} size="xs" showStatusDot />
              <span className="truncate @max-[36rem]/inbox-bar:hidden">{getActorName(assigned.type, assigned.id)}</span>
              {chevron}
            </>
          ) : (
            <>
              <UserRound className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate text-muted-foreground @max-[36rem]/inbox-bar:hidden">
                {t(($) => $.pickers.assignee.trigger_unassigned)}
              </span>
              {chevron}
            </>
          )
        }
      />
      <QuickActionsMenu
        issueId={issue.id}
        onRun={(action, ran) => {
          noteInboxBarAction(issue.id, { type: "quick_action", name: action.name });
          void ran.then((posted) => posted || takeInboxBarAction(issue.id));
        }}
      />
    </>
  );
}
