"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BellOff, Check, CircleDot, ListTree, MoreHorizontal } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import type { InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import { childIssuesOptions, issueDetailOptions, issueSubscribersOptions } from "@multica/core/issues/queries";
import { isOwnIssue } from "@multica/core/issues/ownership";
import { agentListOptions } from "@multica/core/workspace/queries";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { useInboxShortcut } from "./use-inbox-shortcut";

/**
 * The open notification's less frequent actions, behind `…` in its floating
 * group: marking it read or unread (U, handled by the Inbox page), and
 * "Unsubscribe & archive" (⇧E).
 *
 * Unsubscribing is offered only where it is a bystander's way out: the viewer
 * is subscribed, and the issue is not assigned to them or to an agent they
 * own. It stays out of the menu until all of that is known, so it never
 * flashes on an issue where it does not belong.
 *
 * With sub-issues it asks for the scope, reusing the issue-only and subtree
 * opt-outs the Activity section offers, and ⇧E opens the menu on that choice.
 * While the child count is still loading it asks too, rather than picking a
 * scope for the user.
 */
export function InboxMoreMenu({
  issueId,
  userId,
  read,
  onToggleRead,
  onUnsubscribe,
}: {
  issueId: string;
  /** The viewer. */
  userId: string | undefined;
  read: boolean;
  onToggleRead: () => void;
  onUnsubscribe: (scope: InboxUnsubscribeScope) => void;
}) {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: agents, isSuccess: agentsKnown } = useQuery(agentListOptions(wsId));
  const { data: subscribers, isSuccess: subscribersKnown } = useQuery(issueSubscribersOptions(issueId));
  const { data: children = [], isSuccess: childrenLoaded, isFetching: childrenFetching } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !!issue,
  });
  const [open, setOpen] = useState(false);

  const subscribed = !!subscribers?.some((s) => s.user_type === "member" && s.user_id === userId);
  const offered =
    !!issue && agentsKnown && subscribersKnown && subscribed && !isOwnIssue(issue, userId, agents ?? []);
  const childCountKnown = childrenLoaded && !childrenFetching;
  const asksScope = !childCountKnown || children.length > 0;

  useInboxShortcut(
    "unsubscribeArchiveInboxItem",
    offered ? (asksScope ? () => setOpen(true) : () => onUnsubscribe("issue")) : null,
  );

  const label = t(($) => $.detail.more_actions);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={<IconButton label={label} tooltipSide="top" className="text-muted-foreground" />}
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-auto min-w-56 max-w-80">
        {/* onClick, not onSelect: Base UI's Menu.Item has no onSelect. */}
        <DropdownMenuItem onClick={onToggleRead}>
          {read ? <CircleDot /> : <Check />}
          {read ? t(($) => $.context_menu.mark_unread) : t(($) => $.context_menu.mark_read)}
          <DropdownMenuShortcut>
            <ActionShortcutKeycaps action="markInboxUnread" />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        {offered && !asksScope && (
          <DropdownMenuItem onClick={() => onUnsubscribe("issue")}>
            <BellOff />
            {t(($) => $.context_menu.unsubscribe_archive)}
            <DropdownMenuShortcut>
              <ActionShortcutKeycaps action="unsubscribeArchiveInboxItem" />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        )}
        {offered && asksScope && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.unsubscribe.title)}</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => onUnsubscribe("issue")}>
                <BellOff />
                {t(($) => $.unsubscribe.this_issue)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onUnsubscribe("subtree")}>
                <ListTree />
                {childCountKnown
                  ? t(($) => $.unsubscribe.subtree, { count: children.length })
                  : t(($) => $.unsubscribe.subtree_unknown)}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
