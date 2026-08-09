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
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
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

/**
 * An item that opens a submenu of mutually exclusive values instead of firing
 * an action — Text size (Small / Medium / Large), which FIR-4028 moved out of
 * the action row and into this menu. An empty `options` list is dropped like a
 * falsy item, so a caller can compute the list without guarding the menu.
 */
export type EditorActionChoice = {
  key: string;
  label: string;
  icon: LucideIcon;
  value: string;
  options: Array<{ value: string; label: string }>;
  onValueChange: (value: string) => void;
  separatorBefore?: boolean;
};

type MenuEntry = EditorActionItem | EditorActionChoice;

function isChoice(entry: MenuEntry): entry is EditorActionChoice {
  return "options" in entry;
}

export function EditorActionsMenu({
  items,
  align = "end",
  triggerLabel = "Actions",
  className,
}: {
  /** Falsy entries are skipped, so callers can use `cond && {...}` inline. */
  items: Array<MenuEntry | false | null | undefined>;
  align?: "start" | "center" | "end";
  /** Accessible label on the trigger button (e.g. "Note actions"). */
  triggerLabel?: string;
  className?: string;
}) {
  const visible = items.filter(
    (i): i is MenuEntry => Boolean(i) && (!isChoice(i as MenuEntry) || (i as EditorActionChoice).options.length > 0),
  );
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
            {isChoice(item) ? (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <item.icon className="size-4" />
                  {item.label}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  <DropdownMenuRadioGroup
                    value={item.value}
                    onValueChange={item.onValueChange}
                  >
                    {item.options.map((option) => (
                      <DropdownMenuRadioItem
                        key={option.value}
                        value={option.value}
                      >
                        {option.label}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ) : (
              <DropdownMenuItem
                variant={item.destructive ? "destructive" : undefined}
                onClick={item.onSelect}
              >
                <item.icon className="size-4" />
                {item.label}
              </DropdownMenuItem>
            )}
          </React.Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
