"use client";

import { Globe, Lock } from "@multica/ui/icons";
import {
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { IssueView } from "@multica/core/api/schemas";
import { useT } from "../i18n";
import type { IssueViewActions } from "./use-view-actions";

/** One "Visible to" row showing the current choice; the choices open in a submenu. */
export function ViewVisibilityMenuItems({ view, actions }: {
  view: IssueView;
  actions: Pick<IssueViewActions, "canManage" | "setVisibility">;
}) {
  const { t } = useT("issues");
  if (view.scope_type === "my" || !actions.canManage(view)) return null;

  const shared = view.visibility === "workspace";
  const Icon = shared ? Globe : Lock;
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <Icon className="size-3.5" />
        <span className="flex-1">{t(($) => $.views.visible_to)}</span>
        <span className="ml-3 text-caption font-medium text-muted-foreground">
          {shared ? t(($) => $.views.workspace_members) : t(($) => $.views.only_me)}
        </span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-auto min-w-44">
        <DropdownMenuRadioGroup
          value={shared ? "workspace" : "private"}
          onValueChange={(value) => {
            if (value === "private" || value === "workspace") actions.setVisibility(view, value);
          }}
        >
          <DropdownMenuRadioItem value="private">
            <Lock className="size-3.5" />
            {t(($) => $.views.only_me)}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="workspace">
            <Globe className="size-3.5" />
            {t(($) => $.views.workspace_members)}
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
