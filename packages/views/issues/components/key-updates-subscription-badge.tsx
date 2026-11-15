"use client";

import { useQuery } from "@tanstack/react-query";
import { Bell, Settings2 } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { receivesKeyUpdatesOnly } from "@multica/core/notification-preferences/bystander";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { IssueAssigneeType, IssueSubscriber } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

/**
 * Says that this issue reaches the viewer as key updates only — someone
 * else's issue they follow passively (MUL-9343) — and offers the way back to
 * every event for this one issue. Renders nothing until the preference and the
 * agent list resolve, so it never flashes on the viewer's own issue.
 */
export function KeyUpdatesSubscriptionBadge({
  issue,
  userId,
  reason,
  projectCovered,
  pending,
  onFollowAll,
}: {
  issue: { assignee_type?: IssueAssigneeType | null; assignee_id?: string | null };
  userId: string | undefined;
  reason: IssueSubscriber["reason"] | undefined;
  projectCovered: boolean;
  pending: boolean;
  onFollowAll: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: preferences, isSuccess: preferencesKnown } = useQuery(
    notificationPreferenceOptions(wsId),
  );
  const { data: agents, isSuccess: agentsKnown } = useQuery(agentListOptions(wsId));

  if (
    !preferencesKnown ||
    !agentsKnown ||
    !receivesKeyUpdatesOnly({
      reason,
      projectCovered,
      issue,
      userId,
      agents: agents ?? [],
      preferences: preferences?.preferences ?? {},
    })
  ) {
    return null;
  }

  return (
    <DropdownMenu>
      {/* Same quiet surface as the delegated badge: it explains a state; the
          menu holds the one action that changes it. */}
      <DropdownMenuTrigger
        disabled={pending}
        className="cursor-pointer rounded-md bg-muted px-2 py-0.5 text-caption text-muted-foreground transition-colors hover:text-foreground"
      >
        {t(($) => $.detail.key_updates_subscription_badge)}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="whitespace-normal font-normal leading-snug">
            {t(($) => $.detail.key_updates_subscription_hint)}
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onFollowAll} disabled={pending}>
          <Bell />
          {t(($) => $.detail.key_updates_follow_all)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => push(`${paths.settings()}?tab=notifications&section=others_issues`)}
        >
          <Settings2 />
          {t(($) => $.detail.key_updates_settings)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
