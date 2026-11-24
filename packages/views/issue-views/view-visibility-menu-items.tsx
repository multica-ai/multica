"use client";

import { Globe, Lock } from "@multica/ui/icons";
import {
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import type { IssueView } from "@multica/core/api/schemas";
import { useT } from "../i18n";
import type { IssueViewActions } from "./use-view-actions";

export function ViewVisibilityMenuItems({ view, actions }: {
  view: IssueView;
  actions: Pick<IssueViewActions, "canManage" | "setVisibility">;
}) {
  const { t } = useT("issues");
  if (view.scope_type === "my" || !actions.canManage(view)) return null;

  return (
    <>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.views.visible_to)}</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={view.visibility === "workspace" ? "workspace" : "private"}
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
      </DropdownMenuGroup>
    </>
  );
}
