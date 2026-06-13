"use client";

import { BellPlus, ListPlus, MessageSquarePlus, Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

export function DynamicInboxCreateMenu({
  onNewMessage,
  onNewIssue,
  onNewReminder,
}: {
  onNewMessage: () => void;
  onNewIssue: () => void;
  onNewReminder: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            title="Create"
            aria-label="Create"
          />
        }
      >
        <Plus className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={onNewMessage}>
          <MessageSquarePlus className="mr-2 size-4" />
          New message
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onNewIssue}>
          <ListPlus className="mr-2 size-4" />
          New issue
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onNewReminder}>
          <BellPlus className="mr-2 size-4" />
          New reminder
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
