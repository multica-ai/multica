// CEREBRO-PATCH(cerebro-chat-nav-item): FIR-4350 — the "Chat" entry in the left
// sidebar, with its own unread badge. Gated by cerebro_chat_page so the surface
// is invisible until the user enables it.
//
// Lives here rather than in @multica/cerebro-chat-page (path 2 of the CLAUDE.md
// Cerebro Extension Discipline: a cerebro- prefixed sibling that the upstream
// file imports via one marked line). A home in cerebro-chat-page would make
// views depend on that package, which depends on the Slack block, which depends
// on views — a package cycle turbo refuses to order.
"use client";

import { MessagesSquare } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useChatUnreadCount, useFeatureFlag } from "@multica/cerebro-feature-flags";
import { AppLink, useNavigation } from "../navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";


interface ChatNavItemProps {
  workspaceSlug: string;
  wsId?: string | null;
  onClick?: () => void;
}

export function ChatNavItem({ workspaceSlug, wsId, onClick }: ChatNavItemProps) {
  const enabled = useFeatureFlag("cerebro_chat_page");
  const { pathname } = useNavigation();
  const unread = useChatUnreadCount(enabled ? wsId : null);

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/chat`;
  const isActive = pathname === href || pathname.startsWith(href + "/");

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isActive}
        render={<AppLink href={href} />}
        onClick={onClick}
        tooltip="Chat"
        className={cn(
          "text-muted-foreground hover:not-data-active:bg-sidebar-accent/70",
          "data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
        )}
      >
        <MessagesSquare />
        <span>Chat</span>
        {unread > 0 && (
          <span className="ml-auto text-xs">{unread > 99 ? "99+" : unread}</span>
        )}
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
