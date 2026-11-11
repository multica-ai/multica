"use client";

import { useState, type ReactNode } from "react";
import { MoreHorizontal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { DeferredTooltip } from "@multica/ui/components/common/deferred-tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";
import { CLOSED_ROW_MENU, nextRowMenuState, usePointAnchor } from "../../common/point-menu";
import { useCollectionRowMenuState, type RowMenuState } from "./row-context";

/**
 * The row's ⋯ button and its menu. Inside a CollectionList (or any row that
 * provides CollectionRowContext) the same menu also opens on right-click,
 * anchored at the cursor, so both entry points always show identical items.
 * `triggerHidden` drops the button for rows that only open it on right-click.
 */
export function CollectionRowMenu({
  label,
  children,
  contentClassName,
  triggerClassName,
  triggerHidden = false,
  finalFocus,
}: {
  label?: string;
  children: ReactNode;
  contentClassName?: string;
  triggerClassName?: string;
  triggerHidden?: boolean;
  /** Where focus goes once the menu closes; see Base UI's `finalFocus`. */
  finalFocus?: () => boolean;
}) {
  const { t } = useT("layout");
  const row = useCollectionRowMenuState();
  const [local, setLocal] = useState<RowMenuState>(CLOSED_ROW_MENU);
  const menu = row?.menu ?? local;
  const setMenu = row?.setMenu ?? setLocal;
  const name = label ?? t(($) => $.collection.row_actions);

  const point = menu.point;
  const anchor = usePointAnchor(point);

  return (
    <DropdownMenu
      open={menu.open}
      onOpenChange={(open, details) => {
        const next = nextRowMenuState(menu, open, details.reason);
        if (next) setMenu(next);
      }}
    >
      {!triggerHidden && (
        <DeferredTooltip
          content={name}
          trigger={
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  aria-label={name}
                  data-collection-row-menu=""
                  className={cn(
                    "flex size-6 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity",
                    "hover:bg-accent hover:text-accent-foreground focus-visible:opacity-100",
                    "group-hover/row:opacity-100 group-data-menu-open/row:opacity-100",
                    "data-popup-open:bg-accent data-popup-open:text-accent-foreground data-popup-open:opacity-100",
                    "[@media(hover:none)]:opacity-100",
                    triggerClassName,
                  )}
                >
                  <MoreHorizontal className="size-4" />
                </button>
              }
            />
          }
        />
      )}
      <DropdownMenuContent
        align={point ? "start" : "end"}
        anchor={anchor}
        finalFocus={finalFocus}
        className={cn("w-48", contentClassName)}
      >
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
