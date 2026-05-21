"use client";

import { MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { createLogger } from "@multica/core/logger";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");

export function ChatFab() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const toggle = useChatStore((s) => s.toggle);
  const isMobile = useIsMobile();
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));

  // On mobile the chat lives in the bottom tab bar (MobileBottomNav) so the
  // FAB would duplicate that affordance and — worse — overlap the comment
  // input's send / paperclip buttons on issue-detail. Hide it entirely;
  // SSR-safe because useIsMobile returns false during render.
  if (isMobile) return null;
  if (isOpen) return null;

  const unreadSessionCount = sessions.filter((s) => s.has_unread).length;
  const isRunning = (pending?.tasks ?? []).length > 0;

  const handleClick = () => {
    logger.info("fab.click (open chat)", { unreadSessionCount, isRunning });
    toggle();
  };

  // Tooltip text communicates the state that isn't carried by the icon/badge.
  const tooltip = isRunning
    ? t(($) => $.fab.running)
    : unreadSessionCount > 0
      ? t(($) => $.fab.unread, { count: unreadSessionCount })
      : t(($) => $.fab.default);

  return (
    <Tooltip>
      <TooltipTrigger
        onClick={handleClick}
        className={cn(
          // bottom offset: 0.5rem on desktop; lift above the mobile bottom
          // tab bar (3.25rem) + iOS safe-area on mobile so the FAB stays
          // tappable. Tailwind v4 arbitrary value with calc + env() works
          // because the property collapses to a literal calc() expression.
          "absolute right-2 z-50 bottom-[calc(0.5rem+3.25rem+env(safe-area-inset-bottom))] md:bottom-2 flex size-10 cursor-pointer items-center justify-center rounded-full ring-1 ring-foreground/10 bg-card text-muted-foreground shadow-sm transition-transform hover:scale-110 hover:text-accent-foreground active:scale-95",
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
