"use client";

import { StatusIcon } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";
import { Archive } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { useT } from "@multica/i18n/react";
import { InboxDetailLabel } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";

function timeAgo(
  dateStr: string,
  t: (key: string, params?: Record<string, string | number>) => string,
): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return t("time_just_now");
  if (minutes < 60) return t("time_minutes_short", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("time_hours_short", { count: hours });
  const days = Math.floor(hours / 24);
  return t("time_days_short", { count: days });
}

export { timeAgo };

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
  const t = useT("inbox");
  const displayTitle = getInboxDisplayTitle(item);

  return (
    <button
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
              title={t("archive")}
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
          <span className={`shrink-0 text-xs ${item.read ? "text-muted-foreground/60" : "text-muted-foreground"}`}>
            {timeAgo(item.created_at, t)}
          </span>
        </div>
      </div>
    </button>
  );
}
