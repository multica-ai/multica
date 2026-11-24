"use client";

import { Activity, MoreHorizontal, Unplug } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

/** The "…" menu of a connected account: its activity, and disconnecting it. */
export function AccountMenu({
  label,
  onActivity,
  onDisconnect,
}: {
  label: string;
  onActivity: () => void;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<IconButton variant="ghost" size="icon-sm" label={label} />}>
        <MoreHorizontal />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto">
        <DropdownMenuItem onClick={onActivity}>
          <Activity />
          {t(($) => $.connected_apps.activity)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDisconnect}>
          <Unplug />
          {t(($) => $.connected_apps.disconnect)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
