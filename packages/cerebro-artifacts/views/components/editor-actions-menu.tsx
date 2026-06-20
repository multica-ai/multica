"use client";

import * as React from "react";
import type { LucideIcon } from "lucide-react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

/**
 * One shared "⋯" actions menu for both the Documents view (DocumentViewPage)
 * and the Notes view (NoteEditor). Each surface builds its own list of actions
 * (notes have Pin, documents have Download / Open in new window, etc.), but the
 * menu chrome — trigger button, alignment, destructive styling, separators —
 * is identical so the two surfaces look and behave the same way (FIR-1647,
 * request 5).
 *
 * Pass a flat list of items. Falsy entries are ignored, so callers can inline
 * `condition && { ... }` without juggling arrays. An item with
 * `separatorBefore` renders a divider above it (used to fence off destructive
 * actions like Delete). The menu renders nothing when no items survive.
 */
export type EditorActionItem = {
  /** Stable key for React reconciliation. */
  key: string;
  label: string;
  icon: LucideIcon;
  onSelect: () => void;
  /** Renders the item in the destructive (red) style, e.g. Delete. */
  destructive?: boolean;
  /** Draws a separator above this item. */
  separatorBefore?: boolean;
};

export function EditorActionsMenu({
  items,
  align = "end",
  triggerLabel = "Actions",
  className,
}: {
  /** Falsy entries are skipped, so callers can use `cond && {...}` inline. */
  items: Array<EditorActionItem | false | null | undefined>;
  align?: "start" | "center" | "end";
  /** Accessible label on the trigger button (e.g. "Note actions"). */
  triggerLabel?: string;
  className?: string;
}) {
  const visible = items.filter((i): i is EditorActionItem => Boolean(i));
  if (visible.length === 0) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            size="icon-sm"
            variant="ghost"
            className={cn("shrink-0", className)}
            aria-label={triggerLabel}
          >
            <MoreHorizontal className="size-4" />
          </Button>
        }
      />
      <DropdownMenuContent align={align} className="w-52">
        {visible.map((item) => (
          <React.Fragment key={item.key}>
            {item.separatorBefore && <DropdownMenuSeparator />}
            <DropdownMenuItem
              variant={item.destructive ? "destructive" : undefined}
              onClick={item.onSelect}
            >
              <item.icon className="size-4" />
              {item.label}
            </DropdownMenuItem>
          </React.Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
