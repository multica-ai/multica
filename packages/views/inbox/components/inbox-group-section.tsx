"use client";

import { useCallback } from "react";
import { ChevronDown, ChevronRight, CheckSquare, Square } from "lucide-react";
import type { InboxGroup } from "./inbox-grouping";
import type { ViewDensity } from "@multica/core/inbox/inbox-filter-store";
import { InboxListItem } from "./inbox-list-item";
import type { InboxItem } from "@multica/core/types";

export function InboxGroupSection({
  group,
  isCollapsed,
  density,
  selectedKey,
  selectedIds,
  showCheckbox,
  onToggleCollapse,
  onSelectAll,
  onDeselectAll,
  onSelectItem,
  onArchiveItem,
  onMarkReadItem,
  onOpenIssue,
  onToggleCheck,
}: {
  group: InboxGroup;
  isCollapsed: boolean;
  density: ViewDensity;
  selectedKey: string;
  selectedIds: Set<string>;
  showCheckbox: boolean;
  onToggleCollapse: () => void;
  onSelectAll: () => void;
  onDeselectAll: () => void;
  onSelectItem: (item: InboxItem) => void;
  onArchiveItem: (id: string) => void;
  onMarkReadItem: (id: string) => void;
  onOpenIssue: (issueId: string) => void;
  onToggleCheck: (id: string) => void;
}) {
  const allSelected =
    group.items.length > 0 &&
    group.items.every(
      (item) => selectedIds.has(item.issue_id ?? item.id),
    );

  const handleSelectAllToggle = useCallback(() => {
    if (allSelected) {
      onDeselectAll();
    } else {
      onSelectAll();
    }
  }, [allSelected, onSelectAll, onDeselectAll]);

  return (
    <div className="border-b last:border-b-0" role="group" aria-label={group.label}>
      {/* Group header */}
      <button
        type="button"
        onClick={onToggleCollapse}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs font-medium text-muted-foreground hover:bg-accent/50 transition-colors"
        aria-expanded={!isCollapsed}
      >
        {/* Chevron */}
        {isCollapsed ? (
          <ChevronRight className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        )}

        {/* Group label */}
        <span className="flex-1 text-left">{group.label}</span>

        {/* Unread badge */}
        {group.unreadCount > 0 && (
          <span className="rounded-full bg-brand/10 px-1.5 py-0.5 text-[10px] font-semibold text-brand">
            {group.unreadCount}
          </span>
        )}

        {/* Total count */}
        <span className="text-[10px] text-muted-foreground/50">
          {group.totalCount}
        </span>

        {/* Select-all checkbox for this group */}
        {showCheckbox && (
          <span
            role="checkbox"
            aria-checked={allSelected}
            tabIndex={-1}
            onClick={(e) => {
              e.stopPropagation();
              handleSelectAllToggle();
            }}
            className="shrink-0 cursor-pointer text-muted-foreground hover:text-foreground"
          >
            {allSelected ? (
              <CheckSquare className="h-3.5 w-3.5 text-brand" />
            ) : (
              <Square className="h-3.5 w-3.5" />
            )}
          </span>
        )}
      </button>

      {/* Group items */}
      {!isCollapsed && (
        <div>
          {group.items.map((item) => {
            const itemKey = item.issue_id ?? item.id;
            return (
              <InboxListItem
                key={item.id}
                item={item}
                isSelected={itemKey === selectedKey}
                isChecked={selectedIds.has(itemKey)}
                showCheckbox={showCheckbox}
                density={density}
                onClick={() => onSelectItem(item)}
                onArchive={() => onArchiveItem(item.id)}
                onMarkRead={() => onMarkReadItem(item.id)}
                onOpenIssue={() => item.issue_id && onOpenIssue(item.issue_id)}
                onToggleCheck={() => onToggleCheck(itemKey)}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
