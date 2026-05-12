"use client";

import { ListChecks } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface TasksNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro tasks page. Hidden when the workspace has
// the `cerebro_tasks` feature flag turned off. JEH-900.
export function TasksNavItem({ workspaceSlug, onClick }: TasksNavItemProps) {
  const enabled = useFeatureFlag("cerebro_tasks");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/tasks`;
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
        <ListChecks />
        <span>Tasks</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
