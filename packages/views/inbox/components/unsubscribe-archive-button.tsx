"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { BellOff, ListTree } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { InboxUnsubscribeScope } from "@multica/core/inbox/mutations";
import { childIssuesOptions, issueDetailOptions, issueSubscribersOptions } from "@multica/core/issues/queries";
import { isOwnIssue } from "@multica/core/issues/ownership";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";

/**
 * "Unsubscribe & archive" in the Inbox bar of an issue opened from the Inbox.
 *
 * Offered only where it is a bystander's way out: the viewer is subscribed,
 * and the issue is not assigned to them or to an agent they own. Renders
 * nothing until all of that is known, so it never flashes on an issue where
 * it does not belong.
 *
 * With sub-issues it asks for the scope, reusing the issue-only and subtree
 * opt-outs the Activity section offers. While the child count is still
 * loading it asks too, rather than picking a scope for the user.
 */
export function UnsubscribeArchiveButton({
  issueId,
  userId,
  onConfirm,
}: {
  issueId: string;
  /** The viewer. */
  userId: string | undefined;
  onConfirm: (scope: InboxUnsubscribeScope) => void;
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
  const [menuOpen, setMenuOpen] = useState(false);

  const subscribed = !!subscribers?.some((s) => s.user_type === "member" && s.user_id === userId);
  const offered =
    !!issue && agentsKnown && subscribersKnown && subscribed && !isOwnIssue(issue, userId, agents ?? []);
  const childCountKnown = childrenLoaded && !childrenFetching;
  const asksScope = !childCountKnown || children.length > 0;

  const shortcutRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    shortcutRef.current = offered ? (asksScope ? () => setMenuOpen(true) : () => onConfirm("issue")) : null;
  });
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      if (!shortcutMatchesEvent(getShortcut("unsubscribeArchiveInboxItem"), event)) return;
      const run = shortcutRef.current;
      if (!run) return;
      event.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  if (!offered) return null;

  const label = t(($) => $.context_menu.unsubscribe_archive);
  const shortcut = <ActionShortcutKeycaps action="unsubscribeArchiveInboxItem" />;

  if (!asksScope) {
    return (
      <IconButton
        label={label}
        shortcut={shortcut}
        tooltipSide="bottom"
        className="text-muted-foreground"
        onClick={() => onConfirm("issue")}
      >
        <BellOff />
      </IconButton>
    );
  }

  return (
    <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
      <DropdownMenuTrigger
        render={
          <IconButton label={label} shortcut={shortcut} tooltipSide="bottom" className="text-muted-foreground" />
        }
      >
        <BellOff />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.unsubscribe.title)}</DropdownMenuLabel>
          {/* onClick, not onSelect: Base UI's Menu.Item has no onSelect. */}
          <DropdownMenuItem onClick={() => onConfirm("issue")}>
            <BellOff />
            {t(($) => $.unsubscribe.this_issue)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onConfirm("subtree")}>
            <ListTree />
            {childCountKnown
              ? t(($) => $.unsubscribe.subtree, { count: children.length })
              : t(($) => $.unsubscribe.subtree_unknown)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
