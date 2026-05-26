"use client";

import { ShieldCheck } from "lucide-react";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";

interface AccessNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

export function AccessNavItem({ workspaceSlug, onClick }: AccessNavItemProps) {
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const { pathname } = useNavigation();

  if (!workspaceSlug) return null;
  if (role !== "owner" && role !== "admin") return null;

  const href = `/${workspaceSlug}/access`;
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
        <span>Access</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
