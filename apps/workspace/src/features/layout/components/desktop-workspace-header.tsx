"use client";

import { Search, SquarePen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { useModalStore } from "@/features/modals";
import { useSearchStore } from "@/features/search";
import { PomodoroStatusPill } from "@/features/time-tracking";
import { usePathname } from "@/shared/router";
import { getWorkspacePageTitle } from "../navigation";

/**
 * Shows a small draft indicator on shell-level issue creation controls.
 */
function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-1 right-1 size-1.5 rounded-full bg-brand" />;
}

/**
 * Renders the desktop shell header with page title, global actions, and active pomodoro status.
 */
export function DesktopWorkspaceHeader() {
  const pathname = usePathname();
  const pageTitle = getWorkspacePageTitle(pathname);

  return (
    <header className="hidden h-14 shrink-0 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur md:flex">
      <div className="min-w-0 flex-1">
        <h1 className="truncate text-sm font-semibold text-foreground">{pageTitle}</h1>
      </div>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              className="text-muted-foreground"
              onClick={() => useSearchStore.getState().open()}
            >
              <Search className="size-3.5" />
              <span>Search</span>
              <kbd className="ml-1 rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                Cmd K
              </kbd>
            </Button>
          }
        />
        <TooltipContent side="bottom">Search</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              size="sm"
              className="relative"
              onClick={() => useModalStore.getState().open("create-issue")}
            >
              <SquarePen className="size-3.5" />
              <span>New issue</span>
              <DraftDot />
            </Button>
          }
        />
        <TooltipContent side="bottom">New issue</TooltipContent>
      </Tooltip>

      <PomodoroStatusPill />
    </header>
  );
}
