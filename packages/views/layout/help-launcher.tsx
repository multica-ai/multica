"use client";

import { ArrowUpRight, BookOpen, CircleHelp, History, MessageCircle } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useModalStore } from "@multica/core/modals";
import { useT } from "../i18n";

const DOCS_URL = "https://multica.ai/docs";
const CHANGELOG_URL = "https://multica.ai/changelog";

export function HelpLauncher() {
  const { t } = useT("layout");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.help.trigger)}
        title={t(($) => $.help.trigger)}
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
          render={
            <a href={DOCS_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a
              href={CHANGELOG_URL}
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          <History className="h-3.5 w-3.5" />
          {t(($) => $.help.changelog)}
          <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/50" />
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => useModalStore.getState().open("feedback")}
        >
          <MessageCircle className="h-3.5 w-3.5" />
          {t(($) => $.help.feedback)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
