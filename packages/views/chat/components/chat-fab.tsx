"use client";

import { MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { createLogger } from "@multica/core/logger";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";

const logger = createLogger("chat.ui");

export function ChatFab() {
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const toggle = useChatStore((s) => s.toggle);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));

  if (isOpen) return null;

  const unreadSessionCount = sessions.filter((s) => s.has_unread).length;
  const isRunning = (pending?.tasks ?? []).length > 0;

  const handleClick = () => {
    logger.info("fab.click (open chat)", { unreadSessionCount, isRunning });
    toggle();
  };

  // Tooltip text communicates the state that isn't carried by the icon/badge.
  const tooltip = isRunning
    ? "Multica is working..."
    : unreadSessionCount > 0
      ? `${unreadSessionCount} unread ${unreadSessionCount === 1 ? "chat" : "chats"}`
      : "Ask Multica";

  return (
    <Tooltip>
      <TooltipTrigger
        onClick={handleClick}
        className={cn(
          "absolute bottom-2 right-2 z-50 flex size-10 cursor-pointer items-center justify-center rounded-full ring-1 ring-foreground/10 bg-card text-muted-foreground shadow-sm transition-transform hover:scale-110 hover:text-accent-foreground active:scale-95",
          // Impulse the button itself while a chat task is running — no
          // outer ring to keep things calm.
          isRunning && "animate-chat-impulse",
        )}
      >
        <MessageCircle className="size-5" />
        {unreadSessionCount > 0 && (
          <span className="pointer-events-none absolute -top-0.5 -right-0.5 flex min-w-4 h-4 items-center justify-center rounded-full bg-brand px-1 text-xs font-semibold leading-none text-background">
            {unreadSessionCount > 9 ? "9+" : unreadSessionCount}
          </span>
        )}
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={10}>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
