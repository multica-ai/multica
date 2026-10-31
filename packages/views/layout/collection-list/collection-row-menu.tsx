"use client";

import { useMemo, useState, type ReactNode } from "react";
import { MoreHorizontal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { DeferredTooltip } from "@multica/ui/components/common/deferred-tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";
import { useCollectionRowMenuState, type RowMenuState } from "./row-context";

/**
 * The row's ⋯ button and its menu. Inside a CollectionList the same menu also
 * opens on right-click, anchored at the cursor, so both entry points always
 * show identical items.
 */
export function CollectionRowMenu({
  label,
  children,
  contentClassName,
  triggerClassName,
}: {
  label?: string;
  children: ReactNode;
  contentClassName?: string;
  triggerClassName?: string;
}) {
  const { t } = useT("layout");
  const row = useCollectionRowMenuState();
  const [local, setLocal] = useState<RowMenuState>({ open: false, point: null });
  const menu = row?.menu ?? local;
  const setMenu = row?.setMenu ?? setLocal;
  const name = label ?? t(($) => $.collection.row_actions);

  const point = menu.point;
  const anchor = useMemo(() => {
    if (!point) return undefined;
    return {
      getBoundingClientRect: () => ({
        x: point.x,
        y: point.y,
        top: point.y,
        left: point.x,
        right: point.x,
        bottom: point.y,
        width: 0,
        height: 0,
        toJSON: () => ({}),
      }),
    };
  }, [point]);

  return (
    <DropdownMenu
      open={menu.open}
      onOpenChange={(open, details) => {
        // Base UI treats a menu its trigger did not press open as hover-opened
        // and closes it when the pointer leaves the trigger; a right-click menu
        // never touched the trigger.
        if (!open && point && details.reason === "trigger-hover") return;
        setMenu(open ? { open: true, point: null } : { open: false, point });
      }}
    >
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
      <DropdownMenuContent
        align={point ? "start" : "end"}
        anchor={anchor}
        className={cn("w-48", contentClassName)}
      >
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
