"use client";

import { Fragment, type KeyboardEvent, type ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

export interface RowActionItem {
  key: string;
  icon: ReactNode;
  label: string;
  onSelect: () => void;
  danger?: boolean;
}

/**
 * Enter/Space activation for a `role="button"` row that hosts its own
 * controls (this menu, inline buttons): a key pressed inside one of those
 * controls belongs to that control, not to the row.
 */
export function handleRowActivationKey(
  e: KeyboardEvent<HTMLElement>,
  activate: () => void,
) {
  if (e.target !== e.currentTarget) return;
  if (e.key !== "Enter" && e.key !== " ") return;
  e.preventDefault();
  activate();
}

/**
 * A list row's actions, collected behind one always-visible trigger.
 *
 * This is the touch counterpart of the hover controls list rows reveal on a
 * pointer surface: a coarse pointer has neither hover nor right-click, so it
 * would otherwise never reach them. It renders where the primary pointer
 * cannot hover and hides where it can, letting the hover controls take over —
 * the two must therefore offer the same actions.
 *
 * The switch is `hover`, not a width breakpoint: a phone in landscape clears
 * `md` while still having no hover, which would otherwise leave it with the
 * hover controls it can never trigger.
 *
 * `groups` are rendered in order with a separator between them.
 */
export function RowActionsMenu({
  label,
  groups,
}: {
  /** Accessible name of the trigger, e.g. "Chat actions". */
  label: string;
  groups: RowActionItem[][];
}) {
  const filled = groups.filter((group) => group.length > 0);
  if (filled.length === 0) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={label}
            // The row owns click-to-select; opening the menu must not select it.
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
            className="inline-flex size-8 shrink-0 items-center justify-center rounded text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring data-popup-open:bg-accent data-popup-open:text-foreground [@media(hover:hover)]:hidden"
          >
            <MoreHorizontal className="size-4" />
          </button>
        }
      />
      <DropdownMenuContent align="end" className="w-auto">
        {filled.map((group, index) => (
          <Fragment key={group[0]!.key}>
            {index > 0 && <DropdownMenuSeparator />}
            {group.map((item) => (
              <DropdownMenuItem
                key={item.key}
                variant={item.danger ? "destructive" : "default"}
                onClick={item.onSelect}
              >
                {item.icon}
                {item.label}
              </DropdownMenuItem>
            ))}
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
