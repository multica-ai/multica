"use client";

import { ShieldCheck } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";

interface PermissionsNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro permissions page (JEH-1180). Hidden when
// the `cerebro_persona_permissions` feature flag is off.
export function PermissionsNavItem({
  workspaceSlug,
  onClick,
}: PermissionsNavItemProps) {
  const enabled = useFeatureFlag("cerebro_persona_permissions");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/permissions`;
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
        <ShieldCheck />
        <span>Permissions</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
