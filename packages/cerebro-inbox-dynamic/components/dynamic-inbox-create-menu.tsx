"use client";

import type { LucideIcon } from "lucide-react";
import { BellPlus, ListPlus, MessageSquarePlus, Plus } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

type CreateAction = {
  icon: LucideIcon;
  title: string;
  description: string;
  onSelect: () => void;
};

function CreateMenuItem({ icon: Icon, title, description, onSelect }: CreateAction) {
  return (
    <DropdownMenuItem
      onClick={onSelect}
      className="group items-center gap-3 rounded-lg px-2 py-2"
    >
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary transition-colors group-data-[highlighted]:bg-primary group-data-[highlighted]:text-primary-foreground">
        <Icon className="size-[18px]" />
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="text-sm font-medium leading-tight text-foreground">{title}</span>
        <span className="truncate text-xs leading-tight text-muted-foreground">
          {description}
        </span>
      </span>
    </DropdownMenuItem>
  );
}

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

  const actions: CreateAction[] = [
    {
      icon: MessageSquarePlus,
      title: "New message",
      description: "Send a direct message",
      onSelect: onNewMessage,
    },
    {
      icon: ListPlus,
      title: "New issue",
      description: "Create a task or ticket",
      onSelect: onNewIssue,
    },
    {
      icon: BellPlus,
      title: "New reminder",
      description: "Get nudged at a set time",
      onSelect: onNewReminder,
    },
  ];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className={cn(
              "inline-flex items-center justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              isFloating
                ? "h-14 w-14 rounded-full bg-primary text-primary-foreground shadow-lg ring-1 ring-primary/20 transition-transform hover:bg-primary/90 active:scale-95"
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
        side={isFloating ? "top" : "bottom"}
        sideOffset={isFloating ? 12 : 8}
        className="w-[min(calc(100vw-2rem),20rem)] rounded-xl p-1.5 shadow-lg"
      >
        <DropdownMenuGroup>
          <DropdownMenuLabel className="px-2 pb-1 pt-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Create
          </DropdownMenuLabel>
          {actions.map((action) => (
            <CreateMenuItem key={action.title} {...action} />
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
