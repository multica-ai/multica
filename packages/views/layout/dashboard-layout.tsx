"use client";

import type { ReactNode } from "react";
import {
  SidebarProvider,
  SidebarInset,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { ModalRegistry } from "../modals/registry";
import { SourceBackfillModal } from "../onboarding/source-backfill-modal";
import { AppSidebar } from "./app-sidebar";
import { DashboardGuard } from "./dashboard-guard";
import { NavigationProgress } from "./navigation-progress";
import { WorkspacePresencePrefetch } from "./workspace-presence-prefetch";
import { GlobalShortcuts } from "./global-shortcuts";
import { TabBar } from "./tab-bar";
import { useSessionTabsEnabled } from "./session-tabs";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Rendered inside SidebarInset (e.g. ChatWindow, ChatFab — absolute-positioned overlays) */
  extra?: ReactNode;
  /** Rendered inside sidebar header as a search trigger */
  searchSlot?: ReactNode;
  /** Loading indicator */
  loadingIndicator?: ReactNode;
}

/**
 * Desktop's MainCanvas equivalent: the rounded page card sits *below* the
 * session-tab strip, not around it. SidebarInset cannot wrap both — it is the
 * card (ring / radius / shadow), and making it a nested flex child would also
 * drop the `peer-data-[variant=inset]` styles.
 */
function SessionCanvas({ children }: { children: ReactNode }) {
  const { state } = useSidebar();
  const collapsed = state === "collapsed";
  return (
    <div
      className={cn(
        "relative mr-2 mb-2 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-page-canvas ring-1 ring-surface-border shadow-[var(--surface-shadow)]",
        collapsed ? "ml-2" : "ml-0",
      )}
    >
      {children}
    </div>
  );
}

export function DashboardLayout({
  children,
  extra,
  searchSlot,
  loadingIndicator,
}: DashboardLayoutProps) {
  const sessionTabs = useSessionTabsEnabled();
  const canvas = (
    <>
      <NavigationProgress />
      {children}
      <ModalRegistry />
      <SourceBackfillModal />
      {extra}
    </>
  );
  return (
    <DashboardGuard
      loadingFallback={
        <div className="flex h-svh items-center justify-center">
          {loadingIndicator}
        </div>
      }
    >
      <SidebarProvider
        className={cn(
          "h-svh bg-app-shell",
          sessionTabs && "[--sidebar-wrapper-fill:var(--app-shell)]",
        )}
      >
        <GlobalShortcuts />
        <WorkspacePresencePrefetch />
        <AppSidebar searchSlot={searchSlot} />
        {sessionTabs ? (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <div className="relative z-10 h-12 shrink-0">
              <TabBar />
            </div>
            <SessionCanvas>{canvas}</SessionCanvas>
          </div>
        ) : (
          <SidebarInset className="relative overflow-hidden">{canvas}</SidebarInset>
        )}
      </SidebarProvider>
    </DashboardGuard>
  );
}
