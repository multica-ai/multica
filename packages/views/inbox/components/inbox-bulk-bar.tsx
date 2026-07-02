"use client";

import { Archive, CheckCheck, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function InboxBulkBar({
  selectedCount,
  onMarkReadSelected,
  onArchiveSelected,
  onClearSelection,
}: {
  selectedCount: number;
  onMarkReadSelected: () => void;
  onArchiveSelected: () => void;
  onClearSelection: () => void;
}) {
  const { t } = useT("inbox");

  if (selectedCount === 0) return null;

  return (
    <div
      className="flex items-center gap-3 border-t bg-accent/50 px-4 py-2"
      role="toolbar"
      aria-label={t(($) => $.bulk.aria_label)}
    >
      {/* Selected count */}
      <span className="text-xs font-medium text-muted-foreground">
        {t(($) => $.bulk.selected_count, { count: selectedCount })}
      </span>

      <div className="flex-1" />

      {/* Mark read selected */}
      <Button
        variant="ghost"
        size="sm"
        onClick={onMarkReadSelected}
        className="h-7 gap-1.5 text-xs"
      >
        <CheckCheck className="h-3.5 w-3.5" />
        {t(($) => $.bulk.mark_read_selected)}
      </Button>

      {/* Archive selected */}
      <Button
        variant="ghost"
        size="sm"
        onClick={onArchiveSelected}
        className="h-7 gap-1.5 text-xs"
      >
        <Archive className="h-3.5 w-3.5" />
        {t(($) => $.bulk.archive_selected)}
      </Button>

      {/* Clear selection */}
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onClearSelection}
        className="h-7 w-7 text-muted-foreground"
        aria-label={t(($) => $.bulk.clear_selection)}
      >
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}
