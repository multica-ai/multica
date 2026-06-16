"use client";

import type { ReactNode } from "react";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../navigation";
import { ModalRegistry } from "../modals/registry";
import { SourceBackfillModal } from "../onboarding";
import { AppSidebar } from "./app-sidebar";
import { DashboardGuard } from "./dashboard-guard";
import { NavigationProgress } from "./navigation-progress";
import { WorkspacePresencePrefetch } from "./workspace-presence-prefetch";

const PRODUCT_TABS = {
  flow: "研发流程",
  knowledge: "知识库",
} as const;

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
  const { pathname } = useNavigation();
  const p = useWorkspacePaths();
  const isKnowledgeActive = /^(?:\/ug|\/[^/]+\/ug)(?:\/|$)/.test(pathname);

  return (
    <DashboardGuard
      loadingFallback={
        <div className="flex h-svh items-center justify-center">
          {loadingIndicator}
        </div>
      }
    >
      <SidebarProvider className="h-svh flex-col">
        <WorkspacePresencePrefetch />
        <NavigationProgress />
        <div className="flex h-12 shrink-0 items-center justify-center border-b bg-background/85 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/70">
          <div className="inline-flex gap-1 rounded-md border bg-muted/50 p-1">
            <AppLink
              href={p.issues()}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                !isKnowledgeActive
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {PRODUCT_TABS.flow}
            </AppLink>
            <AppLink
              href={p.ugGraph()}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                isKnowledgeActive
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {PRODUCT_TABS.knowledge}
            </AppLink>
          </div>
        </div>
        <div className="relative flex min-h-0 flex-1">
          {!isKnowledgeActive && <AppSidebar searchSlot={searchSlot} className="top-12 h-[calc(100svh-3rem)]" />}
          <SidebarInset
            className={cn(
              "relative min-h-0 overflow-hidden",
              isKnowledgeActive && "m-0 rounded-none md:m-0 md:rounded-none",
            )}
          >
            <div className="min-h-0 flex-1 overflow-hidden">
              {children}
            </div>
            <ModalRegistry />
            <SourceBackfillModal />
            {extra}
          </SidebarInset>
        </div>
      </SidebarProvider>
    </DashboardGuard>
  );
}
