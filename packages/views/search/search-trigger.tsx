"use client";

import { Search } from "lucide-react";
import { SidebarMenuButton } from "@multica/ui/components/ui/sidebar";
import { useT } from "@multica/i18n/react";
import { useSearchStore } from "./search-store";

export function SearchTrigger() {
  const t = useT("search");
  return (
    <SidebarMenuButton
      className="text-muted-foreground"
      onClick={() => useSearchStore.getState().setOpen(true)}
    >
      <Search />
      <span>{t("trigger_label")}</span>
      <kbd className="pointer-events-none ml-auto inline-flex h-5 select-none items-center gap-0.5 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground">
        <span className="text-xs">⌘</span>K
      </kbd>
    </SidebarMenuButton>
  );
}
