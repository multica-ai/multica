"use client";

import type { ReactNode } from "react";
import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { useListDetailSplitStore } from "@multica/core/list-detail/stores";
import { Button } from "@multica/ui/components/ui/button";

interface ListDetailRailProps {
  /** Item count shown in the collapsed strip and next to the header toggle. */
  count: number;
  /** Aria label for the expand button rendered in the collapsed strip. */
  expandAriaLabel: string;
  /** Aria label for the collapse button rendered in the expanded header. */
  collapseAriaLabel: string;
  /** Count text rendered in the expanded header (already localized). */
  countBadge: ReactNode;
  /** The scrollable list of rows, rendered inside the expanded rail body. */
  children: ReactNode;
  /** Test-id prefix so each domain's rail is distinguishable in tests. */
  testIdPrefix: string;
}

/**
 * Left list rail shared by the Autopilots and Agents detail pages' two-column
 * layout. Reads the split store's `collapsed` flag and renders either the
 * narrow icon + count strip or the full list chrome (header toggle + count +
 * scrollable rows). The collapse state is persisted per workspace via the
 * store.
 */
export function ListDetailRail({
  count,
  expandAriaLabel,
  collapseAriaLabel,
  countBadge,
  children,
  testIdPrefix,
}: ListDetailRailProps) {
  const collapsed = useListDetailSplitStore((s) => s.collapsed);
  const toggleCollapsed = useListDetailSplitStore((s) => s.toggleCollapsed);

  if (collapsed) {
    return (
      <div
        className="flex h-full w-full flex-col items-center gap-2 py-2"
        data-testid={`${testIdPrefix}-collapsed`}
      >
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          aria-label={expandAriaLabel}
          onClick={toggleCollapsed}
          data-testid={`${testIdPrefix}-toggle`}
        >
          <PanelLeftOpen />
        </Button>
        <span className="text-micro text-muted-foreground tabular-nums">
          {count}
        </span>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-1.5">
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          aria-label={collapseAriaLabel}
          onClick={toggleCollapsed}
          data-testid={`${testIdPrefix}-toggle`}
        >
          <PanelLeftClose />
        </Button>
        <span className="pr-1 text-caption text-muted-foreground tabular-nums">
          {countBadge}
        </span>
      </div>
      <div
        className="flex-1 min-h-0 overflow-y-auto py-1"
        data-testid={`${testIdPrefix}-list`}
      >
        {children}
      </div>
    </div>
  );
}
