"use client";

import type { ReactNode } from "react";
import { MoreHorizontal, Settings } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

export function IssueSidebarSettingsMenu({ title, label, tab, triggerLabel, children }: {
  title: string;
  label: string;
  tab: "quick-actions" | "wakeups" | "properties" | "plugins" | "code";
  triggerLabel?: string;
  children?: ReactNode;
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={
        <IconButton size="icon-xs" label={triggerLabel ?? t(($) => $.sidebar_settings.menu, { section: title })}><MoreHorizontal /></IconButton>
      } />
      <DropdownMenuContent align="end" className="w-max max-w-(--available-width)">
        {children}
        <DropdownMenuItem onClick={() => navigation.push(`${paths.settings()}?tab=${tab}`)}>
          <Settings />
          {label}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
