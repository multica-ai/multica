"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
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
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import type { InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import { useToggleIssueSubscriber, useUnsubscribeFromIssueSubtree } from "@multica/core/issues/mutations";
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
  const { offered, asksScope, childCountKnown, childCount } = useUnsubscribeOffer(issueId, userId);
  const [open, setOpen] = useState(false);

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
      <DropdownMenuContent side="top" align="end" className="w-auto min-w-56 max-w-80">
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
                  ? t(($) => $.unsubscribe.subtree, { count: childCount })
                  : t(($) => $.unsubscribe.subtree_unknown)}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * `…` once the open notification has settled (archived, snoozed) and stays on
 * screen: following its issue is all that is left to change there, so it
 * offers only the way out. Unlike "Unsubscribe & archive" it leaves the
 * notification as it is. Renders nothing where unsubscribing is not offered.
 */
export function InboxSettledMoreMenu({ issueId }: { issueId: string }) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const userId = useAuthStore((s) => s.user?.id);
  const { offered, asksScope } = useUnsubscribeOffer(issueId, userId);
  const leave = useToggleIssueSubscriber(issueId);
  const leaveSubtree = useUnsubscribeFromIssueSubtree(issueId);
  if (!offered || !userId) return null;

  // On the request's promise: moving on to the next notification unmounts the
  // menu before the request settles, and React Query skips per-call callbacks then.
  const unsubscribe = () =>
    void leave.mutateAsync({ userId, userType: "member", subscribed: true }).then(
      () => toast.success(t(($) => $.kept.unsubscribed), { description: t(($) => $.toasts.unsubscribed_hint) }),
      () => toast.error(tIssues(($) => $.detail.subscription_update_failed)),
    );
  const unsubscribeSubtree = () =>
    void leaveSubtree.mutateAsync({ userId, userType: "member" }).then(
      () => toast.success(tIssues(($) => $.detail.unsubscribe_subtree_succeeded)),
      () => toast.error(tIssues(($) => $.detail.unsubscribe_subtree_failed)),
    );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton label={t(($) => $.detail.more_actions)} tooltipSide="top" className="text-muted-foreground" />
        }
      >
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="end" className="w-auto min-w-56 max-w-80">
        <DropdownMenuItem disabled={leave.isPending} onClick={unsubscribe}>
          <BellOff />
          {tIssues(($) => $.detail.unsubscribe_this)}
        </DropdownMenuItem>
        {asksScope && (
          <DropdownMenuItem disabled={leaveSubtree.isPending} onClick={unsubscribeSubtree}>
            <ListTree />
            {tIssues(($) => $.detail.unsubscribe_subtree)}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Whether the viewer may leave the issue from the Inbox: they follow it, and
 * it is not theirs or their agent's. Unknown until every query behind that
 * has answered, so the offer never flashes where it does not belong.
 */
function useUnsubscribeOffer(issueId: string, userId: string | undefined) {
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const { data: agents, isSuccess: agentsKnown } = useQuery(agentListOptions(wsId));
  const { data: subscribers, isSuccess: subscribersKnown } = useQuery(issueSubscribersOptions(issueId));
  const { data: children = [], isSuccess: childrenLoaded, isFetching: childrenFetching } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !!issue,
  });
  const subscribed = !!subscribers?.some((s) => s.user_type === "member" && s.user_id === userId);
  const offered =
    !!issue && agentsKnown && subscribersKnown && subscribed && !isOwnIssue(issue, userId, agents ?? []);
  const childCountKnown = childrenLoaded && !childrenFetching;
  return { offered, childCountKnown, childCount: children.length, asksScope: !childCountKnown || children.length > 0 };
}
