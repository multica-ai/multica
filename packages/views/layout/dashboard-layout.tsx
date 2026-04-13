"use client";

import type { ReactNode } from "react";
import { SidebarProvider, SidebarInset, SidebarTrigger } from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "../modals/registry";
import { AppSidebar } from "./app-sidebar";
import { DashboardGuard } from "./dashboard-guard";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Rendered inside SidebarInset (e.g. ChatWindow, ChatFab — absolute-positioned overlays) */
  extra?: ReactNode;
  /** Rendered inside sidebar header as a search trigger */
  searchSlot?: ReactNode;
  /** Loading indicator */
  loadingIndicator?: ReactNode;
  /** Path to redirect when user has no workspace */
  onboardingPath?: string;
}

export function DashboardLayout({
  children,
  extra,
  searchSlot,
  loadingIndicator,
  onboardingPath,
}: DashboardLayoutProps) {
  return (
    <DashboardGuard
      loginPath="/"
      onboardingPath={onboardingPath}
      loadingFallback={
        <div className="flex h-svh items-center justify-center">
          {loadingIndicator}
        </div>
      }
    >
      <SidebarProvider className="h-svh">
        <AppSidebar searchSlot={searchSlot} />
        <SidebarInset className="relative overflow-hidden">
          <div className="flex h-10 shrink-0 items-center border-b px-2 md:hidden">
            <SidebarTrigger />
          </div>
          {children}
          <ModalRegistry />
          {extra}
        </SidebarInset>
      </SidebarProvider>
    </DashboardGuard>
  );
}
