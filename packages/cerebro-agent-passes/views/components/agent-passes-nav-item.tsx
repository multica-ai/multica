"use client";

import { KeyRound } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";

interface AgentPassesNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro agent passes admin page (JEH-1731).
// Hidden when the `cerebro_agent_passes` feature flag is off, and when
// the current member is not a workspace owner/admin. Defense-in-depth:
// the page itself also gates non-admins; hiding the nav item is the
// cosmetic complement.
export function AgentPassesNavItem({
  workspaceSlug,
  onClick,
}: AgentPassesNavItemProps) {
  const enabled = useFeatureFlag("cerebro_agent_passes");
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;
  if (role !== "owner" && role !== "admin") return null;

  const href = `/${workspaceSlug}/agent-passes`;
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
        <KeyRound />
        <span>Agent passes</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
