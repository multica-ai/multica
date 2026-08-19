"use client";

import { useState } from "react";
import { CalendarRange, Search } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Calendar } from "@multica/ui/components/ui/calendar";
import { dateOnlyToLocalDate, formatDateOnlyShort, toDateOnly } from "@/lib/date";
import type { WorkspaceStatus } from "@/lib/types";

type StatusFilter = WorkspaceStatus | "all";

const STATUS_ITEMS: Record<StatusFilter, string> = {
  all: "All statuses",
  active: "Active",
  idle: "Idle",
  error: "Error",
};

interface ToolbarProps {
  search: string;
  onSearchChange: (value: string) => void;
  status: StatusFilter;
  onStatusChange: (value: StatusFilter) => void;
  /** 1-based index of the first row on the current page, e.g. 1 or 26. */
  rangeStart: number;
  /** 1-based index of the last row on the current page. */
  rangeEnd: number;
  total: number;
  /** Plan §3.2: "Date range picker for 'Last Activity' filtering". Inclusive
   * "YYYY-MM-DD" bounds; undefined means unset on that side. */
  activityFrom?: string;
  activityTo?: string;
  onActivityRangeChange: (range: { from?: string; to?: string }) => void;
}

export function Toolbar({
  search,
  onSearchChange,
  status,
  onStatusChange,
  rangeStart,
  rangeEnd,
  total,
  activityFrom,
  activityTo,
  onActivityRangeChange,
}: ToolbarProps) {
  // Controlled so "Apply" can dismiss the calendar while leaving the applied
  // filter in place — same pattern as issues-header.tsx's DateSubContent.
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<{ from: Date | undefined; to?: Date }>({
    from: dateOnlyToLocalDate(activityFrom),
    to: dateOnlyToLocalDate(activityTo),
  });
  const hasActivityRange = activityFrom !== undefined || activityTo !== undefined;

  // Re-sync the draft from the applied filter every time the popover opens,
  // so an external reset (e.g. the table empty-state's "Clear filters") is
  // reflected instead of showing a stale draft from a previous open.
  function handleOpenChange(next: boolean) {
    if (next) {
      setDraft({ from: dateOnlyToLocalDate(activityFrom), to: dateOnlyToLocalDate(activityTo) });
    }
    setOpen(next);
  }

  function applyDraft() {
    onActivityRangeChange({
      from: draft.from ? toDateOnly(draft.from) : undefined,
      to: draft.to ? toDateOnly(draft.to) : undefined,
    });
    setOpen(false);
  }

  function clearActivityRange() {
    setDraft({ from: undefined, to: undefined });
    onActivityRangeChange({ from: undefined, to: undefined });
  }
  return (
    <div className="flex flex-wrap items-center gap-3 py-4">
      <div className="relative min-w-64 flex-1">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder="Search by name, owner, or model"
          className="pl-8"
          aria-label="Search workspaces"
        />
      </div>
      {/* Select requires an `items` label map — passing raw children only
          renders the underlying value, per packages/ui/components/ui/select.tsx.
          Base UI's onValueChange can report `null` (e.g. cleared/no match) even
          though we never render an unset option here — fall back to "all"
          rather than widening StatusFilter's type to include null. */}
      <Select
        items={STATUS_ITEMS}
        value={status}
        onValueChange={(value) => onStatusChange(value ?? "all")}
      >
        <SelectTrigger aria-label="Filter by status">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {(Object.keys(STATUS_ITEMS) as StatusFilter[]).map((key) => (
            <SelectItem key={key} value={key}>
              {STATUS_ITEMS[key]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Popover open={open} onOpenChange={handleOpenChange}>
        <PopoverTrigger
          render={
            <Button variant="outline" size="sm" className="gap-1.5">
              <CalendarRange className="size-4" aria-hidden />
              {hasActivityRange
                ? `${activityFrom ? formatDateOnlyShort(activityFrom) : "…"} – ${
                    activityTo ? formatDateOnlyShort(activityTo) : "…"
                  }`
                : "Last activity"}
            </Button>
          }
        />
        <PopoverContent align="start" className="w-auto gap-0 p-0">
          <Calendar
            mode="range"
            selected={draft}
            onSelect={(next) => setDraft(next ?? { from: undefined, to: undefined })}
            captionLayout="dropdown"
          />
          <div className="flex items-center justify-between gap-2 border-t p-2">
            <Button variant="ghost" size="sm" onClick={clearActivityRange} disabled={!hasActivityRange && !draft.from}>
              Clear
            </Button>
            <Button size="sm" onClick={applyDraft} disabled={!draft.from}>
              Apply
            </Button>
          </div>
        </PopoverContent>
      </Popover>
      {/* Plan §3.4: "Showing 1–25 of 142 workspaces". */}
      <span className="ml-auto text-body text-muted-foreground">
        {total === 0
          ? "Showing 0 of 0 workspaces"
          : `Showing ${rangeStart}–${rangeEnd} of ${total} workspaces`}
      </span>
    </div>
  );
}
