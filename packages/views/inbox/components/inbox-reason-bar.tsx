"use client";

import { Archive, ArchiveRestore, Check } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import type { InboxItem } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { InboxDetailLabel } from "./inbox-detail-label";
import { useTimeAgo } from "./inbox-list-item";
import type { InboxAction } from "./use-acted-on-issue";

/**
 * Why the open issue left the Inbox on its own: it was only a notice and has
 * now been opened, it reached a done or cancelled status, or the user handed
 * it to an agent. `archived` covers the rest (archived from another device).
 */
export type InboxHandledReason = "notice" | "closed" | "agent" | "archived";

/**
 * Why an issue is waiting on the user, above its title: who did what, and
 * the action that takes it out of the Inbox. After a reply or an update it
 * says so and makes Archive the next step (`acted`). Once the issue has left
 * the Inbox it says why, and offers to put it back (`handled`).
 */
export function InboxReasonBar({
  item,
  handled = null,
  acted = null,
  more = 0,
  onArchive,
  onRestore,
}: {
  item: InboxItem;
  handled?: InboxHandledReason | null;
  acted?: InboxAction | null;
  /** Notifications on the issue besides the one named here. */
  more?: number;
  onArchive: () => void;
  onRestore?: () => void;
}) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();

  if (handled) {
    const text =
      handled === "notice"
        ? t(($) => $.detail.handled_notice)
        : handled === "closed"
          ? t(($) => $.detail.handled_closed)
          : handled === "agent"
            ? t(($) => $.detail.handled_agent)
            : t(($) => $.detail.handled);
    return (
      <div
        data-testid="inbox-reason-bar"
        className="mb-4 flex min-h-10 items-center gap-2 rounded-md border bg-muted/40 py-1 pl-3 pr-1 text-body text-muted-foreground"
      >
        <Check className="size-4 shrink-0" />
        <span className="min-w-0 flex-1 truncate">{text}</span>
        {onRestore && (
          <Button variant="ghost" size="sm" className="shrink-0" onClick={onRestore}>
            <ArchiveRestore className="mr-1.5 size-3.5" />
            {t(($) => $.detail.restore)}
          </Button>
        )}
      </div>
    );
  }

  return (
    <div
      data-testid="inbox-reason-bar"
      className="mb-4 flex min-h-10 items-center gap-2 rounded-md border bg-muted/40 py-1 pl-3 pr-1 text-body"
    >
      {acted ? (
        <>
          <Check className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate text-muted-foreground">
            {acted === "replied" ? t(($) => $.detail.acted_replied) : t(($) => $.detail.acted_updated)}
          </span>
        </>
      ) : (
        <>
          <ActorAvatar actorType={item.actor_type ?? item.recipient_type} actorId={item.actor_id ?? item.recipient_id} size="sm" />
          <span className="min-w-0 flex-1 truncate">
            <InboxDetailLabel item={item} reasonOnly />
            {more > 0 && (
              <span className="text-muted-foreground"> · {t(($) => $.labels.more, { count: more })}</span>
            )}
          </span>
          <span className="shrink-0 text-caption text-faint-foreground">{timeAgo(item.created_at)}</span>
        </>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <Button variant={acted ? "default" : "ghost"} size="sm" className="shrink-0" onClick={onArchive}>
              <Archive className="mr-1.5 size-3.5" />
              {t(($) => $.detail.archive)}
            </Button>
          }
        />
        <TooltipContent side="bottom">
          {t(($) => $.detail.archive)}
          <ActionShortcutKeycaps action="archiveInboxItem" />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}
