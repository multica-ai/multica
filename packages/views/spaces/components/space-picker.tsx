"use client";

import type { ReactElement } from "react";
import { Check, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { activeSpaceListOptions } from "@multica/core/spaces/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Space } from "@multica/core/types";
import { SpaceIcon } from "./space-icon";
import { useT } from "../../i18n";

// icon + key: how a space is identified everywhere (picker rows, triggers).
function SpaceBadge({ space }: { space: Space }) {
  return (
    <span className="flex items-center gap-1.5 overflow-hidden">
      <SpaceIcon space={space} />
      <span className="shrink-0 text-[10px] font-medium text-muted-foreground">
        {space.key}
      </span>
    </span>
  );
}

// Single-select and non-clearable by design: every issue belongs to exactly
// one space, so the picker never offers an empty state — `spaceId` is only
// null while the space list is still loading.
export function SpacePicker({
  spaceId,
  onChange,
  triggerRender,
  align = "start",
  disabled = false,
}: {
  spaceId: string | null;
  onChange: (spaceId: string) => void;
  triggerRender?: ReactElement;
  align?: "start" | "center" | "end";
  // Locked display (e.g. sub-issues inherit the parent's space server-side).
  disabled?: boolean;
}) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  // Always the full active list: space is a creation-time default, never
  // constrained by the selected project (the old allowedSpaceIds filter was
  // a strong-association-model leftover).
  const { data: spaces = [] } = useQuery(activeSpaceListOptions(wsId));
  const current = spaces.find((space) => space.id === spaceId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        className={
          triggerRender
            ? undefined
            : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden disabled:cursor-default disabled:hover:bg-transparent"
        }
        render={triggerRender}
      >
        {/* Trigger shows icon + name — the key alone reads as an opaque
            identifier out of context (e.g. an auto-normalized "T2323");
            key + name live together in the menu items. */}
        {current ? (
          <>
            <SpaceIcon space={current} />
            <span className="truncate">{current.name}</span>
          </>
        ) : (
          <>
            <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.picker.placeholder)}</span>
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {spaces.map((space) => (
          <DropdownMenuItem key={space.id} onClick={() => onChange(space.id)}>
            <SpaceBadge space={space} />
            <span className="truncate">{space.name}</span>
            {space.id === spaceId && (
              <Check className="ml-auto h-3.5 w-3.5 shrink-0" />
            )}
          </DropdownMenuItem>
        ))}
        {spaces.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function SpaceMultiPicker({
  spaceIds,
  onChange,
  triggerRender,
  align = "start",
  disabled = false,
}: {
  spaceIds: string[];
  onChange: (spaceIds: string[]) => void;
  triggerRender?: ReactElement;
  align?: "start" | "center" | "end";
  // Display-only rendering of the current selection.
  disabled?: boolean;
}) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const { data: spaces = [] } = useQuery(activeSpaceListOptions(wsId));
  const selected = spaces.filter((space) => spaceIds.includes(space.id));

  const toggle = (spaceId: string) => {
    onChange(
      spaceIds.includes(spaceId)
        ? spaceIds.filter((id) => id !== spaceId)
        : [...spaceIds, spaceId],
    );
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        className={
          triggerRender
            ? undefined
            : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden disabled:cursor-default disabled:hover:bg-transparent"
        }
        render={triggerRender}
      >
        {/* Mirror the single picker's icon+name trigger when exactly one space
            is selected; degrade to icons+count for multiple. */}
        {selected.length === 0 ? (
          <>
            <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.picker.placeholder)}</span>
          </>
        ) : selected.length === 1 ? (
          <>
            <SpaceIcon space={selected[0]!} />
            <span className="truncate">{selected[0]!.name}</span>
          </>
        ) : (
          <>
            <span className="flex shrink-0 items-center gap-0.5">
              {selected.slice(0, 3).map((space) => (
                <SpaceIcon key={space.id} space={space} />
              ))}
            </span>
            <span className="truncate">
              {t(($) => $.picker.selected_count, { count: selected.length })}
            </span>
          </>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-56">
        {spaces.map((space) => {
          const checked = spaceIds.includes(space.id);
          return (
            <DropdownMenuCheckboxItem
              key={space.id}
              checked={checked}
              onCheckedChange={() => toggle(space.id)}
            >
              <SpaceBadge space={space} />
              <span className="truncate">{space.name}</span>
            </DropdownMenuCheckboxItem>
          );
        })}
        {spaces.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.picker.empty)}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
