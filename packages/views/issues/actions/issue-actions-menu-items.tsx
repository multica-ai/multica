"use client";

import {
  ArrowDown,
  ArrowUp,
  Calendar,
  Link2,
  MoreHorizontal,
  Pin,
  PinOff,
  Plus,
  Trash2,
  UserMinus,
} from "lucide-react";
import type { Issue } from "@multica/core/types";
import {
  ALL_STATUSES,
  PRIORITY_ORDER,
  PRIORITY_CONFIG,
} from "@multica/core/issues/config";
import { StatusIcon } from "../components/status-icon";
import { PriorityIcon } from "../components/priority-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuSeparator,
} from "@multica/ui/components/ui/context-menu";
import type { UseIssueActionsResult } from "./use-issue-actions";
import { useT } from "../../i18n";

// Both Dropdown and Context menu wrappers expose an API-compatible surface
// (variant, inset, onClick, etc.). We bundle the primitives we need into a
// single object so `IssueActionsMenuItems` can render the same JSX for both.
export interface MenuPrimitives {
  Item: typeof DropdownMenuItem;
  Sub: typeof DropdownMenuSub;
  SubTrigger: typeof DropdownMenuSubTrigger;
  SubContent: typeof DropdownMenuSubContent;
  Separator: typeof DropdownMenuSeparator;
}

export const dropdownPrimitives: MenuPrimitives = {
  Item: DropdownMenuItem,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
  Separator: DropdownMenuSeparator,
};

// Context primitives are API-compatible with Dropdown primitives, but their
// TypeScript identities differ. Cast once here and call it a day — this is the
// single bridge between the two primitive sets.
export const contextPrimitives: MenuPrimitives = {
  Item: ContextMenuItem as unknown as typeof DropdownMenuItem,
  Sub: ContextMenuSub as unknown as typeof DropdownMenuSub,
  SubTrigger: ContextMenuSubTrigger as unknown as typeof DropdownMenuSubTrigger,
  SubContent: ContextMenuSubContent as unknown as typeof DropdownMenuSubContent,
  Separator: ContextMenuSeparator as unknown as typeof DropdownMenuSeparator,
};

interface IssueActionsMenuItemsProps {
  issue: Issue;
  actions: UseIssueActionsResult;
  primitives: MenuPrimitives;
  /** If set, navigate here after the issue is deleted (used by the detail page). */
  onDeletedNavigateTo?: string;
}

export function IssueActionsMenuItems({
  issue,
  actions,
  primitives: P,
  onDeletedNavigateTo,
}: IssueActionsMenuItemsProps) {
  const { t } = useT("issues");
  const {
    members,
    agents,
    isPinned,
    updateField,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openSetParent,
    openAddChild,
    openDeleteConfirm,
  } = actions;

  const now = () => new Date();
  const inDays = (days: number) => {
    const d = new Date();
    d.setDate(d.getDate() + days);
    return d.toISOString();
  };

  return (
    <>
      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
          {t(($) => $.actions.status)}
        </P.SubTrigger>
        <P.SubContent>
          {ALL_STATUSES.map((s) => (
            <P.Item key={s} onClick={() => updateField({ status: s })}>
              <StatusIcon status={s} className="h-3.5 w-3.5" />
              {t(($) => $.status[s])}
              {issue.status === s && (
                <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Priority */}
      <P.Sub>
        <P.SubTrigger>
          <PriorityIcon priority={issue.priority} />
          {t(($) => $.actions.priority)}
        </P.SubTrigger>
        <P.SubContent>
          {PRIORITY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {issue.priority === p && (
                <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Assignee */}
      <P.Sub>
        <P.SubTrigger>
          <UserMinus className="h-3.5 w-3.5" />
          {t(($) => $.actions.assignee)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item
            onClick={() =>
              updateField({ assignee_type: null, assignee_id: null })
            }
          >
            <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.actions.unassigned)}
            {!issue.assignee_type && (
              <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
            )}
          </P.Item>
          {members.map((m) => (
            <P.Item
              key={m.user_id}
              onClick={() =>
                updateField({ assignee_type: "member", assignee_id: m.user_id })
              }
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
              {m.name}
              {issue.assignee_type === "member" &&
                issue.assignee_id === m.user_id && (
                  <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
                )}
            </P.Item>
          ))}
          {agents.map((a) => (
            <P.Item
              key={a.id}
              onClick={() =>
                updateField({ assignee_type: "agent", assignee_id: a.id })
              }
            >
              <ActorAvatar actorType="agent" actorId={a.id} size={16} />
              {a.name}
              {issue.assignee_type === "agent" && issue.assignee_id === a.id && (
                <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Due date */}
      <P.Sub>
        <P.SubTrigger>
          <Calendar className="h-3.5 w-3.5" />
          {t(($) => $.actions.due_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ due_date: now().toISOString() })}>
            {t(($) => $.actions.due_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: inDays(1) })}>
            {t(($) => $.actions.due_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: inDays(7) })}>
            {t(($) => $.actions.due_next_week)}
          </P.Item>
          {issue.due_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ due_date: null })}>
                {t(($) => $.actions.due_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item onClick={togglePin}>
        {isPinned ? (
          <PinOff className="h-3.5 w-3.5" />
        ) : (
          <Pin className="h-3.5 w-3.5" />
        )}
        {isPinned ? t(($) => $.actions.unpin_from_sidebar) : t(($) => $.actions.pin_to_sidebar)}
      </P.Item>
      <P.Item onClick={copyLink}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_link)}
      </P.Item>

      <P.Separator />

      {/* Relationship actions live under "More" — they're lower-frequency and
          will grow (blocks, duplicates, related) as we add more relation types. */}
      <P.Sub>
        <P.SubTrigger>
          <MoreHorizontal className="h-3.5 w-3.5" />
          {t(($) => $.actions.more)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={openCreateSubIssue}>
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.actions.create_sub_issue)}
          </P.Item>
          <P.Item onClick={openSetParent}>
            <ArrowUp className="h-3.5 w-3.5" />
            {t(($) => $.actions.set_parent_issue)}
          </P.Item>
          <P.Item onClick={openAddChild}>
            <ArrowDown className="h-3.5 w-3.5" />
            {t(($) => $.actions.add_sub_issue)}
          </P.Item>
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item
        variant="destructive"
        onClick={() => openDeleteConfirm({ onDeletedNavigateTo })}
      >
        <Trash2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.delete_issue)}
      </P.Item>
    </>
  );
}
