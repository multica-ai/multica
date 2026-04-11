"use client";

import type { ReactNode } from "react";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "../modals/registry";
import { AppSidebar } from "./app-sidebar";
import { DashboardGuard } from "./dashboard-guard";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Sibling of SidebarInset (e.g. SearchCommand, ChatWindow) */
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
      loginPath="/"
      loadingFallback={
        <div className="flex h-svh items-center justify-center">
          {loadingIndicator}
        </div>
      }
    >
      <SidebarProvider className="h-svh">
        <AppSidebar searchSlot={searchSlot} />
        <SidebarInset className="overflow-hidden">
          {children}
          <ModalRegistry />
        </SidebarInset>
        {extra}
      </SidebarProvider>
    </DashboardGuard>
  );
}
