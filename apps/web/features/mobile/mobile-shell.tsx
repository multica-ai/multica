"use client";

import { useState, type ReactNode } from "react";
import {
  ChevronLeft,
  Inbox,
  Kanban,
  ListTodo,
  Menu,
  MessageCircle,
  PanelsTopLeft,
  Server,
  Settings,
  X,
  type LucideIcon,
} from "lucide-react";
import { useRequiredWorkspaceSlug } from "@multica/core/paths";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { cn } from "@multica/ui/lib/utils";
import { DashboardGuard, WorkspacePresencePrefetch } from "@multica/views/layout";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  MOBILE_BOTTOM_NAV_ITEMS,
  MOBILE_DRAWER_NAV_ITEMS,
  type MobileRouteKey,
  mobileRoutes,
} from "./mobile-routes";

const MOBILE_NAV_ICONS: Record<MobileRouteKey, LucideIcon> = {
  kanban: Kanban,
  issues: ListTodo,
  projects: PanelsTopLeft,
  inbox: Inbox,
  runtime: Server,
  chat: MessageCircle,
  settings: Settings,
};

function isActiveRoute(pathname: string, href: string) {
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function MobileShell({ children }: { children: ReactNode }) {
  return (
    <DashboardGuard
      loadingFallback={
        <div className="flex h-svh items-center justify-center bg-background">
          <MulticaIcon className="size-6 animate-pulse" />
        </div>
      }
    >
      <WorkspacePresencePrefetch />
      <MobileShellFrame>{children}</MobileShellFrame>
    </DashboardGuard>
  );
}

export function MobileShellFrame({ children }: { children: ReactNode }) {
  const workspaceSlug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const routes = mobileRoutes(workspaceSlug);
  const [drawerOpen, setDrawerOpen] = useState(false);

  return (
    <div className="flex h-svh flex-col overflow-hidden overscroll-none bg-background text-foreground [&_button]:touch-manipulation [&_input]:text-base">
      <header className="shrink-0 border-b border-border bg-background/95 px-3 pb-2 pt-[max(env(safe-area-inset-top),0px)] backdrop-blur">
        <div className="flex min-h-14 items-center gap-2">
          <button
            type="button"
            aria-label="Go back"
            onClick={navigation.back}
            className="grid min-h-11 min-w-11 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <ChevronLeft className="size-5" aria-hidden="true" />
          </button>
          <div className="min-w-0 flex-1">
            <p className="truncate text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
              Workspace
            </p>
            <p className="truncate text-lg font-semibold leading-tight">
              {workspaceSlug}
            </p>
          </div>
          <button
            type="button"
            aria-label="Open mobile menu"
            onClick={() => setDrawerOpen(true)}
            className="grid min-h-11 min-w-11 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Menu className="size-5" aria-hidden="true" />
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {children}
      </div>

      <nav
        aria-label="Mobile primary"
        className="shrink-0 border-t border-border bg-background/95 px-2 pb-[max(env(safe-area-inset-bottom),0.5rem)] pt-2 backdrop-blur"
      >
        <div className="grid grid-cols-5 gap-1">
          {MOBILE_BOTTOM_NAV_ITEMS.map((item) => {
            const href = routes[item.routeKey];
            const active = isActiveRoute(navigation.pathname, href);
            const Icon = MOBILE_NAV_ICONS[item.routeKey];
            return (
              <AppLink
                key={item.routeKey}
                href={href}
                aria-label={`Open mobile ${item.label}`}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex min-h-12 flex-col items-center justify-center gap-1 rounded-md px-1 text-[11px] font-medium leading-none transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  active
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                <Icon className="size-5" aria-hidden="true" />
                <span className="max-w-full truncate">{item.label}</span>
              </AppLink>
            );
          })}
        </div>
      </nav>

      {drawerOpen ? (
        <div className="fixed inset-0 z-50">
          <button
            type="button"
            aria-label="Close mobile menu overlay"
            className="absolute inset-0 bg-background/70 backdrop-blur-sm"
            onClick={() => setDrawerOpen(false)}
          />
          <aside
            role="dialog"
            aria-modal="true"
            aria-label="Mobile menu"
            className="absolute bottom-0 right-0 top-0 flex w-[min(21rem,86vw)] flex-col border-l border-border bg-background pb-[max(env(safe-area-inset-bottom),1rem)] pt-[max(env(safe-area-inset-top),1rem)] shadow-2xl"
          >
            <div className="flex min-h-14 items-center gap-3 border-b border-border px-4">
              <div className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground">
                <MulticaIcon className="size-5" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
                  Current workspace
                </p>
                <p className="truncate text-base font-semibold">
                  {workspaceSlug}
                </p>
              </div>
              <button
                type="button"
                aria-label="Close mobile menu"
                onClick={() => setDrawerOpen(false)}
                className="grid min-h-11 min-w-11 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <X className="size-5" aria-hidden="true" />
              </button>
            </div>
            <div className="flex-1 space-y-6 overflow-y-auto px-3 py-4">
              <section aria-label="Secondary mobile routes" className="space-y-1">
                {MOBILE_DRAWER_NAV_ITEMS.map((item) => {
                  const href = routes[item.routeKey];
                  const active = isActiveRoute(navigation.pathname, href);
                  const Icon = MOBILE_NAV_ICONS[item.routeKey];
                  return (
                    <AppLink
                      key={item.routeKey}
                      href={href}
                      aria-current={active ? "page" : undefined}
                      onClick={() => setDrawerOpen(false)}
                      className={cn(
                        "flex min-h-11 items-center gap-3 rounded-md px-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        active
                          ? "bg-primary text-primary-foreground"
                          : "text-muted-foreground hover:bg-muted hover:text-foreground",
                      )}
                    >
                      <Icon className="size-5" aria-hidden="true" />
                      {item.label}
                    </AppLink>
                  );
                })}
              </section>
            </div>
          </aside>
        </div>
      ) : null}
    </div>
  );
}
