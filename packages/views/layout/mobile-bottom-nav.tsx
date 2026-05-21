"use client";

import { Inbox, ListTodo, MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../navigation";
import { useWorkspaceSlug, paths } from "@multica/core/paths";
import {
  chatSessionsOptions,
  pendingChatTasksOptions,
} from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../i18n";

// Bottom tab bar shown only on mobile (md:hidden). Fixed to the viewport
// bottom so it stays put while content scrolls; SidebarInset gets matching
// bottom padding so content cannot hide beneath the bar. env(safe-area-inset-bottom)
// keeps the row clear of the iOS home indicator in PWA / standalone mode.
//
// All three slots are AppLinks — Chat used to be a button that toggled an
// open/close store flag, which made tapping Inbox / Issues "do nothing"
// (chat overlay stayed on top) and made tapping Chat twice feel like a
// hidden minimize gesture. Treating Chat as a peer route means the bar
// behaves uniformly: every tap navigates, route change naturally hides the
// chat surface.
type LinkTab = {
  kind: "link";
  key: "inbox" | "issues" | "chat";
  labelKey: "inbox" | "issues" | "chat";
  icon: typeof Inbox;
};
type Tab = LinkTab;

const TABS: Tab[] = [
  { kind: "link", key: "inbox", labelKey: "inbox", icon: Inbox },
  { kind: "link", key: "issues", labelKey: "issues", icon: ListTodo },
  { kind: "link", key: "chat", labelKey: "chat", icon: MessageCircle },
];

// A tab stays active for any descendant route — e.g. /:slug/issues/AGE-1
// keeps the Issues tab lit. Strict equality alone would dim the bar the
// moment the user drills into a detail page, which would feel broken.
// Exported for unit-testing the matcher without rendering the nav.
export function isTabActive(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/");
}

export function MobileBottomNav() {
  const { t } = useT("layout");
  const { pathname } = useNavigation();
  const wsId = useWorkspaceId();
  // Same data the FAB consumed — keeps the unread / running pip behaviour
  // identical now that the FAB no longer renders on mobile. TanStack
  // dedupes by key so the chat-window query and this one share a request.
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const unreadSessionCount = sessions.filter((s) => s.has_unread).length;
  const isChatRunning = (pending?.tasks ?? []).length > 0;

  // Use slug + paths.workspace() rather than useWorkspacePaths(): the bar
  // mounts inside DashboardLayout which can briefly render before the
  // workspace context is populated, and we don't want to throw during that
  // gap. When slug is null the bar simply hides.
  const slug = useWorkspaceSlug();
  if (!slug) return null;
  const p = paths.workspace(slug);
  const chatHref = p.chat();
  const isOnChatRoute = isTabActive(pathname, chatHref);

  return (
    <nav
      aria-label="Primary"
      className="fixed inset-x-0 bottom-0 z-30 flex border-t bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80 md:hidden"
      style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
    >
      {TABS.map((tab) => {
        const Icon = tab.icon;
        const label = t(($) => $.nav[tab.labelKey]);
        const baseClass =
          "relative flex min-h-[3.25rem] flex-1 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px] leading-tight transition-colors";

        const href = p[tab.key]();
        const isActive = isTabActive(pathname, href);
        // Chat carries the unread / running affordances the FAB used to
        // own; the other two tabs are plain Inbox / Issues links.
        const isChatTab = tab.key === "chat";
        return (
          <AppLink
            key={tab.key}
            href={href}
            aria-current={isActive ? "page" : undefined}
            className={cn(
              baseClass,
              isActive
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground active:text-foreground",
            )}
          >
            <span className="relative inline-flex">
              <Icon
                className={cn(
                  "size-5 shrink-0",
                  isActive && "stroke-[2.5]",
                  isChatTab && isChatRunning && !isOnChatRoute && "animate-chat-impulse",
                )}
                aria-hidden="true"
              />
              {isChatTab && !isOnChatRoute && unreadSessionCount > 0 && (
                <span className="pointer-events-none absolute -top-1 -right-2 flex min-w-3.5 h-3.5 items-center justify-center rounded-full bg-brand px-1 text-[10px] font-semibold leading-none text-background">
                  {unreadSessionCount > 9 ? "9+" : unreadSessionCount}
                </span>
              )}
            </span>
            <span className="truncate">{label}</span>
          </AppLink>
        );
      })}
    </nav>
  );
}
