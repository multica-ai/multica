"use client";

import { Bell, Settings2 } from "@multica/ui/icons";
import { useWorkspacePaths } from "@multica/core/paths";
import type { IssueSubscriberDelivery } from "@multica/core/types";
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
 * every event for this one issue. The tier is the server's `delivery` for the
 * viewer's subscription; without one it renders nothing.
 */
export function KeyUpdatesSubscriptionBadge({
  delivery,
  pending,
  onFollowAll,
}: {
  delivery: IssueSubscriberDelivery | undefined;
  pending: boolean;
  onFollowAll: () => void;
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { push } = useNavigation();

  if (delivery !== "key_updates") return null;

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
