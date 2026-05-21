"use client";

import { Search } from "lucide-react";
import { SidebarMenuButton, useSidebar } from "@multica/ui/components/ui/sidebar";
import { isMac, formatShortcut, modKey } from "@multica/core/platform";
import { useSearchStore } from "./search-store";
import { useT } from "../i18n";

export function SearchTrigger() {
  const { t } = useT("search");
  // Close the mobile drawer at the same moment we open the command palette,
  // so the user lands directly on the palette with no overlay-on-overlay
  // limbo. Sidebar context is always present in this slot (rendered inside
  // <Sidebar> by AppSidebar). Desktop has openMobile=false; setting it to
  // false again is a no-op there.
  const { setOpenMobile } = useSidebar();
  return (
    <SidebarMenuButton
      // Mobile gets the same h-11 tap target as the rest of the nav rows.
      className="text-muted-foreground h-11 md:h-8"
      onClick={() => {
        setOpenMobile(false);
        useSearchStore.getState().setOpen(true);
      }}
    >
      <Search />
      <span>{t(($) => $.trigger.label)}</span>
      {/* Keyboard shortcut hint is irrelevant on touch devices. */}
      <kbd className="pointer-events-none ml-auto hidden h-5 select-none items-center gap-0.5 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground md:inline-flex">
        {isMac ? (
          <>
            <span className="text-xs">{modKey}</span>K
          </>
        ) : (
          formatShortcut(modKey, "K")
        )}
      </kbd>
    </SidebarMenuButton>
  );
}
