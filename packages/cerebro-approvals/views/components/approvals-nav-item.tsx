"use client";

import { Inbox } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { Badge } from "@multica/ui/components/ui/badge";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { approvalsListOptions } from "../../core/queries";

interface ApprovalsNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro approval inbox (FIR-2131). Hidden when the
// `cerebro_approvals` flag is off and for non-admins (the page gates them too).
// Shows a badge with the pending count so an approver notices waiting asks.
export function ApprovalsNavItem({
  workspaceSlug,
  onClick,
}: ApprovalsNavItemProps) {
  const enabled = useFeatureFlag("cerebro_approvals");
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const { pathname } = useNavigation();

  const wsId = workspace?.id ?? "";
  const isAdmin = role === "owner" || role === "admin";
  const pendingQuery = useQuery({
    ...approvalsListOptions(wsId, { status: "pending", limit: 1, offset: 0 }),
    enabled: enabled && isAdmin && !!wsId,
  });

  if (!enabled || !workspaceSlug) return null;
  if (!isAdmin) return null;

  const href = `/${workspaceSlug}/approvals`;
  const isActive = pathname === href || pathname.startsWith(href + "/");
  const pending = pendingQuery.data?.pending ?? 0;

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
        <Inbox />
        <span>Approvals</span>
        {pending > 0 && (
          <Badge variant="secondary" className="ml-auto">
            {pending}
          </Badge>
        )}
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
