"use client";

import { useMemo, useState } from "react";
import { Globe2, Layers3, Lock, ShieldAlert } from "lucide-react";
import type {
  AgentAvailabilityMode,
  AgentInvocationTarget,
  AgentPermissionMode,
  Space,
} from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { PickerItem, PropertyPicker } from "../../../issues/components/pickers";
import { useT } from "../../../i18n";
import { CHIP_CLASS } from "./chip";

export interface AvailabilityChange {
  availability_mode: AgentAvailabilityMode;
  availability_space_ids: string[];
}

type DisplayMode = AgentAvailabilityMode | "legacy_custom";

function hasWorkspaceAudience(
  targets: AgentInvocationTarget[] | null | undefined,
): boolean {
  return (targets ?? []).some((target) => target.target_type === "workspace");
}

function resolveDisplayMode(
  availabilityMode: AgentAvailabilityMode | null | undefined,
  permissionMode: AgentPermissionMode,
  targets: AgentInvocationTarget[] | null | undefined,
): DisplayMode {
  const mode =
    availabilityMode ??
    (permissionMode === "private" ? "private" : "workspace");

  // Older agents can be shared with specific people. Keep that state honest
  // instead of labelling it Workspace and implying broader reach. Choosing a
  // new product Availability explicitly migrates it to one of the three
  // supported modes.
  if (
    mode === "workspace" &&
    permissionMode === "public_to" &&
    !hasWorkspaceAudience(targets)
  ) {
    return "legacy_custom";
  }
  return mode;
}

export function AvailabilityPicker({
  availabilityMode,
  availabilitySpaceIds,
  permissionMode,
  invocationTargets,
  spaces,
  canEdit = true,
  hasComposioAllowlist = false,
  onChange,
}: {
  availabilityMode?: AgentAvailabilityMode;
  availabilitySpaceIds?: string[];
  permissionMode: AgentPermissionMode;
  invocationTargets?: AgentInvocationTarget[];
  spaces: Space[];
  canEdit?: boolean;
  hasComposioAllowlist?: boolean;
  onChange: (next: AvailabilityChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [showSharingHint, setShowSharingHint] = useState(false);
  const mode = resolveDisplayMode(
    availabilityMode,
    permissionMode,
    invocationTargets,
  );
  const persistedSelected = useMemo(
    () => new Set(availabilitySpaceIds ?? []),
    [availabilitySpaceIds],
  );
  const [draftMode, setDraftMode] = useState<AgentAvailabilityMode | null>(
    mode === "legacy_custom" ? null : mode,
  );
  const [draftSelected, setDraftSelected] = useState<Set<string>>(
    () => new Set(availabilitySpaceIds ?? []),
  );

  const activeSpaces = useMemo(
    () => spaces.filter((space) => !space.archived_at),
    [spaces],
  );
  const persistedActiveCount = activeSpaces.filter((space) =>
    persistedSelected.has(space.id),
  ).length;
  const archivedSelectedSpaces = useMemo(
    () =>
      spaces.filter(
        (space) => !!space.archived_at && draftSelected.has(space.id),
      ),
    [draftSelected, spaces],
  );
  const knownSpaceIDs = useMemo(
    () => new Set(spaces.map((space) => space.id)),
    [spaces],
  );
  const unavailableIDs = [...draftSelected].filter(
    (id) => !knownSpaceIDs.has(id),
  );

  const choose = (
    nextMode: AgentAvailabilityMode,
  ) => {
    const shared = nextMode !== "private";
    if (shared && mode === "private" && hasComposioAllowlist) {
      setShowSharingHint(true);
    } else if (!shared) {
      setShowSharingHint(false);
    }
    setDraftMode(nextMode);
  };

  const toggleSpace = (spaceID: string, checked: boolean) => {
    const next = new Set(draftSelected);
    if (checked) {
      next.add(spaceID);
    } else {
      next.delete(spaceID);
    }
    setDraftSelected(next);
    setDraftMode("selected_spaces");
  };

  const hasUnavailableSelection =
    archivedSelectedSpaces.length > 0 || unavailableIDs.length > 0;
  const canApply =
    draftMode !== null &&
    (draftMode !== "selected_spaces" ||
      (draftSelected.size > 0 && !hasUnavailableSelection));
  const apply = () => {
    if (!draftMode || !canApply) return;
    void onChange({
      availability_mode: draftMode,
      availability_space_ids:
        draftMode === "selected_spaces" ? [...draftSelected] : [],
    });
    setOpen(false);
  };

  const summary =
    mode === "private"
      ? t(($) => $.availability_scope.trigger_private)
      : mode === "selected_spaces"
        ? t(($) => $.availability_scope.trigger_spaces_count, {
            count: persistedActiveCount,
          })
        : mode === "workspace"
          ? t(($) => $.availability_scope.trigger_workspace)
          : t(($) => $.availability_scope.trigger_legacy_custom);
  const SummaryIcon =
    mode === "private"
      ? Lock
      : mode === "selected_spaces"
        ? Layers3
        : mode === "workspace"
          ? Globe2
          : ShieldAlert;
  const tooltip = t(($) => $.availability_scope.tooltip);

  if (!canEdit || availabilityMode === undefined) {
    const readOnly =
      availabilityMode === undefined
        ? t(($) => $.availability_scope.unsupported_server)
        : t(($) => $.availability_scope.owner_only_readonly);
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              className="inline-flex items-center gap-1 text-xs text-muted-foreground"
              aria-label={readOnly}
              data-testid="availability-readonly"
            >
              <SummaryIcon className="h-3 w-3 shrink-0" />
              <span className="truncate">{summary}</span>
              <Lock className="h-3 w-3 shrink-0 opacity-60" />
            </span>
          }
        />
        <TooltipContent>{readOnly}</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) {
          setShowSharingHint(false);
          setDraftMode(mode === "legacy_custom" ? null : mode);
          setDraftSelected(new Set(persistedSelected));
        }
      }}
      width="w-auto min-w-[17rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={
        <>
          <SummaryIcon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{summary}</span>
        </>
      }
    >
      {mode === "legacy_custom" && (
        <div className="mx-1 mb-1 rounded-md bg-amber-500/10 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability_scope.legacy_custom_hint)}
        </div>
      )}

      <PickerItem
        selected={draftMode === "private"}
        onClick={() => choose("private")}
      >
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">
            {t(($) => $.availability_scope.private_title)}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.availability_scope.private_desc)}
          </div>
        </div>
      </PickerItem>

      <div className="mt-1 border-t pt-1">
        <button
          type="button"
          onClick={() => choose("selected_spaces")}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent ${
            draftMode === "selected_spaces" ? "bg-accent/60" : ""
          }`}
        >
          <Layers3 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div>
            <div className="font-medium">
              {t(($) => $.availability_scope.selected_spaces_title)}
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.availability_scope.selected_spaces_desc)}
            </div>
          </div>
        </button>
        {activeSpaces.length === 0 ? (
          <div className="px-2 py-2 text-xs text-muted-foreground">
            {t(($) => $.availability_scope.spaces_empty)}
          </div>
        ) : (
          <div className="max-h-48 overflow-y-auto px-1">
            {activeSpaces.map((space) => (
              <label
                key={space.id}
                className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
              >
                <Checkbox
                  checked={draftSelected.has(space.id)}
                  onCheckedChange={(value) =>
                    toggleSpace(space.id, value === true)
                  }
                  aria-label={space.name}
                />
                <span className="min-w-0 flex-1 truncate">{space.name}</span>
                <span className="text-[10px] text-muted-foreground">
                  {space.key}
                </span>
              </label>
            ))}
          </div>
        )}
        {unavailableIDs.map((id) => (
          <label
            key={id}
            className="flex items-center gap-2 px-3 py-1 text-xs text-muted-foreground"
          >
            <Checkbox
              checked
              onCheckedChange={(value) => toggleSpace(id, value === true)}
              aria-label={t(($) => $.availability_scope.unavailable_space)}
            />
            <span className="line-through">
              {t(($) => $.availability_scope.unavailable_space)}
            </span>
          </label>
        ))}
        {archivedSelectedSpaces.map((space) => (
          <label
            key={space.id}
            className="flex items-center gap-2 px-3 py-1 text-xs text-muted-foreground"
          >
            <Checkbox
              checked
              onCheckedChange={(value) =>
                toggleSpace(space.id, value === true)
              }
              aria-label={space.name}
            />
            <span className="min-w-0 flex-1 truncate line-through">
              {space.name}
            </span>
            <span>{t(($) => $.availability_scope.archived_badge)}</span>
          </label>
        ))}
      </div>

      <div className="mt-1 border-t pt-1">
        <PickerItem
          selected={draftMode === "workspace"}
          onClick={() => choose("workspace")}
        >
          <Globe2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="text-left">
            <div className="font-medium">
              {t(($) => $.availability_scope.workspace_title)}
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.availability_scope.workspace_desc)}
            </div>
          </div>
        </PickerItem>
      </div>

      {showSharingHint && (
        <div className="mx-1 mt-1 rounded-md bg-amber-500/10 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability_scope.composio_switch_hint)}
        </div>
      )}
      {draftMode === "selected_spaces" && draftSelected.size === 0 && (
        <div className="mx-1 mt-1 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability_scope.select_one_hint)}
        </div>
      )}
      {draftMode === "selected_spaces" && hasUnavailableSelection && (
        <div className="mx-1 mt-1 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.availability_scope.remove_unavailable_hint)}
        </div>
      )}
      <div className="mx-1 mt-1 border-t px-1 pt-2 text-[11px] text-muted-foreground">
        {t(($) => $.availability_scope.work_access_note)}
      </div>
      <div className="mt-2 flex justify-end gap-2 border-t px-1 pt-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setOpen(false)}
        >
          {t(($) => $.availability_scope.cancel)}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!canApply}
          onClick={apply}
        >
          {t(($) => $.availability_scope.apply)}
        </Button>
      </div>
    </PropertyPicker>
  );
}
