"use client";

import { ShieldCheck } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { pendingAsksOptions } from "../../core";

interface AccessNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

export function AccessNavItem({ workspaceSlug, onClick }: AccessNavItemProps) {
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const { pathname } = useNavigation();

  const pendingCount = useQuery(
    pendingAsksOptions(workspace?.id ?? "", { limit: 1, offset: 0 }),
  );

  if (!workspaceSlug) return null;
  if (role !== "owner" && role !== "admin") return null;

  const href = `/${workspaceSlug}/access`;
  const isActive = pathname === href || pathname.startsWith(href + "/");
  const pending = pendingCount.data?.total ?? 0;

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
        <span>Adgang</span>
        {pending > 0 && (
          <span className="ml-auto inline-flex h-4 min-w-[16px] items-center justify-center rounded-full bg-warning text-[10px] font-semibold text-warning-foreground px-1">
            {pending > 99 ? "99+" : pending}
          </span>
        )}
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
