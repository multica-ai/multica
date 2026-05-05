"use client";

import { ArrowUpRight, BookOpen, CircleHelp, History, MessageCircle } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useModalStore } from "@multica/core/modals";
import { useT } from "@multica/i18n/react";
import { openExternal, publicAppUrl } from "../platform";

export function HelpLauncher() {
  const t = useT("help");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t("label")}
        title={t("label")}
        className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
      >
        <CircleHelp className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        side="top"
        sideOffset={8}
        className="min-w-40"
      >
        <DropdownMenuItem
          onClick={() => openExternal(publicAppUrl("/docs"))}
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t("docs")}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => openExternal(publicAppUrl("/changelog"))}
        >
          <History className="h-3.5 w-3.5" />
          {t("changelog")}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => useModalStore.getState().open("feedback")}
        >
          <MessageCircle className="h-3.5 w-3.5" />
          {t("feedback")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
