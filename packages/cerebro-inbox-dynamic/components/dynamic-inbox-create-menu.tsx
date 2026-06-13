"use client";

import { BellPlus, ListPlus, MessageSquarePlus, Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

export function DynamicInboxCreateMenu({
  onNewMessage,
  onNewIssue,
  onNewReminder,
  variant = "header",
}: {
  onNewMessage: () => void;
  onNewIssue: () => void;
  onNewReminder: () => void;
  variant?: "header" | "floating";
}) {
  const isFloating = variant === "floating";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className={cn(
              "inline-flex items-center justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              isFloating
                ? "h-14 w-14 rounded-full bg-primary text-primary-foreground shadow-lg ring-1 ring-primary/20 hover:bg-primary/90"
                : "h-10 min-w-10 gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium text-foreground shadow-sm hover:bg-muted",
            )}
            title="Create"
            aria-label="Create"
          />
        }
      >
        <Plus className={isFloating ? "size-6" : "size-4"} />
        {!isFloating && <span className="hidden sm:inline">Create</span>}
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        sideOffset={8}
        className="w-[min(calc(100vw-2rem),20rem)] p-2"
      >
        <DropdownMenuItem
          onClick={onNewMessage}
          className="min-h-12 gap-3 rounded-md px-3 py-3 text-base sm:text-sm"
        >
          <MessageSquarePlus className="size-5" />
          New message
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={onNewIssue}
          className="min-h-12 gap-3 rounded-md px-3 py-3 text-base sm:text-sm"
        >
          <ListPlus className="size-5" />
          New issue
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={onNewReminder}
          className="min-h-12 gap-3 rounded-md px-3 py-3 text-base sm:text-sm"
        >
          <BellPlus className="size-5" />
          New reminder
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
