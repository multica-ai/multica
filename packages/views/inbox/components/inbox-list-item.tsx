"use client";

import { useCallback } from "react";
import { StatusIcon } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";
import { Archive, Eye, ExternalLink, CheckSquare, Square } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import type { ViewDensity } from "@multica/core/inbox/inbox-filter-store";
import { InboxDetailLabel } from "./inbox-detail-label";
import { getInboxDisplayTitle } from "./inbox-display";
import { useTimeAgo } from "./inbox-list-item-hooks";
import { useT } from "../../i18n";

// Severity color bar mapping: left edge accent strip
const SEVERITY_COLORS: Record<string, string> = {
  action_required: "bg-red-500",
  attention: "bg-amber-500",
  info: "bg-muted-foreground/30",
};

export function InboxListItem({
  item,
  isSelected,
  isChecked,
  showCheckbox,
  density,
  onClick,
  onArchive,
  onMarkRead,
  onOpenIssue,
  onToggleCheck,
}: {
  item: InboxItem;
  isSelected: boolean;
  isChecked?: boolean;
  showCheckbox?: boolean;
  density: ViewDensity;
  onClick: () => void;
  onArchive: () => void;
  onMarkRead: () => void;
  onOpenIssue: () => void;
  onToggleCheck?: () => void;
}) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();
  const displayTitle = getInboxDisplayTitle(item);
  const severityColor = SEVERITY_COLORS[item.severity] ?? SEVERITY_COLORS.info;

  const isCompact = density === "compact";
  const py = isCompact ? "py-1.5" : "py-2.5";
  const px = isCompact ? "px-2" : "px-4";
  const avatarSize = isCompact ? 24 : 28;
  const titleSize = isCompact ? "text-xs" : "text-sm";
  const metaSize = isCompact ? "text-[10px]" : "text-xs";

  const handleArchive = useCallback(
    (e: React.MouseEvent | React.KeyboardEvent) => {
      e.stopPropagation();
      onArchive();
    },
    [onArchive],
  );

  const handleMarkRead = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onMarkRead();
    },
    [onMarkRead],
  );

  const handleOpenIssue = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onOpenIssue();
    },
    [onOpenIssue],
  );

  const handleCheckbox = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      onToggleCheck?.();
    },
    [onToggleCheck],
  );

  return (
    <button
      type="button"
      onClick={onClick}
      className={`group relative flex w-full items-center gap-2 ${px} ${py} text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
      role="option"
      aria-selected={isSelected}
      aria-label={`${displayTitle}${!item.read ? ", unread" : ""}`}
    >
      {/* Severity color bar — left edge accent */}
      <span
        className={`absolute left-0 top-0 h-full w-0.5 ${severityColor}`}
        aria-hidden="true"
      />

      {/* Multi-select checkbox */}
      {showCheckbox && (
        <span
          role="checkbox"
          aria-checked={isChecked}
          tabIndex={-1}
          onClick={handleCheckbox}
          className="shrink-0 cursor-pointer text-muted-foreground hover:text-foreground"
        >
          {isChecked ? (
            <CheckSquare className="h-4 w-4 text-brand" />
          ) : (
            <Square className="h-4 w-4" />
          )}
        </span>
      )}

      {/* Avatar */}
      <ActorAvatar
        actorType={item.actor_type ?? item.recipient_type}
        actorId={item.actor_id ?? item.recipient_id}
        size={avatarSize}
        enableHoverCard
      />

      {/* Content: 3-tier info hierarchy */}
      <div className="min-w-0 flex-1">
        {/* Tier 1: Title + unread indicator */}
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {!item.read && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate ${titleSize} ${
                !item.read ? "font-medium" : "text-muted-foreground"
              }`}
            >
              {displayTitle}
            </span>
          </div>

          {/* Tier 3: Hover-revealed actions */}
          <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
            {item.read && (
              <span
                role="button"
                tabIndex={-1}
                title={t(($) => $.list.mark_read_tooltip)}
                onClick={handleMarkRead}
                className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <Eye className="h-3.5 w-3.5" />
              </span>
            )}
            <span
              role="button"
              tabIndex={-1}
              title={t(($) => $.list.archive_tooltip)}
              onClick={handleArchive}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation();
                  onArchive();
                }
              }}
              className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              <Archive className="h-3.5 w-3.5" />
            </span>
            {item.issue_id && (
              <span
                role="button"
                tabIndex={-1}
                title={t(($) => $.list.open_issue_tooltip)}
                onClick={handleOpenIssue}
                className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </span>
            )}
          </div>

          {/* Status icon (always visible) */}
          <div className="flex shrink-0 items-center gap-1">
            {item.issue_status && (
              <StatusIcon status={item.issue_status} className="h-3.5 w-3.5 shrink-0" />
            )}
          </div>
        </div>

        {/* Tier 2: Type-aware detail + timestamp */}
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p
            className={`min-w-0 overflow-hidden text-ellipsis whitespace-nowrap ${metaSize} ${
              item.read ? "text-muted-foreground/60" : "text-muted-foreground"
            }`}
          >
            <InboxDetailLabel item={item} />
          </p>
          <span
            className={`shrink-0 ${metaSize} ${
              item.read ? "text-muted-foreground/60" : "text-muted-foreground"
            }`}
          >
            {timeAgo(item.created_at)}
          </span>
        </div>
      </div>
    </button>
  );
}
