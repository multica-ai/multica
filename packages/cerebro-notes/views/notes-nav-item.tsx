"use client";

import { NotebookPen } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { AppLink, useNavigation } from "@multica/views/navigation";
import {
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";

interface NotesNavItemProps {
  workspaceSlug: string;
  onClick?: () => void;
}

// NotesNavItem renders the "Notes" entry in the left sidebar. Gated by the
// cerebro_notes flag so the feature is invisible until enabled per workspace.
export function NotesNavItem({ workspaceSlug, onClick }: NotesNavItemProps) {
  const enabled = useFeatureFlag("cerebro_notes");
  const { pathname } = useNavigation();

  if (!enabled || !workspaceSlug) return null;

  const href = `/${workspaceSlug}/notes`;
  const isActive = pathname === href || pathname.startsWith(href + "/");

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isActive}
        render={<AppLink href={href} />}
        onClick={onClick}
        tooltip="Notes"
      >
        <NotebookPen />
        <span>Notes</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
