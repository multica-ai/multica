"use client";

import { Search } from "lucide-react";
import { SidebarMenuButton } from "@multica/ui/components/ui/sidebar";
import { isMac, formatShortcut, modKey } from "@multica/core/platform";
import { useSearchStore } from "./search-store";
import { useT } from "../i18n";

export function SearchTrigger() {
  const { t } = useT("search");
  return (
    <SidebarMenuButton
      className="text-muted-foreground"
      onClick={() => useSearchStore.getState().setOpen(true)}
    >
      <Search />
      <span>{t(($) => $.trigger.label)}</span>
      <kbd className="pointer-events-none ml-auto inline-flex h-5 select-none items-center gap-0.5 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
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
