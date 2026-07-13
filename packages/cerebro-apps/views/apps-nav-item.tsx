"use client";

import { Blocks } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { SidebarMenuButton, SidebarMenuItem } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";

export function AppsNavItem({ workspaceSlug, onClick }: { workspaceSlug: string; onClick?: () => void }) {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const { pathname } = useNavigation();
  if (!enabled || !workspaceSlug) return null;
  const href = `/${workspaceSlug}/apps`;
  return <SidebarMenuItem><SidebarMenuButton isActive={pathname === href || pathname.startsWith(href + "/")} render={<AppLink href={href} />} onClick={onClick} className={cn("text-muted-foreground hover:not-data-active:bg-sidebar-accent/70", "data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground")}><Blocks /><span>Apps</span></SidebarMenuButton></SidebarMenuItem>;
}
