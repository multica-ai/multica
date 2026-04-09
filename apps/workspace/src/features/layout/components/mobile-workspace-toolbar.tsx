"use client";

import { SquarePen } from "lucide-react";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { useModalStore } from "@/features/modals";
import { useWorkspaceStore } from "@/features/workspace";

function DraftDot() {
  const hasDraft = useIssueDraftStore((s) => !!(s.draft.title || s.draft.description));
  if (!hasDraft) return null;
  return <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-brand" />;
}

export function MobileWorkspaceToolbar() {
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);

  return (
    <div className="sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur md:hidden">
      <SidebarTrigger
        aria-label="Open navigation"
        className="text-muted-foreground"
      />
      <div className="min-w-0 flex-1">
        <span className="block text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          Workspace
        </span>
        <span className="block truncate text-sm font-semibold">
          {workspaceName ?? "Workspace"}
        </span>
      </div>
      <Tooltip>
        <TooltipTrigger
          className="relative flex h-7 w-7 items-center justify-center rounded-lg bg-muted text-foreground hover:bg-accent"
          aria-label="New issue"
          onClick={() => useModalStore.getState().open("create-issue")}
        >
          <SquarePen className="size-3.5" />
          <DraftDot />
        </TooltipTrigger>
        <TooltipContent side="bottom">New issue</TooltipContent>
      </Tooltip>
    </div>
  );
}
