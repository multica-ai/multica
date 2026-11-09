"use client";

import { Check } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import type { InboxItem } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { InboxDetailLabel } from "./inbox-detail-label";
import { useTimeAgo } from "./inbox-list-item";

/**
 * Why an issue is waiting on the user, above its title: who did what, and
 * the action that takes it out of Needs action. Once the user has acted on
 * the issue, it says so until they move on.
 */
export function InboxReasonBar({
  item,
  handled,
  onDone,
}: {
  item: InboxItem;
  handled: boolean;
  onDone: () => void;
}) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();

  if (handled) {
    return (
      <div
        data-testid="inbox-reason-bar"
        className="mb-4 flex min-h-10 items-center gap-2 rounded-md border bg-muted/40 px-3 py-1.5 text-body text-muted-foreground"
      >
        <Check className="size-4 shrink-0" />
        <span className="min-w-0 truncate">{t(($) => $.detail.handled)}</span>
      </div>
    );
  }

  return (
    <div
      data-testid="inbox-reason-bar"
      className="mb-4 flex min-h-10 items-center gap-2 rounded-md border bg-muted/40 py-1 pl-3 pr-1 text-body"
    >
      <ActorAvatar actorType={item.actor_type ?? item.recipient_type} actorId={item.actor_id ?? item.recipient_id} size="sm" />
      <span className="min-w-0 flex-1 truncate text-muted-foreground">
        <InboxDetailLabel item={item} />
      </span>
      <span className="shrink-0 text-caption text-faint-foreground">{timeAgo(item.created_at)}</span>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button variant="ghost" size="sm" className="shrink-0" onClick={onDone}>
              <Check className="mr-1.5 size-3.5" />
              {t(($) => $.detail.done)}
            </Button>
          }
        />
        <TooltipContent side="bottom">
          {t(($) => $.detail.done)}
          <ActionShortcutKeycaps action="archiveInboxItem" />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}
