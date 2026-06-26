"use client";

import { Globe } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface BrowserNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// Sidebar entry for the cerebro personal browser tab (FIR-2037). Hidden when
// the `cerebro_browser` flag is off, and desktop-only: the underlying native
// browser pane only exists in the Electron app, so the entry stays hidden on
// web where `window.cerebroBrowser` is absent.
export function BrowserNavItem({ workspaceSlug, onClick }: BrowserNavItemProps) {
  const enabled = useFeatureFlag("cerebro_browser");
  const { pathname } = useNavigation();

  const isDesktop =
    typeof window !== "undefined" && "cerebroBrowser" in window;
  if (!enabled || !workspaceSlug || !isDesktop) return null;

  const href = `/${workspaceSlug}/cerebro/browser`;
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
        <Globe />
        <span>Browser</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
