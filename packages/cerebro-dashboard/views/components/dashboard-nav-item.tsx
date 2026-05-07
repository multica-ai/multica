"use client";

import { LayoutDashboard } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface DashboardNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro dashboard. Hidden when the workspace has the
// `cerebro_dashboard` feature flag turned off — falling back to upstream's
// nav schema (no Dashboard item).
export function DashboardNavItem({ workspaceSlug, onClick }: DashboardNavItemProps) {
  const enabled = useFeatureFlag("cerebro_dashboard");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/dashboard`;
  const isActive = pathname === href || pathname.startsWith(href + "/");

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isActive}
        render={<AppLink href={href} />}
        onClick={onClick}
        className={cn(
          "text-muted-foreground hover:not-data-active:bg-sidebar-accent/70",
          "data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
        )}
      >
        <LayoutDashboard />
        <span>Dashboard</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
