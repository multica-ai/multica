"use client";

import type { ComponentProps, ReactNode } from "react";
import { Archive } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { cn } from "@multica/ui/lib/utils";
import type { ShortcutActionId } from "@multica/core/shortcuts";
import type { InboxItem } from "@multica/core/types";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT } from "../../i18n";
import { InboxActorAvatar, InboxActorLine } from "./inbox-actor";
import { useTimeAgo } from "./inbox-list-item";

/**
 * The bar under the header of an issue opened from the Inbox: the one place
 * its notification lives, whatever state that is in. Open, it says why the
 * issue is here and offers what resolves it, the issue's own actions first,
 * then the notification's. Settled (archived, snoozed), it says so, with the
 * way back and the way on, in the same place.
 *
 * Its width follows the panels beside the issue, not the window, so labels
 * give way by the bar's own width (`@container/inbox-bar`): quick actions
 * first, then the notification's actions, the time, and the issue's values.
 */
export function InboxBar({ settled = false, children }: { settled?: boolean; children: ReactNode }) {
  return (
    <div
      role={settled ? "status" : undefined}
      data-inbox-bar={settled ? "settled" : "open"}
      className={cn(
        "@container/inbox-bar relative flex h-10 shrink-0 items-center gap-1 border-b",
        settled && "bg-muted/40",
        PAGE_GUTTER,
      )}
    >
      {children}
    </div>
  );
}

/**
 * Reserves the 40px InboxBar slot while a cold issue opened from the Inbox
 * loads, so the reading column below never shifts 40px when the issue lands.
 * When the Inbox list already holds the notification row, renders its real
 * actor and reason immediately.
 */
export function InboxBarSkeleton({ item }: { item?: InboxItem | null }) {
  return (
    <InboxBar>
      {item ? (
        <InboxBarReason item={item} />
      ) : (
        <span className="mr-1 flex min-w-0 flex-1 items-center gap-1.5">
          <Skeleton className="size-4 shrink-0 rounded-full" />
          <Skeleton className="h-3.5 w-40" />
        </span>
      )}
      <div className="flex shrink-0 items-center gap-1.5">
        <Skeleton className="h-6 w-14 rounded-md" />
        <Skeleton className="h-6 w-14 rounded-md" />
      </div>
    </InboxBar>
  );
}

/**
 * The open notification's bar: why it is here, the issue's actions, and the
 * notification's own (`children`).
 */
export function InboxOpenBar({
  item,
  issueActions,
  children,
}: {
  item: InboxItem;
  /** Left out where the viewer cannot change the issue. */
  issueActions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <InboxBar>
      <InboxBarReason item={item} />
      {issueActions}
      {issueActions && <InboxBarSeparator />}
      {children}
    </InboxBar>
  );
}

/** Who did what, and when. A comment is named, not quoted: the timeline below shows it. */
function InboxBarReason({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();
  const detail = item.type === "new_comment" ? t(($) => $.labels.commented) : undefined;
  return (
    <span className="mr-1 flex min-w-0 flex-1 items-center gap-1.5 text-caption text-muted-foreground">
      <span className="flex shrink-0">
        <InboxActorAvatar item={item} size="xs" />
      </span>
      <span className="min-w-0 truncate">
        <InboxActorLine item={item} detail={detail} />
      </span>
      <span className="shrink-0 tabular-nums text-faint-foreground @max-[38rem]/inbox-bar:hidden">· {timeAgo(item.created_at)}</span>
    </span>
  );
}

/** What happened to a settled notification, before the bar's actions. */
export function InboxBarStatus({ icon, label }: { icon: ReactNode; label: ReactNode }) {
  return (
    <span className="mr-1 flex min-w-0 flex-1 items-center gap-1 text-caption text-muted-foreground">
      <span aria-hidden className="flex shrink-0 [&_svg]:size-3.5">
        {icon}
      </span>
      <span className="truncate">{label}</span>
    </span>
  );
}

export function InboxBarSeparator() {
  return <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />;
}

/**
 * A notification action in the bar, named, with its shortcut beside it. A
 * narrow bar drops the name, then the shortcut; the tooltip keeps both.
 */
export function InboxBarButton({
  icon,
  label,
  shortcut,
  className,
  ...props
}: Omit<ComponentProps<typeof Button>, "children"> & {
  icon: ReactNode;
  label: string;
  shortcut?: ShortcutActionId;
}) {
  return (
    <TooltipHint label={label} shortcut={shortcut && <ActionShortcutKeycaps action={shortcut} />} side="bottom">
      <Button
        variant="ghost"
        size="sm"
        aria-label={label}
        className={cn("shrink-0 text-muted-foreground", className)}
        {...props}
      >
        {icon}
        <span className="@max-[44rem]/inbox-bar:hidden">{label}</span>
        {shortcut && (
          <span className="flex @max-[30rem]/inbox-bar:hidden">
            <ActionShortcutKeycaps action={shortcut} />
          </span>
        )}
      </Button>
    </TooltipHint>
  );
}

export function InboxArchiveButton({ onClick }: { onClick: () => void }) {
  const { t } = useT("inbox");
  return (
    <InboxBarButton
      icon={<Archive />}
      label={t(($) => $.context_menu.archive)}
      shortcut="archiveInboxItem"
      onClick={onClick}
    />
  );
}
