"use client";

import { BellRing } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";

interface RemindersNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// RemindersNavItem renders the "Reminders" entry in the left sidebar. Gated by
// the cerebro_reminders_page flag (FIR-394, Jesper) so the standalone page can be
// shown or hidden independently of the reminder system's "remind me" actions.
export function RemindersNavItem({ workspaceSlug, onClick }: RemindersNavItemProps) {
  const enabled = useFeatureFlag("cerebro_reminders_page");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/reminders`;
  const isActive = pathname === href || pathname.startsWith(href + "/");

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isActive}
        render={<AppLink href={href} />}
        onClick={onClick}
        tooltip="Reminders"
        className={cn(
          "text-muted-foreground hover:not-data-active:bg-sidebar-accent/70",
          "data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
        )}
      >
        <BellRing />
        <span>Reminders</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
