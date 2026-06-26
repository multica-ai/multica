"use client";

import { StatusIcon } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";
import { Archive } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { InboxDetailLabel } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useT } from "../../i18n";
import { DateTime } from "../../common/date-time";

export function InboxListItem({
  item,
  isSelected,
  onClick,
  onArchive,
}: {
  item: InboxItem;
  isSelected: boolean;
  onClick: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("inbox");
  const displayTitle = getInboxDisplayTitle(item);

  return (
    <button
      type="button"
      onClick={onClick}
      className={`group flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={28}
        enableHoverCard
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {!item.read && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate text-sm ${!item.read ? "font-medium" : "text-muted-foreground"}`}
            >
              {displayTitle}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <span
              role="button"
              tabIndex={-1}
              title={t(($) => $.list.archive_tooltip)}
              onClick={(e) => {
                e.stopPropagation();
                onArchive();
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation();
                  onArchive();
                }
              }}
              className="hidden rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground group-hover:inline-flex"
            >
              <Archive className="h-3.5 w-3.5" />
            </span>
            {item.issue_status && (
              <StatusIcon status={item.issue_status} className="h-3.5 w-3.5 shrink-0" />
            )}
          </div>
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p className={`min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs ${item.read ? "text-muted-foreground/60" : "text-muted-foreground"}`}>
            <InboxDetailLabel item={item} />
          </p>
          <DateTime
            value={item.created_at}
            variant="relative"
            className={`shrink-0 text-xs ${item.read ? "text-muted-foreground/60" : "text-muted-foreground"}`}
          />
        </div>
      </div>
    </button>
  );
}
