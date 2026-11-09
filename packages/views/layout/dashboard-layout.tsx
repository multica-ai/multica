"use client";

import type { ReactNode } from "react";
import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "../modals/registry";
import { SourceBackfillModal } from "../onboarding";
import { AppRail } from "./app-rail";
import { AppShellProvider } from "./app-shell-context";
import { CompactNavSheet } from "./compact-nav-sheet";
import { DashboardGuard } from "./dashboard-guard";
import { NavigationProgress } from "./navigation-progress";
import { ShellRouteDataPrefetch } from "./route-data-prefetch";
import { SpacePanel } from "./space-panel";
import { WorkspacePresencePrefetch } from "./workspace-presence-prefetch";
import { GlobalShortcuts } from "./global-shortcuts";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Rendered inside the content column (e.g. ChatWindow, ChatFab — absolute-positioned overlays) */
  extra?: ReactNode;
  /** Loading indicator */
  loadingIndicator?: ReactNode;
}

/**
 * The content card: the space panel and the page share one surface on the
 * app shell, split by a single hairline. Below `md` the card is the whole
 * window. Desktop builds the same card in its own layout, under the tab bar.
 */
export function AppShellCard({ children }: { children: ReactNode }) {
  return (
    <main className="relative flex min-w-0 flex-1 flex-row overflow-hidden bg-page-canvas md:my-2 md:mr-2 md:rounded-xl md:shadow-[var(--surface-shadow)] md:ring-1 md:ring-surface-border">
      <SpacePanel />
      <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">{children}</div>
    </main>
  );
}

export function DashboardLayout({
  children,
  extra,
  loadingIndicator,
}: DashboardLayoutProps) {
  return (
    <DashboardGuard
      loadingFallback={
        <div className="flex h-svh items-center justify-center">
          {loadingIndicator}
        </div>
      }
    >
      <SidebarProvider className="h-svh bg-app-shell [--sidebar-wrapper-fill:var(--app-shell)]">
        <AppShellProvider>
          <ShellRouteDataPrefetch>
            <GlobalShortcuts />
            <WorkspacePresencePrefetch />
            <AppRail className="hidden md:flex" />
            <CompactNavSheet />
            <AppShellCard>
              <NavigationProgress />
              {children}
              <ModalRegistry />
              <SourceBackfillModal />
              {extra}
            </AppShellCard>
          </ShellRouteDataPrefetch>
        </AppShellProvider>
      </SidebarProvider>
    </DashboardGuard>
  );
}
