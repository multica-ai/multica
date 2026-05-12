"use client";

import { Workflow } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface WorkflowsNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro workflows page. Hidden when the workspace
// has the `cerebro_workflows` feature flag turned off. JEH-1047.
export function WorkflowsNavItem({ workspaceSlug, onClick }: WorkflowsNavItemProps) {
  const enabled = useFeatureFlag("cerebro_workflows");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/workflows`;
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
        <Workflow />
        <span>Workflows</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
