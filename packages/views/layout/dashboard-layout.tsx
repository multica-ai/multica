"use client";

import type { ReactNode } from "react";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "../modals/registry";
import { AppSidebar } from "./app-sidebar";
import { DashboardGuard } from "./dashboard-guard";
import { MobileBottomNav } from "./mobile-bottom-nav";
import { MobilePageTransition } from "./mobile-page-transition";
import { NavigationProgress } from "./navigation-progress";
import { WorkspacePresencePrefetch } from "./workspace-presence-prefetch";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Rendered inside SidebarInset (e.g. ChatWindow, ChatFab — absolute-positioned overlays) */
  extra?: ReactNode;
  /** Rendered inside sidebar header as a search trigger */
  searchSlot?: ReactNode;
  /** Loading indicator */
  loadingIndicator?: ReactNode;
}

export function DashboardLayout({
  children,
  extra,
  searchSlot,
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
      <SidebarProvider className="h-svh">
        <WorkspacePresencePrefetch />
        <AppSidebar searchSlot={searchSlot} />
        {/* Mobile bottom nav adds ~3.25rem of fixed-position chrome at the
         * viewport bottom (md:hidden); pad SidebarInset on mobile so
         * content/scrollers don't disappear under it. env(safe-area-inset-bottom)
         * accounts for the iOS home indicator in standalone PWA mode. */}
        <SidebarInset className="relative overflow-hidden pb-[calc(3.25rem+env(safe-area-inset-bottom))] md:pb-0">
          <NavigationProgress />
          <MobilePageTransition>{children}</MobilePageTransition>
          <ModalRegistry />
          {extra}
          <MobileBottomNav />
        </SidebarInset>
      </SidebarProvider>
    </DashboardGuard>
  );
}
